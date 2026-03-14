package tools

import (
	"sort"
	"strings"
	"testing"

	"github.com/batalabs/muxd/internal/provider"
)

func TestCustomToolRegistry_RegisterAndFind(t *testing.T) {
	reg := NewCustomToolRegistry()
	def := &CustomToolDef{
		Name:        "my_tool",
		Description: "does something",
		Command:     "echo hello",
	}
	if err := reg.Register(def); err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}
	got := reg.Find("my_tool")
	if got == nil {
		t.Fatal("Find returned nil for registered tool")
	}
	if got.Name != "my_tool" {
		t.Errorf("Find returned wrong tool: got %q, want %q", got.Name, "my_tool")
	}
}

func TestCustomToolRegistry_FindUnknown(t *testing.T) {
	reg := NewCustomToolRegistry()
	got := reg.Find("nonexistent")
	if got != nil {
		t.Errorf("Find returned non-nil for unknown tool: %v", got)
	}
}

func TestCustomToolRegistry_All(t *testing.T) {
	reg := NewCustomToolRegistry()
	defs := []*CustomToolDef{
		{Name: "tool_a", Description: "first", Command: "echo a"},
		{Name: "tool_b", Description: "second", Script: "#!/bin/sh\necho b"},
	}
	for _, d := range defs {
		if err := reg.Register(d); err != nil {
			t.Fatalf("Register(%q) error: %v", d.Name, err)
		}
	}
	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d tools, want 2", len(all))
	}
	names := make([]string, len(all))
	for i, d := range all {
		names[i] = d.Name
	}
	sort.Strings(names)
	if names[0] != "tool_a" || names[1] != "tool_b" {
		t.Errorf("All() returned unexpected names: %v", names)
	}
}

func TestCustomToolRegistry_RegisterValidation(t *testing.T) {
	cases := []struct {
		name    string
		def     *CustomToolDef
		wantErr bool
		errSub  string
	}{
		{
			name:    "empty name",
			def:     &CustomToolDef{Name: "", Command: "echo hi"},
			wantErr: true,
			errSub:  "name",
		},
		{
			name:    "invalid chars - starts with digit",
			def:     &CustomToolDef{Name: "1tool", Command: "echo hi"},
			wantErr: true,
			errSub:  "name",
		},
		{
			name:    "invalid chars - hyphen",
			def:     &CustomToolDef{Name: "my-tool", Command: "echo hi"},
			wantErr: true,
			errSub:  "name",
		},
		{
			name:    "too long name - 65 chars",
			def:     &CustomToolDef{Name: strings.Repeat("a", 65), Command: "echo hi"},
			wantErr: true,
			errSub:  "name",
		},
		{
			name:    "no command or script",
			def:     &CustomToolDef{Name: "valid_tool"},
			wantErr: true,
			errSub:  "command",
		},
		{
			name:    "builtin conflict - bash",
			def:     &CustomToolDef{Name: "bash", Command: "echo hi"},
			wantErr: true,
			errSub:  "built-in",
		},
		{
			name:    "valid command",
			def:     &CustomToolDef{Name: "my_cmd", Command: "echo hello"},
			wantErr: false,
		},
		{
			name:    "valid script",
			def:     &CustomToolDef{Name: "my_script", Script: "#!/bin/sh\necho hi"},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewCustomToolRegistry()
			err := reg.Register(tc.def)
			if tc.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tc.wantErr && err != nil && tc.errSub != "" {
				if !strings.Contains(strings.ToLower(err.Error()), tc.errSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errSub)
				}
			}
		})
	}

	t.Run("duplicate name", func(t *testing.T) {
		reg := NewCustomToolRegistry()
		def := &CustomToolDef{Name: "dupe_tool", Command: "echo 1"}
		if err := reg.Register(def); err != nil {
			t.Fatalf("first Register failed: %v", err)
		}
		def2 := &CustomToolDef{Name: "dupe_tool", Command: "echo 2"}
		err := reg.Register(def2)
		if err == nil {
			t.Error("expected error for duplicate name, got nil")
		}
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already") {
			t.Errorf("error %q does not mention duplicate", err.Error())
		}
	})
}

func TestCustomToolRegistry_Specs(t *testing.T) {
	reg := NewCustomToolRegistry()
	def := &CustomToolDef{
		Name:        "greet",
		Description: "greets someone",
		Parameters: map[string]provider.ToolProp{
			"name": {Type: "string", Description: "the name to greet"},
		},
		Required: []string{"name"},
		Command:  "echo hello",
	}
	if err := reg.Register(def); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	specs := reg.Specs()
	if len(specs) != 1 {
		t.Fatalf("Specs() returned %d specs, want 1", len(specs))
	}

	spec := specs[0]
	if spec.Name != "greet" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "greet")
	}
	if spec.Description != "greets someone" {
		t.Errorf("spec.Description = %q, want %q", spec.Description, "greets someone")
	}
	if len(spec.Properties) != 1 {
		t.Errorf("spec.Properties len = %d, want 1", len(spec.Properties))
	}
	prop, ok := spec.Properties["name"]
	if !ok {
		t.Error("spec.Properties missing 'name' key")
	} else {
		if prop.Type != "string" {
			t.Errorf("prop.Type = %q, want %q", prop.Type, "string")
		}
		if prop.Description != "the name to greet" {
			t.Errorf("prop.Description = %q, want %q", prop.Description, "the name to greet")
		}
	}
	if len(spec.Required) != 1 || spec.Required[0] != "name" {
		t.Errorf("spec.Required = %v, want [name]", spec.Required)
	}
}
