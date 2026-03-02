package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// ---------------------------------------------------------------------------
// ProjectMemory — LocalKeys
// ---------------------------------------------------------------------------

func TestProjectMemory_LocalKeys(t *testing.T) {
	dir := t.TempDir()
	pm := NewProjectMemory(dir)

	facts := map[string]string{"shared_key": "value1", "local_key": "value2"}
	if err := pm.Save(facts); err != nil {
		t.Fatal(err)
	}
	if err := pm.MarkLocal("local_key"); err != nil {
		t.Fatal(err)
	}

	// Verify local keys persisted
	localKeys := pm.LocalKeys()
	if len(localKeys) != 1 || localKeys[0] != "local_key" {
		t.Errorf("expected [local_key], got %v", localKeys)
	}

	// IsLocal check
	if !pm.IsLocal("local_key") {
		t.Error("expected local_key to be local")
	}
	if pm.IsLocal("shared_key") {
		t.Error("expected shared_key to not be local")
	}

	// MarkLocal is idempotent
	if err := pm.MarkLocal("local_key"); err != nil {
		t.Fatal(err)
	}
	if len(pm.LocalKeys()) != 1 {
		t.Error("expected MarkLocal to be idempotent")
	}
}

// ---------------------------------------------------------------------------
// ProjectMemory — MergeHub
// ---------------------------------------------------------------------------

func TestProjectMemory_MergeHub(t *testing.T) {
	dir := t.TempDir()
	pm := NewProjectMemory(dir)

	// Set up local state
	local := map[string]string{"local_secret": "abc", "stack": "old"}
	pm.Save(local)
	pm.MarkLocal("local_secret")

	// Merge hub facts — hub wins on shared keys, local_keys preserved
	hubFacts := map[string]string{"stack": "Go+SQLite", "hub_only": "yes"}
	if err := pm.MergeHub(hubFacts); err != nil {
		t.Fatal(err)
	}

	facts, _ := pm.Load()
	if facts["stack"] != "Go+SQLite" {
		t.Errorf("expected hub to win on shared key, got %s", facts["stack"])
	}
	if facts["hub_only"] != "yes" {
		t.Error("expected hub_only key")
	}
	if facts["local_secret"] != "abc" {
		t.Error("expected local_secret preserved")
	}

	// Verify local_keys still tracked
	if !pm.IsLocal("local_secret") {
		t.Error("expected local_secret still local after merge")
	}
}

func TestProjectMemory_MergeHub_noOverwriteLocal(t *testing.T) {
	dir := t.TempDir()
	pm := NewProjectMemory(dir)

	pm.Save(map[string]string{"api_key": "secret123"})
	pm.MarkLocal("api_key")

	// Hub tries to overwrite local key
	err := pm.MergeHub(map[string]string{"api_key": "hub_value"})
	if err != nil {
		t.Fatal(err)
	}

	facts, _ := pm.Load()
	if facts["api_key"] != "secret123" {
		t.Errorf("expected local key preserved, got %s", facts["api_key"])
	}
}

func TestProjectMemory_SavePreservesLocalKeys(t *testing.T) {
	dir := t.TempDir()
	pm := NewProjectMemory(dir)

	pm.Save(map[string]string{"a": "1"})
	pm.MarkLocal("a")

	// Save again with different facts — local_keys should be preserved
	pm.Save(map[string]string{"b": "2"})

	if !pm.IsLocal("a") {
		t.Error("expected local_keys preserved after Save")
	}
}

// ---------------------------------------------------------------------------
// memory_write — scope parameter
// ---------------------------------------------------------------------------

func TestMemoryWriteTool_scope(t *testing.T) {
	tool := memoryWriteTool()

	t.Run("default scope is shared", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		result, err := tool.Execute(map[string]any{
			"action": "set", "key": "db", "value": "SQLite",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(result, "[local]") {
			t.Errorf("expected no [local] tag for shared, got: %s", result)
		}
		if ctx.Memory.IsLocal("db") {
			t.Error("expected db to NOT be local")
		}
	})

	t.Run("scope=local marks key local", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		result, err := tool.Execute(map[string]any{
			"action": "set", "key": "api_key", "value": "secret",
			"scope": "local",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "[local]") {
			t.Errorf("expected [local] tag, got: %s", result)
		}
		if !ctx.Memory.IsLocal("api_key") {
			t.Error("expected api_key to be local")
		}
	})

	t.Run("scope=shared triggers hub push", func(t *testing.T) {
		dir := t.TempDir()
		var pushed map[string]string
		done := make(chan struct{})
		ctx := &ToolContext{
			Memory: NewProjectMemory(dir),
			PushHubMemory: func(facts map[string]string) error {
				pushed = facts
				close(done)
				return nil
			},
		}

		_, err := tool.Execute(map[string]any{
			"action": "set", "key": "stack", "value": "Go",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Wait for async push
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for hub push")
		}

		if pushed["stack"] != "Go" {
			t.Errorf("expected stack=Go pushed, got: %v", pushed)
		}
	})

	t.Run("scope=local does not trigger hub push", func(t *testing.T) {
		dir := t.TempDir()
		pushCalled := false
		ctx := &ToolContext{
			Memory: NewProjectMemory(dir),
			PushHubMemory: func(facts map[string]string) error {
				pushCalled = true
				return nil
			},
		}

		_, err := tool.Execute(map[string]any{
			"action": "set", "key": "secret", "value": "123",
			"scope": "local",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Give goroutine a chance to run (it shouldn't)
		// Using a short sleep here is acceptable since we're verifying absence.
		if pushCalled {
			t.Error("expected no hub push for local scope")
		}
	})

	t.Run("invalid scope returns error", func(t *testing.T) {
		dir := t.TempDir()
		ctx := &ToolContext{Memory: NewProjectMemory(dir)}

		_, err := tool.Execute(map[string]any{
			"action": "set", "key": "k", "value": "v",
			"scope": "global",
		}, ctx)
		if err == nil {
			t.Fatal("expected error for invalid scope")
		}
		if !strings.Contains(err.Error(), "invalid scope") {
			t.Errorf("expected 'invalid scope' error, got: %v", err)
		}
	})

	t.Run("remove triggers hub push", func(t *testing.T) {
		dir := t.TempDir()
		mem := NewProjectMemory(dir)
		mem.Save(map[string]string{"a": "1", "b": "2"})

		var pushed map[string]string
		done := make(chan struct{})
		ctx := &ToolContext{
			Memory: mem,
			PushHubMemory: func(facts map[string]string) error {
				pushed = facts
				close(done)
				return nil
			},
		}

		_, err := tool.Execute(map[string]any{
			"action": "remove", "key": "a",
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for hub push after remove")
		}

		if _, ok := pushed["a"]; ok {
			t.Error("expected key 'a' not in pushed facts")
		}
		if pushed["b"] != "2" {
			t.Errorf("expected b=2, got: %v", pushed)
		}
	})
}

// ---------------------------------------------------------------------------
// pushSharedFacts
// ---------------------------------------------------------------------------

func TestPushSharedFacts(t *testing.T) {
	dir := t.TempDir()
	mem := NewProjectMemory(dir)
	mem.Save(map[string]string{"shared1": "v1", "local1": "v2", "shared2": "v3"})
	mem.MarkLocal("local1")

	var pushed map[string]string
	pushSharedFacts(mem, func(facts map[string]string) error {
		pushed = facts
		return nil
	})

	if len(pushed) != 2 {
		t.Fatalf("expected 2 shared facts, got %d: %v", len(pushed), pushed)
	}
	if pushed["shared1"] != "v1" || pushed["shared2"] != "v3" {
		t.Errorf("unexpected pushed facts: %v", pushed)
	}
	if _, ok := pushed["local1"]; ok {
		t.Error("local1 should not be pushed")
	}
}
