package tools

import (
	"strings"
	"testing"
)

func TestTodoReadTool(t *testing.T) {
	tool := todoReadTool()

	t.Run("nil context returns no list", func(t *testing.T) {
		result, err := tool.Execute(map[string]any{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "No todo list") {
			t.Errorf("expected no list message, got: %s", result)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		ctx := &ToolContext{Todos: &TodoList{}}
		result, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "Todo list is empty." {
			t.Errorf("expected empty message, got: %s", result)
		}
	})

	t.Run("returns items", func(t *testing.T) {
		ctx := &ToolContext{Todos: &TodoList{
			Items: []TodoItem{
				{ID: "1", Title: "Write code", Status: "completed"},
				{ID: "2", Title: "Write tests", Status: "in_progress", Description: "unit tests"},
				{ID: "3", Title: "Deploy", Status: "pending"},
			},
		}}
		result, err := tool.Execute(map[string]any{}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "Write code") {
			t.Errorf("expected Write code in output, got: %s", result)
		}
		if !strings.Contains(result, "in_progress") {
			t.Errorf("expected in_progress in output, got: %s", result)
		}
		if !strings.Contains(result, "unit tests") {
			t.Errorf("expected description in output, got: %s", result)
		}
	})
}

func TestTodoWriteTool(t *testing.T) {
	tool := todoWriteTool()

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := tool.Execute(map[string]any{
			"todos": []any{},
		}, nil)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
	})

	t.Run("writes items", func(t *testing.T) {
		ctx := &ToolContext{Todos: &TodoList{}}
		result, err := tool.Execute(map[string]any{
			"todos": []any{
				map[string]any{"id": "1", "title": "Step 1", "status": "completed"},
				map[string]any{"id": "2", "title": "Step 2", "status": "in_progress"},
				map[string]any{"id": "3", "title": "Step 3", "status": "pending"},
			},
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "3 items") {
			t.Errorf("expected 3 items, got: %s", result)
		}
		if len(ctx.Todos.Items) != 3 {
			t.Errorf("expected 3 items in list, got %d", len(ctx.Todos.Items))
		}
		if ctx.Todos.Items[1].Status != "in_progress" {
			t.Errorf("expected second item in_progress, got %s", ctx.Todos.Items[1].Status)
		}
	})

	t.Run("rejects multiple in_progress", func(t *testing.T) {
		ctx := &ToolContext{Todos: &TodoList{}}
		_, err := tool.Execute(map[string]any{
			"todos": []any{
				map[string]any{"id": "1", "title": "A", "status": "in_progress"},
				map[string]any{"id": "2", "title": "B", "status": "in_progress"},
			},
		}, ctx)
		if err == nil {
			t.Fatal("expected error for multiple in_progress")
		}
		if !strings.Contains(err.Error(), "only one") {
			t.Errorf("expected 'only one' error, got: %v", err)
		}
	})

	t.Run("rejects invalid status", func(t *testing.T) {
		ctx := &ToolContext{Todos: &TodoList{}}
		_, err := tool.Execute(map[string]any{
			"todos": []any{
				map[string]any{"id": "1", "title": "A", "status": "invalid"},
			},
		}, ctx)
		if err == nil {
			t.Fatal("expected error for invalid status")
		}
		if !strings.Contains(err.Error(), "invalid status") {
			t.Errorf("expected 'invalid status' error, got: %v", err)
		}
	})

	t.Run("missing title returns error", func(t *testing.T) {
		ctx := &ToolContext{Todos: &TodoList{}}
		_, err := tool.Execute(map[string]any{
			"todos": []any{
				map[string]any{"id": "1", "status": "pending"},
			},
		}, ctx)
		if err == nil {
			t.Fatal("expected error for missing title")
		}
	})

	t.Run("overwrites previous list", func(t *testing.T) {
		ctx := &ToolContext{Todos: &TodoList{
			Items: []TodoItem{
				{ID: "old", Title: "Old task", Status: "pending"},
			},
		}}
		_, err := tool.Execute(map[string]any{
			"todos": []any{
				map[string]any{"id": "new", "title": "New task", "status": "pending"},
			},
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ctx.Todos.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(ctx.Todos.Items))
		}
		if ctx.Todos.Items[0].ID != "new" {
			t.Errorf("expected new item, got %s", ctx.Todos.Items[0].ID)
		}
	})

	t.Run("empty list clears todos", func(t *testing.T) {
		ctx := &ToolContext{Todos: &TodoList{
			Items: []TodoItem{{ID: "1", Title: "X", Status: "pending"}},
		}}
		result, err := tool.Execute(map[string]any{
			"todos": []any{},
		}, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "0 items") {
			t.Errorf("expected 0 items, got: %s", result)
		}
		if len(ctx.Todos.Items) != 0 {
			t.Errorf("expected empty list, got %d items", len(ctx.Todos.Items))
		}
	})
}
