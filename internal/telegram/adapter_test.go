package telegram

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/time/rate"
	_ "modernc.org/sqlite"

	"github.com/batalabs/muxd/internal/agent"
	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := store.NewFromDB(db)
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	return st
}

func TestIsAllowed(t *testing.T) {
	tests := []struct {
		name    string
		userID  int64
		allowed []int64
		want    bool
	}{
		{
			name:    "user in list",
			userID:  123,
			allowed: []int64{100, 123, 456},
			want:    true,
		},
		{
			name:    "user not in list",
			userID:  789,
			allowed: []int64{100, 123, 456},
			want:    false,
		},
		{
			name:    "empty list denies all",
			userID:  123,
			allowed: []int64{},
			want:    false,
		},
		{
			name:    "nil list denies all",
			userID:  123,
			allowed: nil,
			want:    false,
		},
		{
			name:    "single allowed user",
			userID:  42,
			allowed: []int64{42},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAllowed(tt.userID, tt.allowed)
			if got != tt.want {
				t.Errorf("IsAllowed(%d, %v) = %v, want %v", tt.userID, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		maxLen    int
		wantParts int
		wantFirst string
	}{
		{
			name:      "short message unchanged",
			text:      "Hello, world!",
			maxLen:    4096,
			wantParts: 1,
			wantFirst: "Hello, world!",
		},
		{
			name:      "exact length",
			text:      strings.Repeat("a", 4096),
			maxLen:    4096,
			wantParts: 1,
		},
		{
			name:      "splits at newline",
			text:      strings.Repeat("a", 2000) + "\n" + strings.Repeat("b", 2000) + "\n" + strings.Repeat("c", 2000),
			maxLen:    4096,
			wantParts: 2,
		},
		{
			name:      "hard split when no newline",
			text:      strings.Repeat("a", 8192),
			maxLen:    4096,
			wantParts: 2,
		},
		{
			name:      "empty message",
			text:      "",
			maxLen:    4096,
			wantParts: 1,
		},
		{
			name:      "very long message splits multiple times",
			text:      strings.Repeat("x", 12288),
			maxLen:    4096,
			wantParts: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := SplitMessage(tt.text, tt.maxLen)
			if len(parts) != tt.wantParts {
				t.Errorf("SplitMessage() returned %d parts, want %d", len(parts), tt.wantParts)
			}
			if tt.wantFirst != "" && len(parts) > 0 && parts[0] != tt.wantFirst {
				t.Errorf("first part = %q, want %q", parts[0], tt.wantFirst)
			}
			// Verify all parts are within maxLen
			for i, p := range parts {
				if len(p) > tt.maxLen {
					t.Errorf("part %d has length %d, exceeds maxLen %d", i, len(p), tt.maxLen)
				}
			}
			// Verify reconstruction
			joined := strings.Join(parts, "")
			if joined != tt.text {
				t.Error("joined parts do not equal original text")
			}
		})
	}
}

func TestSplitHTMLMessage_doesNotSplitInsideTagsOrEntities(t *testing.T) {
	// Keep maxLen small to force splits.
	input := "prefix " + strings.Repeat("x", 40) + `<a href="https://example.com?q=a&amp;b=2">link</a> suffix`
	parts := SplitHTMLMessage(input, 64)
	if len(parts) < 2 {
		t.Fatalf("expected split into multiple parts, got %d", len(parts))
	}
	for i, p := range parts {
		if strings.Count(p, "<") > strings.Count(p, ">") {
			t.Fatalf("part %d appears to end inside HTML tag: %q", i, p)
		}
		if strings.LastIndex(p, "&") > strings.LastIndex(p, ";") {
			t.Fatalf("part %d appears to end inside HTML entity: %q", i, p)
		}
	}
	if strings.Join(parts, "") != input {
		t.Fatal("joined HTML parts do not reconstruct original input")
	}
}

func TestIsPrivateChat(t *testing.T) {
	tests := []struct {
		name     string
		chatType string
		want     bool
	}{
		{"private chat", "private", true},
		{"group chat", "group", false},
		{"supergroup chat", "supergroup", false},
		{"channel", "channel", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat := &tgbotapi.Chat{Type: tt.chatType}
			got := IsPrivateChat(chat)
			if got != tt.want {
				t.Errorf("IsPrivateChat(type=%q) = %v, want %v", tt.chatType, got, tt.want)
			}
		})
	}

	t.Run("nil chat returns false", func(t *testing.T) {
		if IsPrivateChat(nil) {
			t.Error("expected false for nil chat")
		}
	})
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no ANSI", "hello world", "hello world"},
		{"bold text", "\x1b[1mhello\x1b[0m", "hello"},
		{"colored text", "\x1b[31mred\x1b[0m and \x1b[32mgreen\x1b[0m", "red and green"},
		{"empty string", "", ""},
		{"multiple codes", "\x1b[1;31;4mformatted\x1b[0m", "formatted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.input)
			if got != tt.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetOrCreateAgent(t *testing.T) {
	st := openTestStore(t)

	ta := &Adapter{
		Config:       config.TelegramConfig{},
		Store:        st,
		APIKey:       "fake-key",
		ModelID:      "fake-model",
		ModelLabel:   "fake",
		sessions:     make(map[int64]*agent.Service),
		askState:     make(map[int64]chan<- string),
		rateLimiters: make(map[int64]*rate.Limiter),
	}

	// First call creates a new agent
	agent1 := ta.GetOrCreateAgent(123)
	if agent1 == nil {
		t.Fatal("expected non-nil agent")
	}
	if agent1.Session() == nil {
		t.Fatal("expected non-nil session")
	}

	// Second call returns the same agent
	agent2 := ta.GetOrCreateAgent(123)
	if agent1 != agent2 {
		t.Error("expected same agent for same chatID")
	}

	// Different chat ID gets different agent
	agent3 := ta.GetOrCreateAgent(456)
	if agent3 == agent1 {
		t.Error("expected different agent for different chatID")
	}
}

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "hides file path",
			err:  fmt.Errorf("open /home/alice/secret-project/config.json: permission denied"),
			want: "An internal error occurred.",
		},
		{
			name: "nil error returns empty",
			err:  nil,
			want: "",
		},
		{
			name: "hides database error",
			err:  fmt.Errorf("sql: database is locked"),
			want: "An internal error occurred.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeError(tt.err)
			if got != tt.want {
				t.Errorf("sanitizeError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAdapter_rateLimiter(t *testing.T) {
	ta := &Adapter{
		rateLimiters: make(map[int64]*rate.Limiter),
	}

	// First few calls should be allowed (burst=5)
	for i := 0; i < 5; i++ {
		if !ta.allowRequest(123) {
			t.Errorf("request %d should be allowed (within burst)", i)
		}
	}

	// Next call should be denied (burst exhausted, no time passed)
	if ta.allowRequest(123) {
		t.Error("request should be denied after burst exhausted")
	}

	// Different user should still be allowed
	if !ta.allowRequest(456) {
		t.Error("different user should have their own limiter")
	}
}

func TestSummarizeTelegramText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"short", "hello world", "hello world"},
		{"exactly 72", strings.Repeat("x", 72), strings.Repeat("x", 72)},
		{"long truncated", strings.Repeat("a", 100), strings.Repeat("a", 72) + "..."},
		{"newlines collapsed", "line1\nline2\nline3", "line1 line2 line3"},
		{"whitespace trimmed", "  hello  ", "hello"},
		{"newlines and long", strings.Repeat("x\n", 100), strings.Repeat("x ", 36) + "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeTelegramText(tt.input)
			if got != tt.want {
				t.Errorf("summarizeTelegramText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDisabledToolsCSV(t *testing.T) {
	tests := []struct {
		name     string
		disabled map[string]bool
		want     string
	}{
		{"nil map", nil, ""},
		{"empty map", map[string]bool{}, ""},
		{"single disabled", map[string]bool{"bash": true}, "bash"},
		{"multiple sorted", map[string]bool{"grep": true, "bash": true, "write": true}, "bash,grep,write"},
		{"false values excluded", map[string]bool{"bash": true, "grep": false}, "bash"},
		{"all false", map[string]bool{"a": false, "b": false}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := disabledToolsCSV(tt.disabled)
			if got != tt.want {
				t.Errorf("disabledToolsCSV(%v) = %q, want %q", tt.disabled, got, tt.want)
			}
		})
	}
}

func TestIsSafeHTMLBoundary(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   bool
	}{
		{"empty string", "", true},
		{"plain text", "hello world", true},
		{"complete tag", "hello <b>bold</b>", true},
		{"inside tag", "hello <a href", false},
		{"unclosed lt", "hello <", false},
		{"complete entity", "hello &amp;", true},
		{"inside entity", "hello &amp", false},
		{"unclosed amp", "text &", false},
		{"tag then entity ok", "<b>hello</b> &amp;", true},
		{"nested ok", "<b><i>hi</i></b>", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSafeHTMLBoundary(tt.prefix)
			if got != tt.want {
				t.Errorf("isSafeHTMLBoundary(%q) = %v, want %v", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestSafeSplitIndex(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		candidate int
		min       int
		htmlAware bool
		want      int
	}{
		{"candidate zero returns 1", "hello", 0, 1, false, 1},
		{"candidate past end clamped", "hi", 10, 1, false, 2},
		{"plain text at candidate", "hello world", 5, 1, false, 5},
		{"min below 1 defaults to 1", "hi", 2, 0, false, 2},
		{"utf8 boundary respected", "hello\xc0", 6, 1, false, 5},
		{"html aware backs off tag", "abc<b", 5, 1, true, 3},
		{"html aware entity backs off", "abc&amp", 7, 1, true, 3},
		{"html aware complete tag ok", "abc<b>x</b>", 11, 1, true, 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeSplitIndex(tt.text, tt.candidate, tt.min, tt.htmlAware)
			if got != tt.want {
				t.Errorf("safeSplitIndex(%q, %d, %d, %v) = %d, want %d",
					tt.text, tt.candidate, tt.min, tt.htmlAware, got, tt.want)
			}
		})
	}
}

func TestSplitMessageInternal_zeroMaxLen(t *testing.T) {
	// maxLen <= 0 should default to MaxMessageLen (4096)
	text := strings.Repeat("a", 5000)
	parts := splitMessageInternal(text, 0, false)
	if len(parts) != 2 {
		t.Errorf("expected 2 parts with default maxLen, got %d", len(parts))
	}
	for i, p := range parts {
		if len(p) > MaxMessageLen {
			t.Errorf("part %d length %d exceeds MaxMessageLen %d", i, len(p), MaxMessageLen)
		}
	}
}

func TestDefaultTelegramLogPath(t *testing.T) {
	path := defaultTelegramLogPath()
	// Should return a non-empty path containing the expected filename
	if path == "" {
		t.Skip("config.DataDir returned empty (no home dir)")
	}
	if !strings.HasSuffix(path, "runtime-telegram.log") {
		t.Errorf("path = %q, want suffix runtime-telegram.log", path)
	}
}

func TestAdapter_projectPath(t *testing.T) {
	ta := &Adapter{}
	path := ta.projectPath()
	if path == "" {
		t.Error("expected non-empty project path")
	}
	// Should return cwd, which is a real directory
	if path == "." {
		t.Error("expected absolute path, got '.'")
	}
}

func TestAdapter_logf(t *testing.T) {
	// logf with empty logPath and failing DataDir should silently do nothing
	ta := &Adapter{logPath: ""}
	// Should not panic
	ta.logf("test message: %d", 42)
	ta.logf("") // empty line should be skipped
}

func TestAdapter_applyModelSpec(t *testing.T) {
	st := openTestStore(t)
	ta := &Adapter{
		Store:        st,
		APIKey:       "fake-key",
		ModelID:      "claude-sonnet-4-6",
		ModelLabel:   "claude-sonnet",
		sessions:     make(map[int64]*agent.Service),
		askState:     make(map[int64]chan<- string),
		rateLimiters: make(map[int64]*rate.Limiter),
		Prefs:        &config.Preferences{},
	}

	provName, modelID, _ := ta.applyModelSpec("claude-haiku")
	if provName != "anthropic" {
		t.Errorf("provider = %q, want anthropic", provName)
	}
	if modelID != "claude-haiku-4-5-20251001" {
		t.Errorf("modelID = %q, want claude-haiku-4-5-20251001", modelID)
	}
	// Verify adapter state was updated
	if ta.ModelID != modelID {
		t.Errorf("ta.ModelID = %q, want %q", ta.ModelID, modelID)
	}
}

func TestAdapter_applyModelSpec_unknownProvider(t *testing.T) {
	st := openTestStore(t)
	ta := &Adapter{
		Store:        st,
		APIKey:       "fake-key",
		ModelID:      "claude-sonnet-4-6",
		ModelLabel:   "claude-sonnet",
		sessions:     make(map[int64]*agent.Service),
		askState:     make(map[int64]chan<- string),
		rateLimiters: make(map[int64]*rate.Limiter),
		Prefs:        &config.Preferences{},
	}

	// A bogus provider prefix should fall back to the current provider
	provName, modelID, _ := ta.applyModelSpec("nonexistent-provider/some-model")
	// Should keep the existing model since the unknown prefix can't be resolved
	if modelID != "claude-sonnet-4-6" {
		t.Errorf("expected existing model ID to be preserved, got %q", modelID)
	}
	_ = provName
}

func TestSplitHTMLMessage_reconstructsOriginal(t *testing.T) {
	// Verify that HTML-aware splitting reconstructs the original text
	input := "<b>Hello</b> &amp; <i>world</i>! " + strings.Repeat("x", 4096)
	parts := SplitHTMLMessage(input, 4096)
	joined := strings.Join(parts, "")
	if joined != input {
		t.Error("joined HTML parts do not match original")
	}
}

func TestSplitMessage_singleChar(t *testing.T) {
	parts := SplitMessage("x", 1)
	if len(parts) != 1 || parts[0] != "x" {
		t.Errorf("expected [\"x\"], got %v", parts)
	}
}

func TestTelegramBotLogger_nilSafety(t *testing.T) {
	// nil logger should not panic
	var logger *telegramBotLogger
	logger.Println("test")
	logger.Printf("test %d", 42)

	// nil adapter should not panic
	logger2 := &telegramBotLogger{adapter: nil}
	logger2.Println("test")
	logger2.Printf("test %d", 42)
}
