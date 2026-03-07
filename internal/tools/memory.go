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
	Facts     map[string]string `json:"facts"`
	LocalKeys []string          `json:"local_keys,omitempty"`
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
	mf, err := m.loadFileLocked()
	if err != nil {
		return nil, err
	}
	return mf.Facts, nil
}

func (m *ProjectMemory) loadFileLocked() (memoryFile, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return memoryFile{Facts: map[string]string{}}, nil
		}
		return memoryFile{}, fmt.Errorf("reading memory file: %w", err)
	}

	var mf memoryFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return memoryFile{}, fmt.Errorf("parsing memory file: %w", err)
	}
	if mf.Facts == nil {
		mf.Facts = map[string]string{}
	}
	return mf, nil
}

// Save atomically writes the facts map to the memory file.
// Preserves existing local_keys.
func (m *ProjectMemory) Save(facts map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked(facts)
}

func (m *ProjectMemory) saveLocked(facts map[string]string) error {
	// Load existing file to preserve local_keys.
	existing, _ := m.loadFileLocked()
	existing.Facts = facts
	return m.saveFileLocked(existing)
}

func (m *ProjectMemory) saveFileLocked(mf memoryFile) error {
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating memory directory: %w", err)
	}

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

// MarkLocal marks a key as local-only (never synced to hub).
func (m *ProjectMemory) MarkLocal(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mf, err := m.loadFileLocked()
	if err != nil {
		return err
	}
	for _, k := range mf.LocalKeys {
		if k == key {
			return nil
		}
	}
	mf.LocalKeys = append(mf.LocalKeys, key)
	return m.saveFileLocked(mf)
}

// IsLocal reports whether a key is marked local-only.
func (m *ProjectMemory) IsLocal(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	mf, err := m.loadFileLocked()
	if err != nil {
		return false
	}
	for _, k := range mf.LocalKeys {
		if k == key {
			return true
		}
	}
	return false
}

// LocalKeys returns the list of local-only keys.
func (m *ProjectMemory) LocalKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	mf, err := m.loadFileLocked()
	if err != nil {
		return nil
	}
	return mf.LocalKeys
}

// MergeHub merges hub facts into local memory.
// Hub values overwrite shared keys. Local-only keys are never touched.
func (m *ProjectMemory) MergeHub(hubFacts map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mf, err := m.loadFileLocked()
	if err != nil {
		return err
	}
	if mf.Facts == nil {
		mf.Facts = make(map[string]string)
	}

	localSet := make(map[string]bool, len(mf.LocalKeys))
	for _, k := range mf.LocalKeys {
		localSet[k] = true
	}

	for k, v := range hubFacts {
		if localSet[k] {
			continue
		}
		mf.Facts[k] = v
	}

	return m.saveFileLocked(mf)
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
			Description: "Read all project memory facts that persist across sessions. Returns key-value pairs. Only call once per turn -do not repeat if you already have the result.",
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
			Description: "Save or remove a project memory fact that persists across sessions. Use 'set' to store a key-value pair, 'remove' to delete one. Good for remembering project conventions, URLs, config details. Facts are shared with the hub by default; use scope='local' for secrets or machine-specific values.",
			Properties: map[string]provider.ToolProp{
				"action": {Type: "string", Description: "Action to perform: 'set' or 'remove'"},
				"key":    {Type: "string", Description: "Fact key (e.g. 'auth', 'database', 'test_patterns')"},
				"value":  {Type: "string", Description: "Fact value (required for 'set' action)"},
				"scope":  {Type: "string", Description: "Scope: 'shared' (default, synced to hub) or 'local' (never synced)"},
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
			scope, _ := input["scope"].(string)
			scope = strings.ToLower(strings.TrimSpace(scope))
			if scope == "" {
				scope = "shared"
			}

			if key == "" {
				return "", fmt.Errorf("key is required")
			}
			if scope != "shared" && scope != "local" {
				return "", fmt.Errorf("invalid scope %q: must be 'shared' or 'local'", scope)
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

				if scope == "local" {
					if err := ctx.Memory.MarkLocal(key); err != nil {
						return "", fmt.Errorf("marking key as local: %w", err)
					}
				}

				// Push non-local facts to hub if connected
				if scope == "shared" && ctx.PushHubMemory != nil {
					go pushSharedFacts(ctx.Memory, ctx.PushHubMemory)
				}

				scopeLabel := ""
				if scope == "local" {
					scopeLabel = " [local]"
				}
				return fmt.Sprintf("Saved memory fact%s: %s = %s", scopeLabel, key, value), nil

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

				// Push updated facts to hub if connected
				if ctx.PushHubMemory != nil {
					go pushSharedFacts(ctx.Memory, ctx.PushHubMemory)
				}

				return fmt.Sprintf("Removed memory fact: %s", key), nil

			default:
				return "", fmt.Errorf("invalid action %q: must be 'set' or 'remove'", action)
			}
		},
	}
}

// pushSharedFacts gathers all non-local facts and pushes them to the hub.
func pushSharedFacts(mem *ProjectMemory, push func(map[string]string) error) {
	facts, err := mem.Load()
	if err != nil || len(facts) == 0 {
		return
	}
	localKeys := mem.LocalKeys()
	localSet := make(map[string]bool, len(localKeys))
	for _, k := range localKeys {
		localSet[k] = true
	}
	shared := make(map[string]string)
	for k, v := range facts {
		if !localSet[k] {
			shared[k] = v
		}
	}
	if len(shared) > 0 {
		_ = push(shared)
	}
}
