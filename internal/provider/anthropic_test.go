package provider

import (
	"encoding/json"
	"testing"

	"github.com/batalabs/muxd/internal/domain"
)

func TestBuildAnthropicMessages_ImageBlock(t *testing.T) {
	msgs := []domain.TranscriptMessage{
		{
			Role: "user",
			Blocks: []domain.ContentBlock{
				{Type: "image", MediaType: "image/png", Base64Data: "aWNvbg==", ImagePath: "test.png"},
				{Type: "text", Text: "describe this"},
			},
		},
	}
	result := buildAnthropicMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	var blocks []anthropicContentBlock
	if err := json.Unmarshal(result[0].Content, &blocks); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "image" {
		t.Errorf("expected image block, got %s", blocks[0].Type)
	}
	if blocks[0].Source == nil {
		t.Fatal("expected image source")
	}
	if blocks[0].Source.Type != "base64" {
		t.Errorf("expected base64 source type, got %s", blocks[0].Source.Type)
	}
	if blocks[0].Source.MediaType != "image/png" {
		t.Errorf("expected image/png media type, got %s", blocks[0].Source.MediaType)
	}
	if blocks[0].Source.Data != "aWNvbg==" {
		t.Errorf("expected base64 data, got %s", blocks[0].Source.Data)
	}
	if blocks[1].Type != "text" {
		t.Errorf("expected text block, got %s", blocks[1].Type)
	}
	if blocks[1].Text != "describe this" {
		t.Errorf("expected 'describe this', got %s", blocks[1].Text)
	}
}

func TestBuildAnthropicMessages_NoImages(t *testing.T) {
	msgs := []domain.TranscriptMessage{
		{Role: "user", Content: "hello world"},
	}
	result := buildAnthropicMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	// Should be a plain text message, not blocks
	var text string
	if err := json.Unmarshal(result[0].Content, &text); err != nil {
		t.Fatalf("expected plain text content: %v", err)
	}
	if text != "hello world" {
		t.Errorf("expected 'hello world', got %q", text)
	}
}
