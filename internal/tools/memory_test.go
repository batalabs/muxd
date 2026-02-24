package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ProjectMemory — Load / Save
// ---------------------------------------------------------------------------

func TestProjectMemory_LoadSave(t *testing.T) {
	t.Run("roundtrip save and load", func(t *testing.T) {
		dir := t.TempDir()
		m := NewProjectMemory(dir)

		facts := map[string]string{
			"auth":     "JWT tokens",
			"database": "SQLite WAL",
		}
		if err := m.Save(facts); err != nil {
			t.Fatalf("Save: %v", err)
		}

		got, err := m.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if got["auth"] != "JWT tokens" || got["database"] != "SQLite WAL" {
			t.Errorf("roundtrip mismatch: %v", got)
		}
	})

	t.Run("missing file returns empty map", func(t *testing.T) {
		dir := t.TempDir()
		m := NewProjectMemory(dir)

		got, err := m.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty map, got: %v", got)
		}
	})

	t.Run("creates .muxd directory", func(t *testing.T) {
		dir := t.TempDir()
		m := NewProjectMemory(dir)

		if err := m.Save(map[string]string{"key": "val"}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		info, err := os.Stat(filepath.Join(dir, ".muxd"))
		if err != nil {
			t.Fatalf("expected .muxd dir: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected .muxd to be a directory")
		}
	})

	t.Run("atomic write produces valid JSON", func(t *testing.T) {
		dir := t.TempDir()
		m := NewProjectMemory(dir)

		if err := m.Save(map[string]string{"k": "v"}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(dir, ".muxd", "memory.json"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		var mf struct {
			Facts map[string]string `json:"facts"`
		}
		if err := json.Unmarshal(data, &mf); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if mf.Facts["k"] != "v" {
			t.Errorf("expected k=v, got: %v", mf.Facts)
		}
	})

	t.Run("overwrite existing facts", func(t *testing.T) {
		dir := t.TempDir()
		m := NewProjectMemory(dir)

		m.Save(map[string]string{"a": "1"})
		m.Save(map[string]string{"b": "2"})

		got, _ := m.Load()
		if _, ok := got["a"]; ok {
			t.Error("expected key 'a' to be gone after overwrite")
		}
		if got["b"] != "2" {
			t.Errorf("expected b=2, got: %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// ProjectMemory — FormatForPrompt
// ---------------------------------------------------------------------------

func TestProjectMemory_FormatForPrompt(t *testing.T) {
	t.Run("empty returns empty string", func(t *testing.T) {
		dir := t.TempDir()
		m := NewProjectMemory(dir)

		result := m.FormatForPrompt()
		if result != "" {
			t.Errorf("expected empty string, got: %q", result)
		}
	})

	t.Run("with facts returns sorted key-value lines", func(t *testing.T) {
		dir := t.TempDir()
		m := NewProjectMemory(dir)
		m.Save(map[string]string{
			"database": "SQLite",
			"auth":     "JWT",
		})

		result := m.FormatForPrompt()
		if !strings.HasPrefix(result, "auth: JWT") {
			t.Errorf("expected auth first (sorted), got: %q", result)
		}
		if !strings.Contains(result, "database: SQLite") {
			t.Errorf("expected database line, got: %q", result)
		}
	})
}

// ---------------------------------------------------------------------------
// memory_read tool
// ---------------------------------------------------------------------------

func TestMemoryReadTool(t *testing.T) {
	tool := memoryReadTool()

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{}, nil)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
	})

	t.Run("nil memory returns error", func(t *testing.T) {
		ctx := &ToolContext{}
		_, err := tool.Execute(map[string]any{}, ctx)
		if err == nil {
			t.Fatal("expected error for nil memory")
		}
	})

	t.Run("empty memory", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		result, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "No project memory") {
			t.Errorf("expected empty message, got: %s", result)
		}
	})

	t.Run("with facts", func(t *testing.T) {
		dir := t.TempDir()
		mem := NewProjectMemory(dir)
		mem.Save(map[string]string{"auth": "JWT", "db": "SQLite"})
		ctx := &ToolContext{Memory: mem}

		result, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "auth: JWT") {
			t.Errorf("expected auth fact, got: %s", result)
		}
		if !strings.Contains(result, "db: SQLite") {
			t.Errorf("expected db fact, got: %s", result)
		}
	})
}

// ---------------------------------------------------------------------------
// memory_write tool
// ---------------------------------------------------------------------------

func TestMemoryWriteTool(t *testing.T) {
	tool := memoryWriteTool()

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"action": "set", "key": "k", "value": "v",
		}, nil)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
	})

	t.Run("set a fact", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		result, err := tool.Execute(map[string]any{
			"action": "set", "key": "auth", "value": "JWT tokens",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Saved") {
			t.Errorf("expected Saved message, got: %s", result)
		}

		facts, _ := ctx.Memory.Load()
		if facts["auth"] != "JWT tokens" {
			t.Errorf("expected auth=JWT tokens, got: %v", facts)
		}
	})

	t.Run("overwrite existing key", func(t *testing.T) {
		dir := t.TempDir()
		mem := NewProjectMemory(dir)
		mem.Save(map[string]string{"auth": "old"})
		ctx := &ToolContext{Memory: mem}

		_, err := tool.Execute(map[string]any{
			"action": "set", "key": "auth", "value": "new",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		facts, _ := ctx.Memory.Load()
		if facts["auth"] != "new" {
			t.Errorf("expected auth=new, got: %v", facts)
		}
	})

	t.Run("remove existing key", func(t *testing.T) {
		dir := t.TempDir()
		mem := NewProjectMemory(dir)
		mem.Save(map[string]string{"auth": "JWT", "db": "SQLite"})
		ctx := &ToolContext{Memory: mem}

		result, err := tool.Execute(map[string]any{
			"action": "remove", "key": "auth",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Removed") {
			t.Errorf("expected Removed message, got: %s", result)
		}

		facts, _ := ctx.Memory.Load()
		if _, ok := facts["auth"]; ok {
			t.Error("expected auth to be removed")
		}
		if facts["db"] != "SQLite" {
			t.Error("expected db to remain")
		}
	})

	t.Run("remove nonexistent key", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		result, err := tool.Execute(map[string]any{
			"action": "remove", "key": "missing",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "not found") {
			t.Errorf("expected 'not found' message, got: %s", result)
		}
	})

	t.Run("invalid action returns error", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		_, err := tool.Execute(map[string]any{
			"action": "delete", "key": "k",
		}, ctx)
		if err == nil {
			t.Fatal("expected error for invalid action")
		}
		if !strings.Contains(err.Error(), "invalid action") {
			t.Errorf("expected 'invalid action' error, got: %v", err)
		}
	})

	t.Run("empty key returns error", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		_, err := tool.Execute(map[string]any{
			"action": "set", "key": "", "value": "v",
		}, ctx)
		if err == nil {
			t.Fatal("expected error for empty key")
		}
	})

	t.Run("set without value returns error", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		_, err := tool.Execute(map[string]any{
			"action": "set", "key": "k",
		}, ctx)
		if err == nil {
			t.Fatal("expected error for set without value")
		}
	})
}
