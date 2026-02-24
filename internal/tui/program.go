package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Prog holds a reference to the running Bubble Tea program so that Cmds
// (which execute in goroutines) can call Prog.Println() to push styled
// content into the terminal's native scrollback.
var Prog *tea.Program

// SetProgram sets the global Prog variable.
func SetProgram(p *tea.Program) {
	Prog = p
}

// MustGetwd returns the current working directory or "." on error.
func MustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
