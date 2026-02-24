package mcp

import (
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToToolSpec_SimpleProperties(t *testing.T) {
	tool := &mcpsdk.Tool{
		Name:        "read_file",
		Description: "Read a file from disk",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path to read",
				},
				"encoding": map[string]any{
					"type":        "string",
					"description": "File encoding",
					"enum":        []any{"utf-8", "ascii", "latin-1"},
				},
			},
			"required": []any{"path"},
		},
	}

	spec := ToToolSpec("fs", tool)

	if spec.Name != "mcp__fs__read_file" {
		t.Errorf("name = %q, want %q", spec.Name, "mcp__fs__read_file")
	}
	if spec.Description != "Read a file from disk" {
		t.Errorf("description = %q", spec.Description)
	}
	if len(spec.Properties) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(spec.Properties))
	}

	pathProp := spec.Properties["path"]
	if pathProp.Type != "string" {
		t.Errorf("path type = %q, want string", pathProp.Type)
	}
	if pathProp.Description != "File path to read" {
		t.Errorf("path description = %q", pathProp.Description)
	}

	encProp := spec.Properties["encoding"]
	if len(encProp.Enum) != 3 {
		t.Errorf("encoding enum len = %d, want 3", len(encProp.Enum))
	}

	if len(spec.Required) != 1 || spec.Required[0] != "path" {
		t.Errorf("required = %v, want [path]", spec.Required)
	}
}

func TestToToolSpec_NestedObject(t *testing.T) {
	tool := &mcpsdk.Tool{
		Name:        "create_item",
		Description: "Create an item",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"metadata": map[string]any{
					"type":        "object",
					"description": "Item metadata",
					"properties": map[string]any{
						"key": map[string]any{
							"type": "string",
						},
						"value": map[string]any{
							"type": "string",
						},
					},
					"required": []any{"key"},
				},
			},
		},
	}

	spec := ToToolSpec("api", tool)

	meta, ok := spec.Properties["metadata"]
	if !ok {
		t.Fatal("metadata property not found")
	}
	if meta.Type != "object" {
		t.Errorf("metadata type = %q, want object", meta.Type)
	}
	if len(meta.Properties) != 2 {
		t.Errorf("metadata properties len = %d, want 2", len(meta.Properties))
	}
	if len(meta.Required) != 1 || meta.Required[0] != "key" {
		t.Errorf("metadata required = %v, want [key]", meta.Required)
	}
}

func TestToToolSpec_ArrayItems(t *testing.T) {
	tool := &mcpsdk.Tool{
		Name:        "batch",
		Description: "Batch operation",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type":        "array",
					"description": "List of items",
					"items": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	spec := ToToolSpec("batch-svc", tool)

	itemsProp := spec.Properties["items"]
	if itemsProp.Type != "array" {
		t.Errorf("items type = %q, want array", itemsProp.Type)
	}
	if itemsProp.Items == nil {
		t.Fatal("items.Items is nil")
	}
	if itemsProp.Items.Type != "string" {
		t.Errorf("items.Items.Type = %q, want string", itemsProp.Items.Type)
	}
}

func TestToToolSpec_NilInputSchema(t *testing.T) {
	tool := &mcpsdk.Tool{
		Name:        "simple",
		Description: "A tool with no schema",
		InputSchema: nil,
	}

	spec := ToToolSpec("svc", tool)

	if spec.Name != "mcp__svc__simple" {
		t.Errorf("name = %q", spec.Name)
	}
	if spec.Properties != nil {
		t.Errorf("expected nil properties, got %v", spec.Properties)
	}
}

func TestToToolSpec_NoTypeFieldFallsBackToObject(t *testing.T) {
	tool := &mcpsdk.Tool{
		Name:        "complex",
		Description: "Tool with oneOf (unsupported)",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"data": map[string]any{
					// No "type" key — should fallback to "object"
					"description": "Complex data",
				},
			},
		},
	}

	spec := ToToolSpec("svc", tool)

	dataProp := spec.Properties["data"]
	if dataProp.Type != "object" {
		t.Errorf("data type = %q, want object (fallback)", dataProp.Type)
	}
}
