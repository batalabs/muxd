package agent

import (
	"errors"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
	"github.com/batalabs/muxd/internal/provider"
)

// ---------------------------------------------------------------------------
// mockConsultProvider implements provider.Provider for consult tests.
// ---------------------------------------------------------------------------

type mockConsultProvider struct {
	name     string
	response string
	err      error
}

func (m *mockConsultProvider) Name() string { return m.name }

func (m *mockConsultProvider) StreamMessage(
	apiKey, modelID string,
	msgs []domain.TranscriptMessage,
	tools []provider.ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, provider.Usage, error) {
	if m.err != nil {
		return nil, "", provider.Usage{}, m.err
	}
	if onDelta != nil {
		onDelta(m.response)
	}
	return []domain.ContentBlock{{Type: "text", Text: m.response}}, "end_turn", provider.Usage{}, nil
}

func (m *mockConsultProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// TestConsultWithProvider
// ---------------------------------------------------------------------------

func TestConsultWithProvider(t *testing.T) {
	t.Run("returns response text from mock", func(t *testing.T) {
		prov := &mockConsultProvider{name: "mock", response: "I agree with your approach."}
		got, err := consultWithProvider(prov, "test-api-key", "mock-model", "Should I use a mutex here?")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "I agree with your approach." {
			t.Errorf("expected 'I agree with your approach.', got %q", got)
		}
	})

	t.Run("propagates provider error", func(t *testing.T) {
		provErr := errors.New("provider unavailable")
		prov := &mockConsultProvider{name: "mock", err: provErr}
		_, err := consultWithProvider(prov, "test-api-key", "mock-model", "any summary")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, provErr) {
			t.Errorf("expected wrapped provErr, got %v", err)
		}
	})

	t.Run("sends correct system prompt", func(t *testing.T) {
		var capturedSystem string
		capturingProv := &capturingConsultProvider{captureSystem: &capturedSystem}
		_, err := consultWithProvider(capturingProv, "key", "model", "test summary")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedSystem != consultSystemPrompt {
			t.Errorf("expected system prompt %q, got %q", consultSystemPrompt, capturedSystem)
		}
	})

	t.Run("sends summary as user message", func(t *testing.T) {
		var capturedMsgs []domain.TranscriptMessage
		capturingProv := &capturingConsultProvider{captureMsgs: &capturedMsgs}
		summary := "Here is my proposed approach to refactoring."
		_, err := consultWithProvider(capturingProv, "key", "model", summary)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(capturedMsgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(capturedMsgs))
		}
		if capturedMsgs[0].Role != "user" {
			t.Errorf("expected role=user, got %q", capturedMsgs[0].Role)
		}
		if capturedMsgs[0].Content != summary {
			t.Errorf("expected content=%q, got %q", summary, capturedMsgs[0].Content)
		}
	})

	t.Run("sends no tools", func(t *testing.T) {
		var capturedTools []provider.ToolSpec
		capturingProv := &capturingConsultProvider{captureTools: &capturedTools}
		_, err := consultWithProvider(capturingProv, "key", "model", "summary")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(capturedTools) != 0 {
			t.Errorf("expected no tools, got %d", len(capturedTools))
		}
	})
}

// ---------------------------------------------------------------------------
// TestConsult (public method on *Service)
// ---------------------------------------------------------------------------

func TestConsult(t *testing.T) {
	t.Run("errors when modelConsult is empty", func(t *testing.T) {
		svc := &Service{}
		_, err := svc.Consult("some summary")
		if err == nil {
			t.Fatal("expected error when modelConsult is empty")
		}
		if err.Error() != "no consult model configured" {
			t.Errorf("expected 'no consult model configured', got %q", err.Error())
		}
	})
}

func TestService_SetModelConsult(t *testing.T) {
	t.Run("SetModelConsult stores value", func(t *testing.T) {
		svc := &Service{}
		svc.SetModelConsult("openai/gpt-4o")
		if svc.modelConsult != "openai/gpt-4o" {
			t.Errorf("expected openai/gpt-4o, got %s", svc.modelConsult)
		}
	})

	t.Run("ConsultModel returns stored value", func(t *testing.T) {
		svc := &Service{}
		svc.SetModelConsult("anthropic/claude-sonnet")
		got := svc.ConsultModel()
		if got != "anthropic/claude-sonnet" {
			t.Errorf("expected anthropic/claude-sonnet, got %s", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// capturingConsultProvider captures StreamMessage arguments for inspection.
type capturingConsultProvider struct {
	captureSystem *string
	captureMsgs   *[]domain.TranscriptMessage
	captureTools  *[]provider.ToolSpec
}

func (c *capturingConsultProvider) Name() string { return "mock" }

func (c *capturingConsultProvider) StreamMessage(
	apiKey, modelID string,
	msgs []domain.TranscriptMessage,
	tools []provider.ToolSpec,
	system string,
	onDelta func(string),
) ([]domain.ContentBlock, string, provider.Usage, error) {
	if c.captureSystem != nil {
		*c.captureSystem = system
	}
	if c.captureMsgs != nil {
		*c.captureMsgs = msgs
	}
	if c.captureTools != nil {
		*c.captureTools = tools
	}
	return []domain.ContentBlock{{Type: "text", Text: "ok"}}, "end_turn", provider.Usage{}, nil
}

func (c *capturingConsultProvider) FetchModels(apiKey string) ([]domain.APIModelInfo, error) {
	return nil, nil
}
