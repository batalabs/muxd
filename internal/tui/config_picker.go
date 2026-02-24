package tui

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/config"
)

type configPickerMode int

const (
	configPickerGroups configPickerMode = iota
	configPickerKeys
	configPickerEdit
)

type ConfigPicker struct {
	groups   []config.ConfigGroup
	groupIdx int
	keyIdx   int
	mode     configPickerMode
	active   bool

	editKey string
	editBuf string
}

func NewConfigPicker(prefs config.Preferences) *ConfigPicker {
	return &ConfigPicker{
		groups: prefs.Grouped(),
		active: true,
		mode:   configPickerGroups,
	}
}

func NewConfigPickerAtGroup(prefs config.Preferences, group string) *ConfigPicker {
	p := NewConfigPicker(prefs)
	p.FocusGroup(group)
	return p
}

func (p *ConfigPicker) IsActive() bool { return p != nil && p.active }
func (p *ConfigPicker) Dismiss()       { p.active = false }

func (p *ConfigPicker) Refresh(prefs config.Preferences) {
	p.groups = prefs.Grouped()
	if p.groupIdx >= len(p.groups) {
		p.groupIdx = max(0, len(p.groups)-1)
	}
	if g := p.selectedGroup(); g != nil && p.keyIdx >= len(g.Entries) {
		p.keyIdx = max(0, len(g.Entries)-1)
	}
}

func (p *ConfigPicker) FocusGroup(group string) {
	group = strings.ToLower(strings.TrimSpace(group))
	for i, g := range p.groups {
		if strings.ToLower(g.Name) == group {
			p.groupIdx = i
			p.mode = configPickerKeys
			p.keyIdx = 0
			return
		}
	}
}

func (p *ConfigPicker) selectedGroup() *config.ConfigGroup {
	if len(p.groups) == 0 || p.groupIdx < 0 || p.groupIdx >= len(p.groups) {
		return nil
	}
	return &p.groups[p.groupIdx]
}

func (p *ConfigPicker) selectedEntry() *config.PrefEntry {
	g := p.selectedGroup()
	if g == nil || len(g.Entries) == 0 || p.keyIdx < 0 || p.keyIdx >= len(g.Entries) {
		return nil
	}
	return &g.Entries[p.keyIdx]
}

func (p *ConfigPicker) MoveUp() {
	switch p.mode {
	case configPickerGroups:
		if p.groupIdx > 0 {
			p.groupIdx--
		}
	case configPickerKeys:
		if p.keyIdx > 0 {
			p.keyIdx--
		}
	}
}

func (p *ConfigPicker) MoveDown() {
	switch p.mode {
	case configPickerGroups:
		if p.groupIdx < len(p.groups)-1 {
			p.groupIdx++
		}
	case configPickerKeys:
		g := p.selectedGroup()
		if g != nil && p.keyIdx < len(g.Entries)-1 {
			p.keyIdx++
		}
	}
}

func (p *ConfigPicker) EnterGroup() {
	if p.mode == configPickerGroups {
		p.mode = configPickerKeys
		p.keyIdx = 0
	}
}

func (p *ConfigPicker) Back() {
	switch p.mode {
	case configPickerEdit:
		p.mode = configPickerKeys
		p.editKey = ""
		p.editBuf = ""
	case configPickerKeys:
		p.mode = configPickerGroups
		p.keyIdx = 0
	}
}

func (p *ConfigPicker) StartEdit(key, initial string) {
	p.mode = configPickerEdit
	p.editKey = key
	p.editBuf = initial
}

func (p *ConfigPicker) AppendEdit(r rune) {
	p.editBuf += string(r)
}

func (p *ConfigPicker) BackspaceEdit() {
	if len(p.editBuf) == 0 {
		return
	}
	rs := []rune(p.editBuf)
	p.editBuf = string(rs[:len(rs)-1])
}

func (p *ConfigPicker) CommitEdit() (key, value string, ok bool) {
	if p.mode != configPickerEdit {
		return "", "", false
	}
	key = p.editKey
	value = p.editBuf
	p.mode = configPickerKeys
	p.editKey = ""
	p.editBuf = ""
	return key, value, true
}

func (p *ConfigPicker) View(width int) string {
	if width < 40 {
		width = 40
	}
	var b strings.Builder
	b.WriteString(FooterHead.Render("Config Picker"))
	b.WriteString("\n")

	switch p.mode {
	case configPickerGroups:
		b.WriteString(FooterMeta.Render("  Enter=select group  Esc=close"))
		b.WriteString("\n\n")
		for i, g := range p.groups {
			line := fmt.Sprintf("  %s", g.Name)
			if i == p.groupIdx {
				line = "> " + g.Name
				b.WriteString(CompletionSelStyle.Render(line))
			} else {
				b.WriteString(FooterMeta.Render(line))
			}
			b.WriteString("\n")
		}
	case configPickerKeys:
		g := p.selectedGroup()
		groupLabel := "group"
		if g != nil {
			groupLabel = g.Name
		}
		b.WriteString(FooterMeta.Render("  Group: " + groupLabel + "  Enter=edit/toggle  Esc=back"))
		b.WriteString("\n\n")
		if g == nil || len(g.Entries) == 0 {
			b.WriteString(FooterMeta.Render("  No entries."))
			b.WriteString("\n")
			return b.String()
		}
		for i, e := range g.Entries {
			line := fmt.Sprintf("  %-24s %s", e.Key, e.Value)
			if i == p.keyIdx {
				line = fmt.Sprintf("> %-24s %s", e.Key, e.Value)
				b.WriteString(CompletionSelStyle.Render(line))
			} else {
				b.WriteString(FooterMeta.Render(line))
			}
			b.WriteString("\n")
		}
	case configPickerEdit:
		b.WriteString(FooterMeta.Render("  Edit " + p.editKey + "  Enter=save  Esc=cancel"))
		b.WriteString("\n\n")
		b.WriteString(FooterMeta.Render("  Value: " + p.editBuf))
		b.WriteString(CursorStyle.Render("█"))
		b.WriteString("\n")
	}

	return b.String()
}
