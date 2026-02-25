package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/batalabs/muxd/internal/provider"
)

// ProjectMemory provides thread-safe, file-backed per-project fact storage.
type ProjectMemory struct {
	path string
	mu   sync.Mutex
}

type memoryFile struct {
	Facts map[string]string `json:"facts"`
}

// NewProjectMemory creates a ProjectMemory rooted at <projectDir>/.muxd/memory.json.
func NewProjectMemory(projectDir string) *ProjectMemory {
	return &ProjectMemory{
		path: filepath.Join(projectDir, ".muxd", "memory.json"),
	}
}

// Load reads and unmarshals the memory file. Returns an empty map if the file
// does not exist.
func (m *ProjectMemory) Load() (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadLocked()
}

func (m *ProjectMemory) loadLocked() (map[string]string, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading memory file: %w", err)
	}

	var mf memoryFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("parsing memory file: %w", err)
	}
	if mf.Facts == nil {
		mf.Facts = map[string]string{}
	}
	return mf.Facts, nil
}

// Save atomically writes the facts map to the memory file.
func (m *ProjectMemory) Save(facts map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked(facts)
}

func (m *ProjectMemory) saveLocked(facts map[string]string) error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating memory directory: %w", err)
	}

	mf := memoryFile{Facts: facts}
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling memory: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: write to temp file then rename.
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp memory file: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming memory file: %w", err)
	}
	return nil
}

// FormatForPrompt loads facts and formats them as "key: value" lines.
// Returns "" if no facts exist.
func (m *ProjectMemory) FormatForPrompt() string {
	facts, err := m.Load()
	if err != nil || len(facts) == 0 {
		return ""
	}

	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, strings.TrimRight(facts[k], " \t"))
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// memory_read tool
// ---------------------------------------------------------------------------

func memoryReadTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "memory_read",
			Description: "Read all project memory facts. Returns key-value pairs persisted across sessions in .muxd/memory.json.",
			Properties:  map[string]provider.ToolProp{},
			Required:    []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.Memory == nil {
				return "", fmt.Errorf("project memory not available")
			}
			facts, err := ctx.Memory.Load()
			if err != nil {
				return "", fmt.Errorf("loading memory: %w", err)
			}
			if len(facts) == 0 {
				return "No project memory facts stored.", nil
			}

			keys := make([]string, 0, len(facts))
			for k := range facts {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var b strings.Builder
			for _, k := range keys {
				fmt.Fprintf(&b, "%s: %s\n", k, strings.TrimRight(facts[k], " \t"))
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}

// ---------------------------------------------------------------------------
// memory_write tool
// ---------------------------------------------------------------------------

func memoryWriteTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "memory_write",
			Description: "Write or remove a project memory fact. Facts persist across sessions in .muxd/memory.json. Use action 'set' to add/update, 'remove' to delete.",
			Properties: map[string]provider.ToolProp{
				"action": {Type: "string", Description: "Action to perform: 'set' or 'remove'"},
				"key":    {Type: "string", Description: "Fact key (e.g. 'auth', 'database', 'test_patterns')"},
				"value":  {Type: "string", Description: "Fact value (required for 'set' action)"},
			},
			Required: []string{"action", "key"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.Memory == nil {
				return "", fmt.Errorf("project memory not available")
			}

			action, _ := input["action"].(string)
			action = strings.ToLower(strings.TrimSpace(action))
			key, _ := input["key"].(string)
			key = strings.TrimSpace(key)
			value, _ := input["value"].(string)
			value = strings.TrimSpace(value)

			if key == "" {
				return "", fmt.Errorf("key is required")
			}

			switch action {
			case "set":
				if value == "" {
					return "", fmt.Errorf("value is required for set action")
				}
				ctx.Memory.mu.Lock()
				facts, err := ctx.Memory.loadLocked()
				if err != nil {
					ctx.Memory.mu.Unlock()
					return "", fmt.Errorf("loading memory: %w", err)
				}
				facts[key] = value
				if err := ctx.Memory.saveLocked(facts); err != nil {
					ctx.Memory.mu.Unlock()
					return "", fmt.Errorf("saving memory: %w", err)
				}
				ctx.Memory.mu.Unlock()
				return fmt.Sprintf("Saved memory fact: %s = %s", key, value), nil

			case "remove":
				ctx.Memory.mu.Lock()
				facts, err := ctx.Memory.loadLocked()
				if err != nil {
					ctx.Memory.mu.Unlock()
					return "", fmt.Errorf("loading memory: %w", err)
				}
				if _, exists := facts[key]; !exists {
					ctx.Memory.mu.Unlock()
					return fmt.Sprintf("Key %q not found in project memory.", key), nil
				}
				delete(facts, key)
				if err := ctx.Memory.saveLocked(facts); err != nil {
					ctx.Memory.mu.Unlock()
					return "", fmt.Errorf("saving memory: %w", err)
				}
				ctx.Memory.mu.Unlock()
				return fmt.Sprintf("Removed memory fact: %s", key), nil

			default:
				return "", fmt.Errorf("invalid action %q: must be 'set' or 'remove'", action)
			}
		},
	}
}
