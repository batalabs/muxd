package tui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// PasteMsg carries clipboard read results to the TUI model.
type PasteMsg struct {
	Text string
	Err  error
}

// ClipboardWriteMsg carries clipboard write results to the TUI model.
type ClipboardWriteMsg struct {
	OK  bool
	Err error
}

// ReadClipboardCmd returns a Bubble Tea Cmd that reads the system clipboard
// and delivers the contents as a PasteMsg.
// Supports Windows (powershell), macOS (pbpaste), and Linux (xclip).
func ReadClipboardCmd() tea.Cmd {
	return func() tea.Msg {
		if out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw").Output(); err == nil {
			return PasteMsg{Text: string(out)}
		}
		if out, err := exec.Command("pbpaste").Output(); err == nil {
			return PasteMsg{Text: string(out)}
		}
		if out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output(); err == nil {
			return PasteMsg{Text: string(out)}
		}
		return PasteMsg{Err: fmt.Errorf("clipboard read not available")}
	}
}

// WriteClipboardCmd returns a Bubble Tea Cmd that writes text to the system
// clipboard and delivers a ClipboardWriteMsg on completion.
// Supports Windows (powershell), macOS (pbcopy), and Linux (xclip).
func WriteClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		if text == "" {
			return ClipboardWriteMsg{Err: fmt.Errorf("nothing to copy")}
		}

		winCmd := exec.Command("powershell", "-NoProfile", "-Command", "$input | Set-Clipboard")
		winCmd.Stdin = strings.NewReader(text)
		if err := winCmd.Run(); err == nil {
			return ClipboardWriteMsg{OK: true}
		}

		macCmd := exec.Command("pbcopy")
		macCmd.Stdin = strings.NewReader(text)
		if err := macCmd.Run(); err == nil {
			return ClipboardWriteMsg{OK: true}
		}

		linuxCmd := exec.Command("xclip", "-selection", "clipboard")
		linuxCmd.Stdin = strings.NewReader(text)
		if err := linuxCmd.Run(); err == nil {
			return ClipboardWriteMsg{OK: true}
		}

		return ClipboardWriteMsg{Err: fmt.Errorf("clipboard write not available")}
	}
}
