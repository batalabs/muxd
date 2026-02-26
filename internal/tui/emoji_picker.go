package tui

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/config"
)

// emojiEntry holds a preset name and its emoji character.
type emojiEntry struct {
	Name  string
	Emoji string
}

// EmojiPicker is an interactive overlay for selecting a footer emoji.
type EmojiPicker struct {
	entries     []emojiEntry
	selectedIdx int
	active      bool
}

// NewEmojiPicker creates an emoji picker with all preset options.
func NewEmojiPicker(current string) *EmojiPicker {
	names := config.EmojiPresetNames()
	entries := make([]emojiEntry, 0, len(names)+1)
	// "none" option first
	entries = append(entries, emojiEntry{Name: "none", Emoji: ""})
	for _, name := range names {
		entries = append(entries, emojiEntry{
			Name:  name,
			Emoji: config.ResolveEmoji(name),
		})
	}

	// Find current selection
	selectedIdx := 0
	for i, e := range entries {
		if e.Emoji == current {
			selectedIdx = i
			break
		}
	}

	return &EmojiPicker{
		entries:     entries,
		selectedIdx: selectedIdx,
		active:      true,
	}
}

func (p *EmojiPicker) IsActive() bool {
	return p != nil && p.active
}

func (p *EmojiPicker) Dismiss() {
	p.active = false
}

func (p *EmojiPicker) MoveUp() {
	if p.selectedIdx > 0 {
		p.selectedIdx--
	}
}

func (p *EmojiPicker) MoveDown() {
	if p.selectedIdx < len(p.entries)-1 {
		p.selectedIdx++
	}
}

// Selected returns the emoji string for the currently highlighted entry.
func (p *EmojiPicker) Selected() string {
	if p.selectedIdx < 0 || p.selectedIdx >= len(p.entries) {
		return ""
	}
	return p.entries[p.selectedIdx].Emoji
}

// SelectedName returns the preset name for the currently highlighted entry.
func (p *EmojiPicker) SelectedName() string {
	if p.selectedIdx < 0 || p.selectedIdx >= len(p.entries) {
		return ""
	}
	return p.entries[p.selectedIdx].Name
}

func (p *EmojiPicker) View(width int) string {
	if width < 40 {
		width = 40
	}
	var b strings.Builder
	b.WriteString(FooterHead.Render("Emoji Picker"))
	b.WriteString("\n")
	b.WriteString(FooterMeta.Render("  Up/Down=navigate  Enter=select  Esc=cancel"))
	b.WriteString("\n\n")

	for i, e := range p.entries {
		display := e.Name
		if e.Emoji != "" {
			display = fmt.Sprintf("%s  %s", e.Emoji, e.Name)
		}

		var line string
		if i == p.selectedIdx {
			line = fmt.Sprintf("> %-20s", display)
			b.WriteString(CompletionSelStyle.Render(line))
		} else {
			line = fmt.Sprintf("  %-20s", display)
			b.WriteString(FooterMeta.Render(line))
		}
		b.WriteString("\n")
	}

	return b.String()
}
