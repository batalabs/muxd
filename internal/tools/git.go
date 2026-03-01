package tools

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// git_status
// ---------------------------------------------------------------------------

func gitStatusTool() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "git_status",
			Description: "Show modified, staged, and untracked files in the git repo. Returns short-format status. Only call once per turn — do not repeat if you already have the result.",
			Properties: map[string]provider.ToolProp{
				"path": {Type: "string", Description: "Directory path (default: cwd)"},
			},
			Required: []string{},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			p := ctx.Cwd
			if v, ok := input["path"].(string); ok && v != "" {
				p = v
			}
			cmd := exec.Command("git", "status", "-s")
			cmd.Dir = p
			out, err := cmd.CombinedOutput()
			s := string(out)
			if err != nil {
				return fmt.Sprintf("git error: %v\\n%s", err, s), nil
			}
			if len(s) > 50*1024 {
				s = s[:50*1024] + "\\n... (truncated)"
			}
			return strings.TrimSpace(s), nil
		},
	}
}
