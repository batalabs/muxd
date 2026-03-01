package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/provider"
)

// TodoList holds an in-memory per-session task list.
type TodoList struct {
	mu    sync.Mutex
	Items []TodoItem
}

// TodoItem is a single item in the task list.
type TodoItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"` // "pending", "in_progress", "completed"
	Description string `json:"description,omitempty"`
}

// ScheduledJobInfo is a lightweight representation of a scheduled tool job
// for the tool layer. Decoupled from the store.ScheduledToolJob type.
type ScheduledJobInfo struct {
	ID           string
	ToolName     string
	ToolInput    map[string]any
	ScheduledFor time.Time
	Recurrence   string
	Status       string
	CreatedAt    time.Time
}

// MCPManager is the interface for the MCP tool invocation layer.
// Defined here to avoid circular imports between tools and mcp packages.
type MCPManager interface {
	CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (string, bool)
}

// ToolContext provides shared state to tool implementations.
type ToolContext struct {
	Cwd                string
	Todos              *TodoList
	Memory             *ProjectMemory
	PlanMode           *bool
	Disabled           map[string]bool
	ScheduledAllowed   map[string]bool
	SpawnAgent         func(description, prompt string) (string, error)
	ScheduleTweet      func(text string, scheduledFor time.Time, recurrence string) (string, error) // legacy
	ScheduleTool       func(toolName string, input map[string]any, scheduledFor time.Time, recurrence string) (string, error)
	ListScheduledJobs  func(toolName string, limit int) ([]ScheduledJobInfo, error)
	CancelScheduledJob func(id string) error
	UpdateScheduledJob func(id string, toolInput map[string]any, scheduledFor *time.Time, recurrence *string) error
	BraveAPIKey        string
	TextbeltAPIKey     string
	XClientID          string
	XClientSecret      string
	XAccessToken       string
	XRefreshToken      string
	XTokenExpiry       string
	SaveXOAuthTokens   func(accessToken, refreshToken, tokenExpiry string) error
	MCP                MCPManager
}

// ToolFunc is the signature for tool execution functions.
type ToolFunc func(input map[string]any, ctx *ToolContext) (string, error)

// ToolDef binds a provider-agnostic tool specification to its implementation.
type ToolDef struct {
	Spec    provider.ToolSpec
	Execute ToolFunc
}

// Getwd is the function used to determine the current working directory.
// Override in tests to control the working directory.
var Getwd = os.Getwd

// AllTools returns the full list of tool definitions.
// PTC (AllowedCallers) and Tool Search (DeferLoading) infrastructure is in the
// provider layer but disabled by default. Set these fields on individual tools
// to enable once compatibility with your model is confirmed.
func AllTools() []ToolDef {
	return []ToolDef{
		fileReadTool(),
		fileWriteTool(),
		fileEditTool(),
		bashTool(),
		grepTool(),
		listFilesTool(),
		askUserTool(),
		todoReadTool(),
		todoWriteTool(),
		webSearchTool(),
		webFetchTool(),
		xPostTool(),
		xSearchTool(),
		xMentionsTool(),
		xReplyTool(),
		xScheduleTool(),
		xScheduleListTool(),
		xScheduleUpdateTool(),
		xScheduleCancelTool(),
		smsSendTool(),
		smsStatusTool(),
		smsScheduleTool(),
		logReadTool(),
		patchApplyTool(),
		planEnterTool(),
		planExitTool(),
		taskTool(),
		gitStatusTool(),
		memoryReadTool(),
		memoryWriteTool(),
		scheduleTaskTool(),
	}
}

// AllToolSpecs returns the provider-agnostic tool specifications.
func AllToolSpecs() []provider.ToolSpec {
	tools := AllTools()
	specs := make([]provider.ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = t.Spec
	}
	return specs
}

// FindTool looks up a tool by name.
func FindTool(name string) (ToolDef, bool) {
	for _, t := range AllTools() {
		if t.Spec.Name == name {
			return t, true
		}
	}
	return ToolDef{}, false
}

// ToolNames returns all built-in tool names, sorted alphabetically.
func ToolNames() []string {
	all := AllTools()
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Spec.Name)
	}
	sort.Strings(names)
	return names
}

// ToolDisplayName returns a user-facing alias for a tool name.
func ToolDisplayName(name string) string {
	switch name {
	case "web_search":
		return "web_search_brave"
	default:
		return name
	}
}

// NormalizeToolName maps user-facing aliases back to canonical tool names.
func NormalizeToolName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "web_search_brave":
		return "web_search"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

// ToolRiskTags returns a small set of risk tags for UI display.
func ToolRiskTags(name string) []string {
	if strings.HasPrefix(name, "mcp__") {
		return []string{"mcp"}
	}
	switch name {
	case "bash":
		return []string{"shell", "write"}
	case "file_write", "file_edit", "patch_apply":
		return []string{"write"}
	case "web_fetch", "web_search":
		return []string{"network"}
	case "x_post", "x_schedule", "x_reply":
		return []string{"network", "write"}
	case "x_search", "x_mentions":
		return []string{"network"}
	case "x_schedule_update", "x_schedule_cancel":
		return []string{"write"}
	case "sms_send":
		return []string{"network", "write"}
	case "sms_schedule":
		return []string{"write"}
	case "sms_status":
		return []string{"network"}
	default:
		return nil
	}
}

// ToolProfileDisabledSet returns disabled tools for a named preset profile.
// Profiles: safe, coder, research.
func ToolProfileDisabledSet(profile string) map[string]bool {
	profile = strings.ToLower(strings.TrimSpace(profile))
	disabled := map[string]bool{}
	switch profile {
	case "safe":
		disabled["bash"] = true
		disabled["web_fetch"] = true
		disabled["web_search"] = true
		disabled["x_post"] = true
		disabled["x_search"] = true
		disabled["x_mentions"] = true
		disabled["x_reply"] = true
		disabled["x_schedule"] = true
		disabled["x_schedule_update"] = true
		disabled["x_schedule_cancel"] = true
		disabled["sms_send"] = true
		disabled["sms_status"] = true
		disabled["sms_schedule"] = true
	case "coder":
		// Keep all enabled by default.
	case "research":
		disabled["file_write"] = true
		disabled["file_edit"] = true
		disabled["patch_apply"] = true
		disabled["bash"] = true
	}
	return disabled
}

// ---------------------------------------------------------------------------
// file_read
// ---------------------------------------------------------------------------

func fileReadTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "file_read",
			Description: "Read a file's contents with line numbers. Use offset and limit for large files. Read before editing to get exact text.",
			Properties: map[string]provider.ToolProp{
				"path":   {Type: "string", Description: "Absolute or relative file path to read"},
				"offset": {Type: "integer", Description: "Line number to start reading from (1-based, default: 1)"},
				"limit":  {Type: "integer", Description: "Maximum number of lines to read (default: all)"},
			},
			Required: []string{"path"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			path, ok := input["path"].(string)
			if !ok || path == "" {
				return "", fmt.Errorf("path is required")
			}

			if IsDeniedConfigFile(path) {
				return "", fmt.Errorf("access denied: %s contains secrets and cannot be read by the agent", filepath.Base(path))
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("reading %s: %w", path, err)
			}

			text := strings.ReplaceAll(string(data), "\r\n", "\n")
			lines := strings.Split(text, "\n")

			offset := 1
			if v, ok := input["offset"].(float64); ok && v > 0 {
				offset = int(v)
			}

			limit := len(lines)
			if v, ok := input["limit"].(float64); ok && v > 0 {
				limit = int(v)
			}

			start := offset - 1
			if start < 0 {
				start = 0
			}
			if start > len(lines) {
				start = len(lines)
			}
			end := start + limit
			if end > len(lines) {
				end = len(lines)
			}

			var b strings.Builder
			for i := start; i < end; i++ {
				fmt.Fprintf(&b, "%4d │ %s\n", i+1, lines[i])
			}

			result := b.String()
			if len(result) > 50*1024 {
				result = result[:50*1024] + "\n... (truncated at 50KB)"
			}

			return result, nil
		},
	}
}

// ---------------------------------------------------------------------------
// file_write
// ---------------------------------------------------------------------------

func fileWriteTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "file_write",
			Description: "Create or overwrite a file. Parent directories are created automatically. Prefer file_edit for modifying existing files.",
			Properties: map[string]provider.ToolProp{
				"path":    {Type: "string", Description: "File path to write to"},
				"content": {Type: "string", Description: "Content to write to the file"},
			},
			Required: []string{"path", "content"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			path, ok := input["path"].(string)
			if !ok || path == "" {
				return "", fmt.Errorf("path is required")
			}
			content, _ := input["content"].(string)

			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("creating directories: %w", err)
			}

			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("writing %s: %w", path, err)
			}

			lines := strings.Count(content, "\n") + 1
			return fmt.Sprintf("Wrote %d bytes (%d lines) to %s", len(content), lines, path), nil
		},
	}
}

// ---------------------------------------------------------------------------
// file_edit
// ---------------------------------------------------------------------------

func fileEditTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "file_edit",
			Description: "Replace exact text in a file. old_string must match exactly once (or use replace_all for bulk changes). Always read the file first to get the exact text to match.",
			Properties: map[string]provider.ToolProp{
				"path":        {Type: "string", Description: "File path"},
				"old_string":  {Type: "string", Description: "Exact text to find"},
				"new_string":  {Type: "string", Description: "Text to replace it with"},
				"replace_all": {Type: "boolean", Description: "Replace all occurrences instead of requiring exactly one (default: false)"},
			},
			Required: []string{"path", "old_string", "new_string"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			path, ok := input["path"].(string)
			if !ok || path == "" {
				return "", fmt.Errorf("path is required")
			}
			oldStr, ok := input["old_string"].(string)
			if !ok || oldStr == "" {
				return "", fmt.Errorf("old_string is required")
			}
			newStr, _ := input["new_string"].(string)

			replaceAll := false
			if v, ok := input["replace_all"].(bool); ok {
				replaceAll = v
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("reading %s: %w", path, err)
			}

			content := string(data)
			count := strings.Count(content, oldStr)
			if count == 0 {
				return "", fmt.Errorf("old_string not found in %s", path)
			}

			var newContent string
			if replaceAll {
				newContent = strings.ReplaceAll(content, oldStr, newStr)
			} else {
				if count > 1 {
					return "", fmt.Errorf("old_string found %d times in %s (must match exactly once, or set replace_all)", count, path)
				}
				newContent = strings.Replace(content, oldStr, newStr, 1)
			}

			if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
				return "", fmt.Errorf("writing %s: %w", path, err)
			}

			return fmt.Sprintf("Edited %s: replaced %d occurrence(s) of %d bytes with %d bytes", path, count, len(oldStr), len(newStr)), nil
		},
	}
}

// ---------------------------------------------------------------------------
// bash
// ---------------------------------------------------------------------------

func bashTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "bash",
			Description: "Run a shell command and return stdout+stderr. Use for git, build commands, installers, and other CLI tools. Prefer file_read/file_edit/grep for file operations.",
			Properties: map[string]provider.ToolProp{
				"command": {Type: "string", Description: "Shell command to execute"},
				"timeout": {Type: "integer", Description: "Timeout in seconds (default: 30, max: 120)"},
			},
			Required: []string{"command"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			command, ok := input["command"].(string)
			if !ok || command == "" {
				return "", fmt.Errorf("command is required")
			}

			timeout := 30
			if v, ok := input["timeout"].(float64); ok && v > 0 {
				timeout = int(v)
				if timeout > 120 {
					timeout = 120
				}
			}

			cmdCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				// Prefer a POSIX shell when command syntax requires it (e.g. heredocs).
				if needsPosixShell(command) {
					if _, err := exec.LookPath("bash"); err == nil {
						cmd = exec.CommandContext(cmdCtx, "bash", "-lc", command)
					} else if _, err := exec.LookPath("sh"); err == nil {
						cmd = exec.CommandContext(cmdCtx, "sh", "-c", command)
					}
				}
				if cmd == nil {
					cmd = exec.CommandContext(cmdCtx, "cmd", "/C", command)
				}
			} else {
				cmd = exec.CommandContext(cmdCtx, "sh", "-c", command)
			}
			cwd, _ := Getwd()
			cmd.Dir = cwd

			out, err := cmd.CombinedOutput()
			result := string(out)
			if len(result) > 50*1024 {
				result = result[:50*1024] + "\n... (truncated at 50KB)"
			}

			if err != nil {
				if cmdCtx.Err() == context.DeadlineExceeded {
					return result + "\n(command timed out after " + strconv.Itoa(timeout) + "s)", nil
				}
				return result + "\n(exit code: " + err.Error() + ")", nil
			}

			return result, nil
		},
	}
}

func needsPosixShell(command string) bool {
	// Minimal heuristic: heredoc syntax is unsupported in cmd.exe.
	return strings.Contains(command, "<<")
}

// ---------------------------------------------------------------------------
// grep
// ---------------------------------------------------------------------------

func grepTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "grep",
			Description: "Search file contents for a regex pattern. Returns matching lines as file:line:content. Use include to filter by extension (e.g. '*.go'). Use context_lines for surrounding context. Only call once per query — do not repeat with the same pattern.",
			Properties: map[string]provider.ToolProp{
				"pattern":       {Type: "string", Description: "Regular expression pattern to search for"},
				"path":          {Type: "string", Description: "Directory or file to search (default: current directory)"},
				"include":       {Type: "string", Description: "Glob pattern to filter files (e.g. '*.go', '*.js')"},
				"context_lines": {Type: "integer", Description: "Number of lines to show before and after each match (like grep -C)"},
			},
			Required: []string{"pattern"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			pattern, ok := input["pattern"].(string)
			if !ok || pattern == "" {
				return "", fmt.Errorf("pattern is required")
			}

			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", fmt.Errorf("invalid regex: %w", err)
			}

			searchPath := "."
			if v, ok := input["path"].(string); ok && v != "" {
				searchPath = v
			}

			include := ""
			if v, ok := input["include"].(string); ok {
				include = v
			}

			contextLines := 0
			if v, ok := input["context_lines"].(float64); ok && v > 0 {
				contextLines = int(v)
				if contextLines > 10 {
					contextLines = 10
				}
			}

			var matches []string
			const maxMatches = 200

			errLimitReached := fmt.Errorf("limit reached")

			walkErr := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}

				if d.IsDir() {
					name := d.Name()
					if strings.HasPrefix(name, ".") && name != "." {
						return filepath.SkipDir
					}
					return nil
				}

				if strings.HasPrefix(d.Name(), ".") {
					return nil
				}

				if include != "" {
					matched, _ := filepath.Match(include, d.Name())
					if !matched {
						return nil
					}
				}

				info, err := d.Info()
				if err != nil || info.Size() > 1024*1024 {
					return nil
				}

				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}

				if IsBinary(data) {
					return nil
				}

				lines := strings.Split(string(data), "\n")

				if contextLines == 0 {
					for i, line := range lines {
						if re.MatchString(line) {
							matches = append(matches, fmt.Sprintf("%s:%d:%s", path, i+1, line))
							if len(matches) >= maxMatches {
								return errLimitReached
							}
						}
					}
					return nil
				}

				// Context lines mode: collect matching line indices, then
				// emit groups with context and -- separators.
				var matchIndices []int
				for i, line := range lines {
					if re.MatchString(line) {
						matchIndices = append(matchIndices, i)
					}
				}
				if len(matchIndices) == 0 {
					return nil
				}

				// Build set of lines to show.
				show := make(map[int]bool)
				for _, idx := range matchIndices {
					lo := idx - contextLines
					if lo < 0 {
						lo = 0
					}
					hi := idx + contextLines
					if hi >= len(lines) {
						hi = len(lines) - 1
					}
					for j := lo; j <= hi; j++ {
						show[j] = true
					}
				}

				// Emit lines in order, inserting -- between non-contiguous groups.
				prevEmitted := -2
				matchSet := make(map[int]bool, len(matchIndices))
				for _, idx := range matchIndices {
					matchSet[idx] = true
				}
				for i := 0; i < len(lines); i++ {
					if !show[i] {
						continue
					}
					if prevEmitted >= 0 && i > prevEmitted+1 {
						matches = append(matches, "--")
					}
					prefix := " "
					if matchSet[i] {
						prefix = ":"
					}
					matches = append(matches, fmt.Sprintf("%s:%d%s%s", path, i+1, prefix, lines[i]))
					prevEmitted = i
					if len(matches) >= maxMatches {
						return errLimitReached
					}
				}

				return nil
			})

			if len(matches) == 0 {
				return "No matches found.", nil
			}

			result := strings.Join(matches, "\n")
			if walkErr == errLimitReached {
				result += fmt.Sprintf("\n... (truncated at %d matches)", maxMatches)
			}
			return result, nil
		},
	}
}

// IsDeniedConfigFile checks if a path resolves to a config file that contains
// secrets (config.json) and should not be readable by the agent.
func IsDeniedConfigFile(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absPath = filepath.Clean(absPath)

	// Check config.json in config dir
	configPath := config.ConfigFilePath()
	if configPath != "" {
		if absPath == filepath.Clean(configPath) {
			return true
		}
	}

	// Check config.json in current working directory
	cwd, _ := Getwd()
	configInCwd := filepath.Clean(filepath.Join(cwd, "config.json"))
	if absPath == configInCwd {
		return true
	}

	return false
}

// IsBinary reports whether data looks like binary content.
func IsBinary(data []byte) bool {
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// list_files
// ---------------------------------------------------------------------------

// hiddenDirs is the set of directory names to skip during listing/walking.
var hiddenDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".idea": true, ".vscode": true,
	"node_modules": true, "__pycache__": true, ".DS_Store": true,
}

func listFilesTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "list_files",
			Description: "List files and directories in a path. Use to explore project structure before reading files. Directories have a / suffix. Skips .git, node_modules, and other generated directories.",
			Properties: map[string]provider.ToolProp{
				"path":      {Type: "string", Description: "Directory path to list (default: current directory)"},
				"recursive": {Type: "boolean", Description: "List files recursively (default: false)"},
				"include":   {Type: "string", Description: "Glob pattern to filter file names (e.g. '*.go')"},
			},
			Required: []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			dirPath := "."
			if v, ok := input["path"].(string); ok && v != "" {
				dirPath = v
			}

			recursive := false
			if v, ok := input["recursive"].(bool); ok {
				recursive = v
			}

			include := ""
			if v, ok := input["include"].(string); ok {
				include = v
			}

			const maxEntries = 500

			// Check if path contains glob metacharacters.
			if strings.ContainsAny(dirPath, "*?[") {
				return listFilesGlob(dirPath, maxEntries)
			}

			info, err := os.Stat(dirPath)
			if err != nil {
				return "", fmt.Errorf("stat %s: %w", dirPath, err)
			}
			if !info.IsDir() {
				return "", fmt.Errorf("%s is not a directory", dirPath)
			}

			if !recursive {
				return listFilesFlat(dirPath, include, maxEntries)
			}
			return listFilesRecursive(dirPath, include, maxEntries)
		},
	}
}

func listFilesFlat(dirPath, include string, maxEntries int) (string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", fmt.Errorf("reading directory %s: %w", dirPath, err)
	}

	var results []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if hiddenDirs[name] {
			continue
		}
		if include != "" && !e.IsDir() {
			matched, _ := filepath.Match(include, name)
			if !matched {
				continue
			}
		}
		if e.IsDir() {
			results = append(results, name+"/")
		} else {
			results = append(results, name)
		}
		if len(results) >= maxEntries {
			break
		}
	}

	if len(results) == 0 {
		return "No entries found.", nil
	}
	result := strings.Join(results, "\n")
	if len(results) >= maxEntries {
		result += fmt.Sprintf("\n... (truncated at %d entries)", maxEntries)
	}
	return result, nil
}

func listFilesRecursive(dirPath, include string, maxEntries int) (string, error) {
	var results []string
	errLimit := fmt.Errorf("limit")

	walkErr := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if (strings.HasPrefix(name, ".") && name != ".") || hiddenDirs[name] {
				return filepath.SkipDir
			}
			// Skip the root directory itself.
			if path == dirPath {
				return nil
			}
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}

		rel, _ := filepath.Rel(dirPath, path)
		if rel == "." {
			return nil
		}
		// Normalize to forward slashes for consistent output.
		rel = filepath.ToSlash(rel)

		if include != "" && !d.IsDir() {
			matched, _ := filepath.Match(include, name)
			if !matched {
				return nil
			}
		}

		if d.IsDir() {
			results = append(results, rel+"/")
		} else {
			results = append(results, rel)
		}
		if len(results) >= maxEntries {
			return errLimit
		}
		return nil
	})

	if len(results) == 0 {
		return "No entries found.", nil
	}

	sort.Strings(results)
	result := strings.Join(results, "\n")
	if walkErr == errLimit {
		result += fmt.Sprintf("\n... (truncated at %d entries)", maxEntries)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// ask_user
// ---------------------------------------------------------------------------

func askUserTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "ask_user",
			Description: "Ask the user a question and wait for their response. Use when you need clarification, a decision, or confirmation before proceeding. The agent loop will pause until the user replies.",
			Properties: map[string]provider.ToolProp{
				"question": {Type: "string", Description: "The question to ask the user"},
			},
			Required: []string{"question"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			// ask_user is special-cased in the agent loop and never executed directly.
			return "", fmt.Errorf("ask_user must be handled by the agent loop")
		},
	}
}

func listFilesGlob(pattern string, maxEntries int) (string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid glob pattern: %w", err)
	}
	if len(matches) == 0 {
		return "No entries found.", nil
	}

	var results []string
	for _, m := range matches {
		name := filepath.Base(m)
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		entry := filepath.ToSlash(m)
		if info.IsDir() {
			entry += "/"
		}
		results = append(results, entry)
		if len(results) >= maxEntries {
			break
		}
	}

	if len(results) == 0 {
		return "No entries found.", nil
	}
	result := strings.Join(results, "\n")
	if len(results) >= maxEntries {
		result += fmt.Sprintf("\n... (truncated at %d entries)", maxEntries)
	}
	return result, nil
}
