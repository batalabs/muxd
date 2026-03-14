package tools

import (
	"fmt"

	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// consult
// ---------------------------------------------------------------------------

func consultToolDef() ToolDef {
	return ToolDef{
		Spec: provider.ToolSpec{
			Name:        "consult",
			Description: "Ask a different AI model for a second opinion on a problem or approach. Write a focused summary of the problem and your uncertainty. The response is shown to the user in a separate view.",
			Properties: map[string]provider.ToolProp{
				"summary": {Type: "string", Description: "A focused summary of the problem or approach you want a second opinion on"},
			},
			Required: []string{"summary"},
		},
		Execute: func(input map[string]any, ctx *ToolContext) (string, error) {
			summary, ok := input["summary"].(string)
			if !ok || summary == "" {
				return "", fmt.Errorf("summary is required")
			}

			if ctx == nil || ctx.ConsultFunc == nil {
				return "", fmt.Errorf("no consult model configured")
			}

			model, response, err := ctx.ConsultFunc(summary)
			if err != nil {
				return "", fmt.Errorf("consult: %w", err)
			}

			// Send the response to the TUI via the global Prog.
			// Import cycle prevention: the tui package imports tools, so we
			// cannot import tui here. Instead, we call the SendConsultResponse
			// hook if set, which is wired by the daemon layer.
			if SendConsultResponse != nil {
				SendConsultResponse(model, response)
			}

			return "Second opinion delivered to user.", nil
		},
	}
}

// SendConsultResponse is a package-level hook called by the consult tool to
// deliver the response to the UI layer. It is set by the daemon/tui wiring at
// startup to avoid an import cycle between tools and tui packages.
var SendConsultResponse func(model, response string)
