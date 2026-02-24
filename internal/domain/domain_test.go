package domain

import (
	"regexp"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// uuid.go
// ---------------------------------------------------------------------------

func TestNewUUID(t *testing.T) {
	id := NewUUID()
	if id == "" {
		t.Fatal("expected non-empty UUID")
	}

	// RFC 4122 v4 format: 8-4-4-4-12 hex chars
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !re.MatchString(id) {
		t.Errorf("UUID %q does not match v4 format", id)
	}
}

func TestNewUUID_unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewUUID()
		if seen[id] {
			t.Fatalf("duplicate UUID on iteration %d: %s", i, id)
		}
		seen[id] = true
	}
}

// ---------------------------------------------------------------------------
// commands.go
// ---------------------------------------------------------------------------

func TestCommandHelp_TUI(t *testing.T) {
	cmds := CommandHelp(false)
	for _, c := range cmds {
		if c.TelegramOnly {
			t.Errorf("TUI help should not include TelegramOnly command %s", c.Name)
		}
	}
	// Verify TUI-only commands are present
	found := false
	for _, c := range cmds {
		if c.Name == "/undo" {
			found = true
			break
		}
	}
	if !found {
		t.Error("TUI help should include /undo")
	}
}

func TestCommandHelp_Telegram(t *testing.T) {
	cmds := CommandHelp(true)
	for _, c := range cmds {
		if c.TUIOnly {
			t.Errorf("Telegram help should not include TUIOnly command %s", c.Name)
		}
	}
	// Verify Telegram-only commands are present
	found := false
	for _, c := range cmds {
		if c.Name == "/start" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Telegram help should include /start")
	}
}

func TestCommandHelp_commonCommandsInBoth(t *testing.T) {
	tuiCmds := CommandHelp(false)
	tgCmds := CommandHelp(true)

	// /help should appear in both
	for _, name := range []string{"/help", "/new", "/config"} {
		tuiFound, tgFound := false, false
		for _, c := range tuiCmds {
			if c.Name == name {
				tuiFound = true
			}
		}
		for _, c := range tgCmds {
			if c.Name == name {
				tgFound = true
			}
		}
		if !tuiFound {
			t.Errorf("%s should be in TUI help", name)
		}
		if !tgFound {
			t.Errorf("%s should be in Telegram help", name)
		}
	}
}

func TestCommandGroups_nonEmpty(t *testing.T) {
	if len(CommandGroups) == 0 {
		t.Fatal("expected non-empty CommandGroups")
	}
	for _, g := range CommandGroups {
		if g.Key == "" || g.Label == "" {
			t.Errorf("group has empty key or label: %+v", g)
		}
	}
}

func TestCommandDefs_allHaveGroup(t *testing.T) {
	for _, c := range CommandDefs {
		if c.Name == "" {
			t.Error("command with empty name")
		}
		if c.Group == "" {
			t.Errorf("command %s has no group", c.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// types.go — TranscriptMessage
// ---------------------------------------------------------------------------

func TestTranscriptMessage_HasBlocks(t *testing.T) {
	tests := []struct {
		name   string
		msg    TranscriptMessage
		expect bool
	}{
		{"no blocks", TranscriptMessage{Content: "hello"}, false},
		{"empty blocks slice", TranscriptMessage{Blocks: []ContentBlock{}}, false},
		{"with blocks", TranscriptMessage{Blocks: []ContentBlock{{Type: "text", Text: "hi"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.HasBlocks(); got != tt.expect {
				t.Errorf("HasBlocks() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestTranscriptMessage_TextContent(t *testing.T) {
	tests := []struct {
		name   string
		msg    TranscriptMessage
		expect string
	}{
		{
			"no blocks returns Content",
			TranscriptMessage{Content: "hello world"},
			"hello world",
		},
		{
			"single text block",
			TranscriptMessage{Blocks: []ContentBlock{
				{Type: "text", Text: "first"},
			}},
			"first",
		},
		{
			"multiple text blocks joined",
			TranscriptMessage{Blocks: []ContentBlock{
				{Type: "text", Text: "first"},
				{Type: "text", Text: "second"},
			}},
			"first\nsecond",
		},
		{
			"filters non-text blocks",
			TranscriptMessage{Blocks: []ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "tool_use", ToolName: "bash"},
				{Type: "text", Text: "world"},
			}},
			"hello\nworld",
		},
		{
			"only tool blocks returns empty",
			TranscriptMessage{Blocks: []ContentBlock{
				{Type: "tool_use", ToolName: "bash"},
				{Type: "tool_result", ToolResult: "ok"},
			}},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.TextContent(); got != tt.expect {
				t.Errorf("TextContent() = %q, want %q", got, tt.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// types.go — Session
// ---------------------------------------------------------------------------

func TestSession_TagList(t *testing.T) {
	tests := []struct {
		name   string
		tags   string
		expect []string
	}{
		{"empty", "", nil},
		{"single tag", "foo", []string{"foo"}},
		{"multiple tags", "foo,bar,baz", []string{"foo", "bar", "baz"}},
		{"whitespace trimmed", " foo , bar , baz ", []string{"foo", "bar", "baz"}},
		{"skips empty segments", "foo,,bar,", []string{"foo", "bar"}},
		{"only commas", ",,,", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Session{Tags: tt.tags}
			got := s.TagList()
			if tt.expect == nil && got != nil {
				t.Errorf("TagList() = %v, want nil", got)
				return
			}
			if len(got) != len(tt.expect) {
				t.Errorf("TagList() len = %d, want %d: %v", len(got), len(tt.expect), got)
				return
			}
			for i := range tt.expect {
				if got[i] != tt.expect[i] {
					t.Errorf("TagList()[%d] = %q, want %q", i, got[i], tt.expect[i])
				}
			}
		})
	}
}

func TestSession_HasTag(t *testing.T) {
	s := Session{Tags: "foo, Bar, BAZ"}

	tests := []struct {
		tag    string
		expect bool
	}{
		{"foo", true},
		{"Foo", true},
		{"FOO", true},
		{"bar", true},
		{"baz", true},
		{"qux", false},
		{"", false},
		{"  foo  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := s.HasTag(tt.tag); got != tt.expect {
				t.Errorf("HasTag(%q) = %v, want %v", tt.tag, got, tt.expect)
			}
		})
	}
}

func TestSession_HasTag_emptyTags(t *testing.T) {
	s := Session{}
	if s.HasTag("anything") {
		t.Error("expected HasTag to return false for empty tags")
	}
}

// ---------------------------------------------------------------------------
// types.go — ContentBlock JSON
// ---------------------------------------------------------------------------

func TestContentBlock_fields(t *testing.T) {
	b := ContentBlock{
		Type:         "tool_use",
		ToolUseID:    "tu-1",
		ToolName:     "bash",
		ToolInput:    map[string]any{"cmd": "ls"},
		CallerType:   "direct",
		CallerToolID: "stu-1",
	}
	if b.Type != "tool_use" {
		t.Errorf("Type = %q", b.Type)
	}
	if b.ToolName != "bash" {
		t.Errorf("ToolName = %q", b.ToolName)
	}
	if b.CallerType != "direct" {
		t.Errorf("CallerType = %q", b.CallerType)
	}
}

// ---------------------------------------------------------------------------
// types.go — ModelPricing / APIModelInfo
// ---------------------------------------------------------------------------

func TestModelPricing_fields(t *testing.T) {
	p := ModelPricing{InputPerMillion: 3.0, OutputPerMillion: 15.0}
	if p.InputPerMillion != 3.0 {
		t.Errorf("InputPerMillion = %f", p.InputPerMillion)
	}
	if p.OutputPerMillion != 15.0 {
		t.Errorf("OutputPerMillion = %f", p.OutputPerMillion)
	}
}

func TestAPIModelInfo_fields(t *testing.T) {
	m := APIModelInfo{ID: "gpt-4o", DisplayName: "GPT-4o", CreatedAt: "2024-01-01"}
	if m.ID != "gpt-4o" {
		t.Errorf("ID = %q", m.ID)
	}
}

// ---------------------------------------------------------------------------
// types.go — JSON round-trip
// ---------------------------------------------------------------------------

func TestContentBlock_jsonTags(t *testing.T) {
	// Verify omitempty tags work correctly
	b := ContentBlock{Type: "text", Text: "hello"}
	if b.ToolUseID != "" {
		t.Error("expected empty ToolUseID")
	}
	if b.IsError {
		t.Error("expected IsError to be false")
	}
}

func TestSession_zeroValue(t *testing.T) {
	var s Session
	if s.ID != "" {
		t.Error("expected empty ID")
	}
	if s.Tags != "" {
		t.Error("expected empty Tags")
	}
	if tags := s.TagList(); tags != nil {
		t.Errorf("expected nil TagList, got %v", tags)
	}
}

// ---------------------------------------------------------------------------
// uuid.go — format details
// ---------------------------------------------------------------------------

func TestNewUUID_version4Bits(t *testing.T) {
	for i := 0; i < 50; i++ {
		id := NewUUID()
		parts := strings.Split(id, "-")
		if len(parts) != 5 {
			t.Fatalf("expected 5 parts, got %d: %s", len(parts), id)
		}
		// Third group should start with '4' (version 4)
		if parts[2][0] != '4' {
			t.Errorf("version nibble = %c, want '4' in UUID %s", parts[2][0], id)
		}
		// Fourth group should start with 8, 9, a, or b (variant 1)
		c := parts[3][0]
		if c != '8' && c != '9' && c != 'a' && c != 'b' {
			t.Errorf("variant nibble = %c, want 8/9/a/b in UUID %s", c, id)
		}
	}
}
