package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// TodoList -- shared in-memory per-session todo list
// ---------------------------------------------------------------------------

// validStatuses is the set of allowed status values.
var validStatuses = map[string]bool{
	"pending":     true,
	"in_progress": true,
	"completed":   true,
}

// ---------------------------------------------------------------------------
// todo_read
// ---------------------------------------------------------------------------

func todoReadTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "todo_read",
			Description: "Read the current todo list. Returns all items with their ID, title, status, and description. Use this to check progress before planning next steps.",
			Properties:  map[string]provider.ToolProp{},
			Required:    []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.Todos == nil {
				return "No todo list available.", nil
			}

			ctx.Todos.mu.Lock()
			items := ctx.Todos.Items
			ctx.Todos.mu.Unlock()

			if len(items) == 0 {
				return "Todo list is empty.", nil
			}

			var b strings.Builder
			for _, item := range items {
				fmt.Fprintf(&b, "[%s] %s — %s", item.ID, item.Status, item.Title)
				if item.Description != "" {
					fmt.Fprintf(&b, " (%s)", item.Description)
				}
				b.WriteString("\n")
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}

// ---------------------------------------------------------------------------
// todo_write
// ---------------------------------------------------------------------------

func todoWriteTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "todo_write",
			Description: "Overwrite the todo list with a new set of items. Each item has an id, title, status (pending/in_progress/completed), and optional description. Only one item may be in_progress at a time. Use this to track multi-step plans.",
			Properties: map[string]provider.ToolProp{
				"todos": {
					Type:        "array",
					Description: "The complete list of todo items.",
					Items: &provider.ToolProp{
						Type: "object",
						Properties: map[string]provider.ToolProp{
							"id":          {Type: "string", Description: "Short unique identifier (e.g. '1', 'a')"},
							"title":       {Type: "string", Description: "Brief task title"},
							"status":      {Type: "string", Description: "One of: pending, in_progress, completed", Enum: []string{"pending", "in_progress", "completed"}},
							"description": {Type: "string", Description: "Optional longer description"},
						},
						Required: []string{"id", "title", "status"},
					},
				},
			},
			Required: []string{"todos"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			if ctx == nil || ctx.Todos == nil {
				return "", fmt.Errorf("todo list not available")
			}

			rawTodos, ok := input["todos"]
			if !ok {
				return "", fmt.Errorf("todos is required")
			}

			// The input is a []any from JSON unmarshalling.
			todosSlice, ok := rawTodos.([]any)
			if !ok {
				return "", fmt.Errorf("todos must be an array")
			}

			items := make([]TodoItem, 0, len(todosSlice))
			inProgressCount := 0

			for i, raw := range todosSlice {
				obj, ok := raw.(map[string]any)
				if !ok {
					return "", fmt.Errorf("todo item %d is not an object", i)
				}

				item := TodoItem{}

				if id, ok := obj["id"].(string); ok {
					item.ID = id
				} else {
					item.ID = fmt.Sprintf("%d", i+1)
				}

				if title, ok := obj["title"].(string); ok {
					item.Title = title
				} else {
					return "", fmt.Errorf("todo item %d: title is required", i)
				}

				if status, ok := obj["status"].(string); ok {
					if !validStatuses[status] {
						return "", fmt.Errorf("todo item %d: invalid status %q (must be pending, in_progress, or completed)", i, status)
					}
					item.Status = status
				} else {
					item.Status = "pending"
				}

				if desc, ok := obj["description"].(string); ok {
					item.Description = desc
				}

				if item.Status == "in_progress" {
					inProgressCount++
				}

				items = append(items, item)
			}

			if inProgressCount > 1 {
				return "", fmt.Errorf("only one item may be in_progress at a time (found %d)", inProgressCount)
			}

			ctx.Todos.mu.Lock()
			ctx.Todos.Items = items
			ctx.Todos.mu.Unlock()

			// Return the list as confirmation.
			data, _ := json.MarshalIndent(items, "", "  ")
			return fmt.Sprintf("Updated todo list (%d items):\n%s", len(items), string(data)), nil
		},
	}
}
