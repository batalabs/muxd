package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/batalabs/muxd/internal/provider"
)

// validToolName matches names that start with a letter and contain only
// letters, digits, and underscores, with a maximum length of 64 characters.
var validToolName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// CustomToolDef describes a user-defined tool backed by a shell command or
// inline script.
type CustomToolDef struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Parameters  map[string]provider.ToolProp `json:"parameters,omitempty"`
	Required    []string                     `json:"required,omitempty"`
	Command     string                       `json:"command,omitempty"`
	Script      string                       `json:"script,omitempty"`
	Persistent  bool                         `json:"-"`
}

// ToSpec converts the definition to a provider-agnostic ToolSpec.
func (d *CustomToolDef) ToSpec() provider.ToolSpec {
	props := make(map[string]provider.ToolProp, len(d.Parameters))
	for k, v := range d.Parameters {
		props[k] = v
	}
	req := make([]string, len(d.Required))
	copy(req, d.Required)
	return provider.ToolSpec{
		Name:        d.Name,
		Description: d.Description,
		Properties:  props,
		Required:    req,
	}
}

// CustomToolRegistry stores user-defined tools keyed by name.
type CustomToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]*CustomToolDef
}

// NewCustomToolRegistry returns an empty, ready-to-use registry.
func NewCustomToolRegistry() *CustomToolRegistry {
	return &CustomToolRegistry{
		tools: make(map[string]*CustomToolDef),
	}
}

// Register validates and adds def to the registry.
// It returns an error if:
//   - the name is empty or does not match ^[a-zA-Z][a-zA-Z0-9_]{0,63}$
//   - neither Command nor Script is set
//   - the name conflicts with a built-in tool
//   - a custom tool with the same name is already registered
func (r *CustomToolRegistry) Register(def *CustomToolDef) error {
	if def.Name == "" || !validToolName.MatchString(def.Name) {
		return fmt.Errorf("invalid tool name %q: must match ^[a-zA-Z][a-zA-Z0-9_]{0,63}$", def.Name)
	}
	if def.Command == "" && def.Script == "" {
		return fmt.Errorf("custom tool %q: command or script is required", def.Name)
	}
	if _, ok := FindTool(def.Name); ok {
		return fmt.Errorf("custom tool %q conflicts with a built-in tool", def.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("custom tool %q already registered", def.Name)
	}
	r.tools[def.Name] = def
	return nil
}

// Find returns the CustomToolDef with the given name, or nil if not found.
func (r *CustomToolRegistry) Find(name string) *CustomToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// All returns all registered custom tool definitions.
func (r *CustomToolRegistry) All() []*CustomToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*CustomToolDef, 0, len(r.tools))
	for _, d := range r.tools {
		out = append(out, d)
	}
	return out
}

// Specs converts all registered custom tools into ToolSpecs.
func (r *CustomToolRegistry) Specs() []provider.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]provider.ToolSpec, 0, len(r.tools))
	for _, d := range r.tools {
		specs = append(specs, d.ToSpec())
	}
	return specs
}

// substituteParams replaces {{name}} placeholders in tmpl with the
// corresponding values from params. Values that contain shell special
// characters are wrapped in single quotes with internal single quotes
// escaped as '\”. Placeholders with no matching key are left as-is.
func substituteParams(tmpl string, params map[string]any) string {
	return placeholderRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := match[2 : len(match)-2] // strip {{ and }}
		val, ok := params[key]
		if !ok {
			return match
		}
		s := fmt.Sprintf("%v", val)
		return shellEscape(s)
	})
}

// placeholderRe matches {{identifier}} tokens.
var placeholderRe = regexp.MustCompile(`\{\{[^}]+\}\}`)

// shellEscape returns s unchanged if it contains no shell special characters,
// otherwise wraps it in single quotes, escaping embedded single quotes as '\”.
func shellEscape(s string) string {
	if !strings.ContainsAny(s, " \t\n!\"#$&'()*,;<=>?[\\]^`{|}~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// paramsToEnv converts a params map to PARAM_NAME=value environment variable
// strings suitable for use with exec.Cmd.Env. Keys are upper-cased and
// prefixed with PARAM_.
func paramsToEnv(params map[string]any) []string {
	env := make([]string, 0, len(params))
	for k, v := range params {
		env = append(env, "PARAM_"+strings.ToUpper(k)+"="+fmt.Sprintf("%v", v))
	}
	return env
}

// Execute finds the named tool, substitutes params into its command (or sets
// environment variables for script tools), and runs it via the system shell
// with a 30-second timeout. It returns stdout on success, or an error
// containing stderr on non-zero exit.
func (r *CustomToolRegistry) Execute(name string, input map[string]any, cwd string) (string, error) {
	def := r.Find(name)
	if def == nil {
		return "", fmt.Errorf("custom tool %q not found", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if def.Command != "" {
		cmdStr := substituteParams(def.Command, input)
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/C", cmdStr)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", cmdStr)
		}
	} else {
		// Script: pass params as environment variables.
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/C", def.Script)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", def.Script)
		}
		cmd.Env = append(cmd.Environ(), paramsToEnv(input)...)
	}

	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// CustomToolsDir returns the path to the user's custom tools directory:
// ~/.config/muxd/tools/
func CustomToolsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	return filepath.Join(home, ".config", "muxd", "tools"), nil
}

// SaveTool writes def as an indented JSON file to <dir>/<name>.json,
// creating dir if it does not exist.
func SaveTool(dir string, def *CustomToolDef) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating tools dir: %w", err)
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tool %q: %w", def.Name, err)
	}
	dest := filepath.Join(dir, def.Name+".json")
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("writing tool file %q: %w", dest, err)
	}
	return nil
}

// LoadFromDir scans dir for *.json files, parses each as a CustomToolDef,
// marks it Persistent, resolves relative Script paths against dir, and
// registers it. Invalid or duplicate files are silently skipped.
// Returns nil if dir does not exist.
func (r *CustomToolRegistry) LoadFromDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading tools dir %q: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var def CustomToolDef
		if err := json.Unmarshal(data, &def); err != nil {
			continue
		}
		def.Persistent = true
		if def.Script != "" && !filepath.IsAbs(def.Script) {
			def.Script = filepath.Join(dir, def.Script)
		}
		// Silently ignore invalid or duplicate registrations.
		_ = r.Register(&def)
	}
	return nil
}
