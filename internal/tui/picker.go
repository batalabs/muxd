package tui

import (
	"fmt"
	"strings"

	"github.com/batalabs/muxd/internal/domain"
)

// pickerMode represents the current interaction mode of the picker.
type pickerMode int

const (
	pickerBrowse        pickerMode = iota // normal browsing
	pickerRenaming                        // editing a new title
	pickerConfirmDelete                   // waiting for y/n
)

// SessionPicker is an interactive session selector overlay.
type SessionPicker struct {
	sessions    []domain.Session
	filtered    []domain.Session
	selectedIdx int
	filter      string
	active      bool

	mode      pickerMode
	renameBuf string // title being edited in rename mode

	selected map[string]bool // multi-select: session IDs toggled on
}

// NewSessionPicker creates a picker with the given sessions.
func NewSessionPicker(sessions []domain.Session) *SessionPicker {
	return &SessionPicker{
		sessions: sessions,
		filtered: sessions,
		active:   true,
		selected: make(map[string]bool),
	}
}

// IsActive reports whether the picker is currently shown.
func (p *SessionPicker) IsActive() bool {
	return p != nil && p.active
}

// Dismiss closes the picker.
func (p *SessionPicker) Dismiss() {
	p.active = false
	p.selected = make(map[string]bool)
}

// Mode returns the current picker mode.
func (p *SessionPicker) Mode() pickerMode {
	return p.mode
}

// SelectedSession returns the currently highlighted session, or nil.
func (p *SessionPicker) SelectedSession() *domain.Session {
	if len(p.filtered) == 0 {
		return nil
	}
	return &p.filtered[p.selectedIdx]
}

// MoveUp moves the selection up.
func (p *SessionPicker) MoveUp() {
	if p.selectedIdx > 0 {
		p.selectedIdx--
	}
}

// MoveDown moves the selection down.
func (p *SessionPicker) MoveDown() {
	if p.selectedIdx < len(p.filtered)-1 {
		p.selectedIdx++
	}
}

// SetFilter replaces the filter string and re-filters.
func (p *SessionPicker) SetFilter(f string) {
	p.filter = f
	p.applyFilter()
}

// AppendFilter adds a rune to the filter.
func (p *SessionPicker) AppendFilter(r rune) {
	p.filter += string(r)
	p.applyFilter()
}

// BackspaceFilter removes the last rune from the filter.
func (p *SessionPicker) BackspaceFilter() {
	if len(p.filter) > 0 {
		runes := []rune(p.filter)
		p.filter = string(runes[:len(runes)-1])
		p.applyFilter()
	}
}

// StartRename enters rename mode, pre-filling with the current title.
func (p *SessionPicker) StartRename() {
	if sel := p.SelectedSession(); sel != nil {
		p.mode = pickerRenaming
		p.renameBuf = sel.Title
	}
}

// StartDelete enters delete confirmation mode.
func (p *SessionPicker) StartDelete() {
	if p.SelectedSession() != nil {
		p.mode = pickerConfirmDelete
	}
}

// CancelMode returns to browse mode.
func (p *SessionPicker) CancelMode() {
	p.mode = pickerBrowse
	p.renameBuf = ""
}

// RenameBuffer returns the current rename input.
func (p *SessionPicker) RenameBuffer() string {
	return p.renameBuf
}

// AppendRename adds a rune to the rename buffer.
func (p *SessionPicker) AppendRename(r rune) {
	p.renameBuf += string(r)
}

// BackspaceRename removes the last rune from the rename buffer.
func (p *SessionPicker) BackspaceRename() {
	if len(p.renameBuf) > 0 {
		runes := []rune(p.renameBuf)
		p.renameBuf = string(runes[:len(runes)-1])
	}
}

// CommitRename applies the rename to the selected session in the local list
// and returns to browse mode. Returns the session ID and new title.
func (p *SessionPicker) CommitRename() (id, newTitle string) {
	sel := p.SelectedSession()
	if sel == nil {
		p.CancelMode()
		return "", ""
	}
	id = sel.ID
	newTitle = strings.TrimSpace(p.renameBuf)
	if newTitle == "" {
		p.CancelMode()
		return "", ""
	}
	// Update in both slices.
	for i := range p.sessions {
		if p.sessions[i].ID == id {
			p.sessions[i].Title = newTitle
			break
		}
	}
	for i := range p.filtered {
		if p.filtered[i].ID == id {
			p.filtered[i].Title = newTitle
			break
		}
	}
	p.CancelMode()
	return id, newTitle
}

// RemoveSelected removes the selected session from the master list and
// re-applies the filter. Returns the removed session ID.
func (p *SessionPicker) RemoveSelected() string {
	sel := p.SelectedSession()
	if sel == nil {
		p.CancelMode()
		return ""
	}
	id := sel.ID
	// Remove from the master sessions list.
	for i := range p.sessions {
		if p.sessions[i].ID == id {
			p.sessions = append(p.sessions[:i], p.sessions[i+1:]...)
			break
		}
	}
	// Re-derive filtered list from sessions.
	p.applyFilter()
	// Adjust selection index if it now exceeds the list.
	if p.selectedIdx >= len(p.filtered) && p.selectedIdx > 0 {
		p.selectedIdx--
	}
	p.mode = pickerBrowse
	p.renameBuf = ""
	return id
}

// ToggleSelected toggles the highlighted session's selection.
func (p *SessionPicker) ToggleSelected() {
	sel := p.SelectedSession()
	if sel == nil {
		return
	}
	if p.selected[sel.ID] {
		delete(p.selected, sel.ID)
	} else {
		p.selected[sel.ID] = true
	}
}

// SelectAll selects all filtered sessions. If all are already selected, deselects all.
func (p *SessionPicker) SelectAll() {
	allSelected := true
	for _, s := range p.filtered {
		if !p.selected[s.ID] {
			allSelected = false
			break
		}
	}
	if allSelected {
		p.ClearSelected()
	} else {
		for _, s := range p.filtered {
			p.selected[s.ID] = true
		}
	}
}

// ClearSelected clears all selections.
func (p *SessionPicker) ClearSelected() {
	p.selected = make(map[string]bool)
}

// SelectedCount returns the number of selected sessions.
func (p *SessionPicker) SelectedCount() int {
	return len(p.selected)
}

// SelectedIDs returns the IDs of all selected sessions.
func (p *SessionPicker) SelectedIDs() []string {
	ids := make([]string, 0, len(p.selected))
	for id := range p.selected {
		ids = append(ids, id)
	}
	return ids
}

// RemoveSelectedMulti removes all selected sessions from the master list,
// clears the selection map, re-filters, adjusts the index, and returns the
// removed IDs.
func (p *SessionPicker) RemoveSelectedMulti() []string {
	if len(p.selected) == 0 {
		return nil
	}
	removed := make([]string, 0, len(p.selected))
	kept := p.sessions[:0:0]
	for _, s := range p.sessions {
		if p.selected[s.ID] {
			removed = append(removed, s.ID)
		} else {
			kept = append(kept, s)
		}
	}
	p.sessions = kept
	p.selected = make(map[string]bool)
	p.applyFilter()
	if p.selectedIdx >= len(p.filtered) && p.selectedIdx > 0 {
		p.selectedIdx = len(p.filtered) - 1
	}
	p.mode = pickerBrowse
	p.renameBuf = ""
	return removed
}

func (p *SessionPicker) applyFilter() {
	if p.filter == "" {
		p.filtered = p.sessions
	} else {
		lower := strings.ToLower(p.filter)
		p.filtered = nil
		for _, s := range p.sessions {
			if fuzzyMatch(s, lower) {
				p.filtered = append(p.filtered, s)
			}
		}
	}
	p.selectedIdx = 0
}

// fuzzyMatch checks if the session matches the filter against title, ID prefix, or tags.
func fuzzyMatch(s domain.Session, lower string) bool {
	if strings.Contains(strings.ToLower(s.Title), lower) {
		return true
	}
	if strings.Contains(strings.ToLower(s.ID), lower) {
		return true
	}
	if strings.Contains(strings.ToLower(s.Tags), lower) {
		return true
	}
	return false
}

// View renders the picker as a string.
func (p *SessionPicker) View(width int) string {
	if width < 40 {
		width = 40
	}

	var b strings.Builder

	// Header
	header := "Session Picker"
	b.WriteString(FooterHead.Render(header))
	b.WriteString("\n")

	// Mode-specific input line
	switch p.mode {
	case pickerRenaming:
		renameLine := "  Rename: " + p.renameBuf
		b.WriteString(FooterMeta.Render(renameLine))
		b.WriteString(CursorStyle.Render("\u2588"))
		b.WriteString("\n\n")
	case pickerConfirmDelete:
		if p.SelectedCount() > 0 {
			b.WriteString(ErrorLineStyle.Render(fmt.Sprintf("  Delete %d sessions? (y/n)", p.SelectedCount())))
		} else {
			sel := p.SelectedSession()
			title := ""
			if sel != nil {
				title = sel.Title
			}
			b.WriteString(ErrorLineStyle.Render(fmt.Sprintf("  Delete \"%s\"? (y/n)", title)))
		}
		b.WriteString("\n\n")
	default:
		filterLine := "  Filter: " + p.filter
		b.WriteString(FooterMeta.Render(filterLine))
		b.WriteString(CursorStyle.Render("\u2588"))
		b.WriteString("\n\n")
	}

	if len(p.filtered) == 0 {
		b.WriteString(FooterMeta.Render("  No matching sessions."))
		b.WriteString("\n")
	} else {
		const maxVisible = 10
		// Determine visible window
		start := 0
		if p.selectedIdx >= maxVisible {
			start = p.selectedIdx - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(p.filtered) {
			end = len(p.filtered)
		}

		for i := start; i < end; i++ {
			s := p.filtered[i]
			indicator := "  "
			if i == p.selectedIdx {
				indicator = "> "
			}

			check := "[ ] "
			if p.selected[s.ID] {
				check = "[x] "
			}

			idPrefix := s.ID[:8]
			title := s.Title
			if len(title) > 30 {
				title = title[:27] + "..."
			}
			ago := TimeAgo(s.UpdatedAt)
			msgCount := fmt.Sprintf("%d msgs", s.MessageCount)

			line := fmt.Sprintf("%s%s%-8s  %-30s  %-8s  %s",
				indicator, check, idPrefix, title, ago, msgCount)

			if s.Tags != "" {
				line += "  [" + s.Tags + "]"
			}
			if s.ParentSessionID != "" {
				line += "  \u2514branch"
			}

			if i == p.selectedIdx {
				b.WriteString(CompletionSelStyle.Render(line))
			} else {
				b.WriteString(FooterMeta.Render(line))
			}
			b.WriteString("\n")
		}

		if len(p.filtered) > maxVisible {
			more := fmt.Sprintf("  ... %d total", len(p.filtered))
			b.WriteString(FooterMeta.Render(more))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	switch p.mode {
	case pickerRenaming:
		b.WriteString(FooterMeta.Render("  Enter=save  Esc=cancel"))
	case pickerConfirmDelete:
		b.WriteString(FooterMeta.Render("  y=delete  n/Esc=cancel"))
	default:
		if p.SelectedCount() >= 2 {
			b.WriteString(FooterMeta.Render("  Space=select  a=all  d=delete  Esc=clear"))
		} else {
			b.WriteString(FooterMeta.Render("  Space=select  a=all  d=delete  r=rename  Enter=open  Esc=cancel"))
		}
	}
	b.WriteString("\n")

	return b.String()
}
