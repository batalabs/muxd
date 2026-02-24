package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/daemon"
)

// HandleCommand dispatches service management actions.
func HandleCommand(action string) error {
	switch strings.ToLower(action) {
	case "install":
		return serviceInstall()
	case "uninstall":
		return serviceUninstall()
	case "status":
		return serviceStatus()
	case "start":
		return serviceStart()
	case "stop":
		return serviceStop()
	default:
		return fmt.Errorf("unknown service action: %s (use install|uninstall|status|start|stop)", action)
	}
}

// ---------------------------------------------------------------------------
// Platform paths
// ---------------------------------------------------------------------------

// ServiceExePath returns the path to the current executable.
func ServiceExePath() (string, error) {
	return os.Executable()
}

// LaunchdPlistPath returns the path to the launchd plist file.
func LaunchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "com.muxd.daemon.plist"), nil
}

// SystemdUnitPath returns the path to the systemd user unit file.
func SystemdUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", "muxd.service"), nil
}

// DaemonLogPath returns the path to the daemon log file.
func DaemonLogPath() string {
	dir, err := config.DataDir()
	if err != nil {
		return "/tmp/muxd-daemon.log"
	}
	return filepath.Join(dir, "daemon.log")
}

// ---------------------------------------------------------------------------
// Install
// ---------------------------------------------------------------------------

func serviceInstall() error {
	exe, err := ServiceExePath()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(exe)
	case "linux":
		return installSystemd(exe)
	case "windows":
		return installWindows(exe)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func installLaunchd(exe string) error {
	path, err := LaunchdPlistPath()
	if err != nil {
		return err
	}
	logPath := DaemonLogPath()

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.muxd.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--daemon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
</dict>
</plist>
`, exe, logPath, logPath)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	out, err := exec.Command("launchctl", "load", "-w", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl load: %s: %w", string(out), err)
	}

	fmt.Printf("Service installed: %s\n", path)
	return nil
}

func installSystemd(exe string) error {
	path, err := SystemdUnitPath()
	if err != nil {
		return err
	}
	logPath := DaemonLogPath()

	unit := fmt.Sprintf(`[Unit]
Description=muxd daemon
After=network.target

[Service]
Type=simple
ExecStart=%s --daemon
Restart=on-failure
RestartSec=5
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, exe, logPath, logPath)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating systemd user dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}

	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload: %s: %w", string(out), err)
	}
	if out, err := exec.Command("systemctl", "--user", "enable", "muxd").CombinedOutput(); err != nil {
		return fmt.Errorf("enable: %s: %w", string(out), err)
	}

	fmt.Printf("Service installed: %s\n", path)
	return nil
}

func installWindows(exe string) error {
	value := fmt.Sprintf(`"%s" --daemon`, exe)
	out, err := exec.Command("reg", "add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "muxd", "/t", "REG_SZ", "/d", value, "/f",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	fmt.Println("Service installed (startup registry entry: HKCU\\...\\Run\\muxd)")
	return nil
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

func serviceUninstall() error {
	switch runtime.GOOS {
	case "darwin":
		return uninstallLaunchd()
	case "linux":
		return uninstallSystemd()
	case "windows":
		return uninstallWindows()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func uninstallLaunchd() error {
	path, err := LaunchdPlistPath()
	if err != nil {
		return err
	}
	if err := exec.Command("launchctl", "unload", "-w", path).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service: launchctl unload: %v\n", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}
	fmt.Println("Service uninstalled.")
	return nil
}

func uninstallSystemd() error {
	if err := exec.Command("systemctl", "--user", "stop", "muxd").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service: systemctl stop: %v\n", err)
	}
	if err := exec.Command("systemctl", "--user", "disable", "muxd").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service: systemctl disable: %v\n", err)
	}

	path, err := SystemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service: systemctl daemon-reload: %v\n", err)
	}

	fmt.Println("Service uninstalled.")
	return nil
}

func uninstallWindows() error {
	out, err := exec.Command("reg", "delete",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "muxd", "/f",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg delete: %s: %w", strings.TrimSpace(string(out)), err)
	}
	fmt.Println("Service uninstalled.")
	return nil
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

func serviceStatus() error {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("launchctl", "list", "com.muxd.daemon").CombinedOutput()
		if err != nil {
			fmt.Println("Service is not loaded.")
			return nil
		}
		fmt.Println(string(out))
		return nil

	case "linux":
		out, err := exec.Command("systemctl", "--user", "status", "muxd").CombinedOutput()
		if err != nil {
			// systemctl status returns non-zero for inactive services; output still useful
		}
		fmt.Println(string(out))
		return nil

	case "windows":
		out, err := exec.Command("reg", "query",
			`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v", "muxd",
		).CombinedOutput()
		if err != nil {
			fmt.Println("Service is not installed.")
		} else {
			fmt.Println("Startup entry found:")
			fmt.Println(strings.TrimSpace(string(out)))
		}
		// Also check lockfile for running daemon
		lf, lfErr := daemon.ReadLockfile()
		if lfErr == nil && !daemon.IsLockfileStale(lf) {
			fmt.Printf("Daemon running: PID %d, port %d\n", lf.PID, lf.Port)
		} else {
			fmt.Println("Daemon is not running.")
		}
		return nil

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// ---------------------------------------------------------------------------
// Start / Stop
// ---------------------------------------------------------------------------

func serviceStart() error {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("launchctl", "start", "com.muxd.daemon").CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl start: %s: %w", string(out), err)
		}
		fmt.Println("Service started.")
		return nil

	case "linux":
		out, err := exec.Command("systemctl", "--user", "start", "muxd").CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl start: %s: %w", string(out), err)
		}
		fmt.Println("Service started.")
		return nil

	case "windows":
		// Read the registry to get the command, then launch it
		exe, err := ServiceExePath()
		if err != nil {
			return fmt.Errorf("locating executable: %w", err)
		}
		cmd := exec.Command(exe, "--daemon")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting daemon: %w", err)
		}
		// Detach -- don't wait
		if err := cmd.Process.Release(); err != nil {
			fmt.Fprintf(os.Stderr, "service: release process: %v\n", err)
		}
		fmt.Printf("Daemon started (PID %d).\n", cmd.Process.Pid)
		return nil

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func serviceStop() error {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("launchctl", "stop", "com.muxd.daemon").CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl stop: %s: %w", string(out), err)
		}
		fmt.Println("Service stopped.")
		return nil

	case "linux":
		out, err := exec.Command("systemctl", "--user", "stop", "muxd").CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl stop: %s: %w", string(out), err)
		}
		fmt.Println("Service stopped.")
		return nil

	case "windows":
		// Find and kill the daemon process via lockfile
		lf, err := daemon.ReadLockfile()
		if err != nil {
			return fmt.Errorf("no running daemon found (no lockfile)")
		}
		proc, err := os.FindProcess(lf.PID)
		if err != nil {
			return fmt.Errorf("finding process: %w", err)
		}
		if err := proc.Kill(); err != nil {
			return fmt.Errorf("killing process: %w", err)
		}
		if err := daemon.RemoveLockfile(); err != nil {
			fmt.Fprintf(os.Stderr, "service: remove lockfile: %v\n", err)
		}
		fmt.Println("Service stopped.")
		return nil

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
