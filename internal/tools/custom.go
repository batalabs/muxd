package tools

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/batalabs/muxd/internal/provider"
)

// validToolName matches names that start with a letter and contain only
// letters, digits, and underscores, with a maximum length of 64 characters.
var validToolName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

// CustomToolDef describes a user-defined tool backed by a shell command or
// inline script.
type CustomToolDef struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Parameters  map[string]provider.ToolProp `json:"parameters,omitempty"`
	Required    []string                    `json:"required,omitempty"`
	Command     string                      `json:"command,omitempty"`
	Script      string                      `json:"script,omitempty"`
	Persistent  bool                        `json:"-"`
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
