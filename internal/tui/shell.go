package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// findBash locates a bash executable. On Windows it checks common Git Bash
// paths before falling back to PATH lookup.
func findBash() string {
	if runtime.GOOS != "windows" {
		return "/bin/sh"
	}
	// Git Bash common locations.
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fall back to PATH lookup.
	if p, err := exec.LookPath("bash.exe"); err == nil {
		return p
	}
	return "cmd.exe"
}

// cmdBuiltins are cmd.exe built-in commands that don't exist as standalone
// executables and must be routed through cmd.exe.
var cmdBuiltins = map[string]bool{
	"dir": true, "type": true, "copy": true, "move": true, "del": true,
	"ren": true, "rename": true, "cls": true, "set": true, "vol": true,
	"ver": true, "color": true, "title": true, "mklink": true, "assoc": true,
	"ftype": true, "pushd": true, "popd": true, "start": true, "erase": true,
}

// shellForCommand picks the right shell and args for a command on Windows.
// Returns (shell, args). On non-Windows, always uses bash.
func shellForCommand(command string) (string, []string) {
	if runtime.GOOS != "windows" {
		return findBash(), []string{"-c", command}
	}

	first := strings.ToLower(strings.Fields(command)[0])

	// PowerShell cmdlets follow Verb-Noun pattern (e.g. Get-Process).
	if strings.Contains(first, "-") && first[0] >= 'a' && first[0] <= 'z' {
		if ps, err := exec.LookPath("pwsh.exe"); err == nil {
			return ps, []string{"-NoProfile", "-Command", command}
		}
		return "powershell.exe", []string{"-NoProfile", "-Command", command}
	}

	// cmd.exe builtins.
	if cmdBuiltins[first] {
		return "cmd.exe", []string{"/c", command}
	}

	// Default to bash for everything else.
	shell := findBash()
	if filepath.Base(shell) == "cmd.exe" {
		return shell, []string{"/c", command}
	}
	return shell, []string{"-c", command}
}

// RunShellCmd runs a shell command in the given directory and returns the
// result via ShellResultMsg.
func RunShellCmd(command, cwd string) tea.Cmd {
	return func() tea.Msg {
		shell, args := shellForCommand(command)
		c := exec.Command(shell, args...)
		c.Dir = cwd
		result, err := c.CombinedOutput()
		output := strings.TrimSpace(string(result))
		if err != nil && output == "" {
			output = "Error: " + err.Error()
		}
		return ShellResultMsg{Output: output, Err: err}
	}
}

// shellGitInfo returns a short git status string for the given directory,
// e.g. "main*" (dirty) or "main" (clean). Returns "" if not a git repo.
func shellGitInfo(cwd string) string {
	branch := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branch.Dir = cwd
	out, err := branch.Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))

	dirty := exec.Command("git", "status", "--porcelain")
	dirty.Dir = cwd
	dOut, _ := dirty.Output()
	if len(strings.TrimSpace(string(dOut))) > 0 {
		name += "*"
	}
	return name
}

// handleShellKey handles key input in shell mode.
func (m Model) handleShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		cmd := strings.TrimSpace(m.shellInput)
		m.shellInput = ""
		m.shellInputCursor = 0
		if cmd == "exit" {
			m.shellActive = false
			return m, PrintToScrollback(WelcomeStyle.Render("Exited muxd shell."))
		}
		if cmd == "/help" {
			return m, PrintToScrollback(shellHelpText())
		}
		if cmd == "" {
			return m, nil
		}
		// Record in history.
		m.shellHistory = append(m.shellHistory, cmd)
		m.shellHistoryIdx = len(m.shellHistory)
		// Handle cd locally so the cwd persists across commands.
		// Match "cd", "cd ", "cd.." (Windows-style), "cd\" etc.
		lower := strings.ToLower(cmd)
		if lower == "cd" || strings.HasPrefix(lower, "cd ") ||
			strings.HasPrefix(lower, "cd..") || strings.HasPrefix(lower, "cd\\") ||
			strings.HasPrefix(lower, "cd/") {
			return m.handleShellCd(cmd)
		}
		// Echo the command before running it.
		echo := FooterMeta.Render("$ " + cmd)
		return m, tea.Batch(PrintToScrollback(echo), RunShellCmd(cmd, m.shellCwd))
	case tea.KeyCtrlC:
		m.shellActive = false
		m.shellInput = ""
		m.shellInputCursor = 0
		return m, PrintToScrollback(WelcomeStyle.Render("Exited muxd shell."))
	case tea.KeyEsc:
		// Esc clears current input; if already empty, exits.
		if m.shellInput != "" {
			m.shellInput = ""
			m.shellInputCursor = 0
			return m, nil
		}
		m.shellActive = false
		return m, PrintToScrollback(WelcomeStyle.Render("Exited muxd shell."))
	case tea.KeyBackspace:
		if m.shellInputCursor > 0 {
			m.shellInput = m.shellInput[:m.shellInputCursor-1] + m.shellInput[m.shellInputCursor:]
			m.shellInputCursor--
		}
		return m, nil
	case tea.KeyLeft:
		if m.shellInputCursor > 0 {
			m.shellInputCursor--
		}
		return m, nil
	case tea.KeyRight:
		if m.shellInputCursor < len(m.shellInput) {
			m.shellInputCursor++
		}
		return m, nil
	case tea.KeyHome, tea.KeyCtrlA:
		m.shellInputCursor = 0
		return m, nil
	case tea.KeyEnd, tea.KeyCtrlE:
		m.shellInputCursor = len(m.shellInput)
		return m, nil
	case tea.KeyUp:
		if len(m.shellHistory) > 0 && m.shellHistoryIdx > 0 {
			m.shellHistoryIdx--
			m.shellInput = m.shellHistory[m.shellHistoryIdx]
			m.shellInputCursor = len(m.shellInput)
		}
		return m, nil
	case tea.KeyDown:
		if m.shellHistoryIdx < len(m.shellHistory)-1 {
			m.shellHistoryIdx++
			m.shellInput = m.shellHistory[m.shellHistoryIdx]
			m.shellInputCursor = len(m.shellInput)
		} else if m.shellHistoryIdx == len(m.shellHistory)-1 {
			m.shellHistoryIdx = len(m.shellHistory)
			m.shellInput = ""
			m.shellInputCursor = 0
		}
		return m, nil
	case tea.KeyTab:
		return m, nil
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			for _, r := range msg.Runes {
				m.shellInput = m.shellInput[:m.shellInputCursor] + string(r) + m.shellInput[m.shellInputCursor:]
				m.shellInputCursor++
			}
		}
		return m, nil
	}
}

// shellHelpText returns a formatted help string for the muxd shell.
func shellHelpText() string {
	var b strings.Builder
	b.WriteString(WelcomeStyle.Render("muxd shell") + "\n\n")
	lines := []struct{ key, desc string }{
		{"exit", "Return to muxd chat"},
		{"/help", "Show this help"},
	}
	for _, l := range lines {
		b.WriteString("  " + FooterHead.Render(l.key) + "  " + FooterMeta.Render(l.desc) + "\n")
	}
	b.WriteString("\n" + FooterMeta.Render("  Windows commands (dir, type, etc.) are auto-detected."))
	b.WriteString("\n" + FooterMeta.Render("  PowerShell cmdlets (Get-Process, etc.) are auto-detected."))
	b.WriteString("\n" + FooterMeta.Render("  Git branch shown in header (green=clean, yellow=dirty)."))
	return b.String()
}

// handleShellCd changes the shell mode working directory.
func (m Model) handleShellCd(cmd string) (tea.Model, tea.Cmd) {
	// Strip "cd" prefix case-insensitively; handles "cd ..", "cd..", "CD\foo".
	target := strings.TrimSpace(cmd[2:])
	if target == "" || target == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return m, PrintToScrollback(ErrorLineStyle.Render("cd: " + err.Error()))
		}
		target = home
	}
	// Resolve relative paths against current shell cwd.
	if !filepath.IsAbs(target) {
		target = filepath.Join(m.shellCwd, target)
	}
	target = filepath.Clean(target)
	info, err := os.Stat(target)
	if err != nil {
		return m, PrintToScrollback(ErrorLineStyle.Render("cd: " + err.Error()))
	}
	if !info.IsDir() {
		return m, PrintToScrollback(ErrorLineStyle.Render("cd: not a directory: " + target))
	}
	m.shellCwd = target
	return m, nil
}
