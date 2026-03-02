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
	"github.com/batalabs/muxd/internal/hub"
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
	case "install-hub":
		return hubServiceInstall()
	case "uninstall-hub":
		return hubServiceUninstall()
	case "start-hub":
		return hubServiceStart()
	case "stop-hub":
		return hubServiceStop()
	case "status-hub":
		return hubServiceStatus()
	default:
		return fmt.Errorf("unknown service action: %s (use install|uninstall|status|start|stop|install-hub|uninstall-hub|start-hub|stop-hub|status-hub)", action)
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
// Hub platform paths
// ---------------------------------------------------------------------------

// HubLaunchdPlistPath returns the path to the hub launchd plist file.
func HubLaunchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "com.muxd.hub.plist"), nil
}

// HubSystemdUnitPath returns the path to the hub systemd user unit file.
func HubSystemdUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", "muxd-hub.service"), nil
}

// HubLogPath returns the path to the hub log file.
func HubLogPath() string {
	dir, err := config.DataDir()
	if err != nil {
		return "/tmp/muxd-hub.log"
	}
	return filepath.Join(dir, "hub.log")
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

// ===========================================================================
// Hub service management
// ===========================================================================

// ---------------------------------------------------------------------------
// Hub Install
// ---------------------------------------------------------------------------

func hubServiceInstall() error {
	exe, err := ServiceExePath()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return hubInstallLaunchd(exe)
	case "linux":
		return hubInstallSystemd(exe)
	case "windows":
		return hubInstallWindows(exe)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func hubInstallLaunchd(exe string) error {
	path, err := HubLaunchdPlistPath()
	if err != nil {
		return err
	}
	logPath := HubLogPath()

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.muxd.hub</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--hub</string>
        <string>--hub-bind</string>
        <string>0.0.0.0</string>
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

	fmt.Printf("Hub service installed: %s\n", path)
	return nil
}

func hubInstallSystemd(exe string) error {
	path, err := HubSystemdUnitPath()
	if err != nil {
		return err
	}
	logPath := HubLogPath()

	unit := fmt.Sprintf(`[Unit]
Description=muxd hub
After=network.target

[Service]
Type=simple
ExecStart=%s --hub --hub-bind 0.0.0.0
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
	if out, err := exec.Command("systemctl", "--user", "enable", "muxd-hub").CombinedOutput(); err != nil {
		return fmt.Errorf("enable: %s: %w", string(out), err)
	}

	fmt.Printf("Hub service installed: %s\n", path)
	fmt.Println("NOTE: run 'loginctl enable-linger $USER' to keep the hub running after SSH logout.")
	return nil
}

func hubInstallWindows(exe string) error {
	value := fmt.Sprintf(`"%s" --hub --hub-bind 0.0.0.0`, exe)
	out, err := exec.Command("reg", "add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "muxd-hub", "/t", "REG_SZ", "/d", value, "/f",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	fmt.Println("Hub service installed (startup registry entry: HKCU\\...\\Run\\muxd-hub)")
	return nil
}

// ---------------------------------------------------------------------------
// Hub Uninstall
// ---------------------------------------------------------------------------

func hubServiceUninstall() error {
	switch runtime.GOOS {
	case "darwin":
		return hubUninstallLaunchd()
	case "linux":
		return hubUninstallSystemd()
	case "windows":
		return hubUninstallWindows()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func hubUninstallLaunchd() error {
	path, err := HubLaunchdPlistPath()
	if err != nil {
		return err
	}
	if err := exec.Command("launchctl", "unload", "-w", path).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service: launchctl unload: %v\n", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}
	fmt.Println("Hub service uninstalled.")
	return nil
}

func hubUninstallSystemd() error {
	if err := exec.Command("systemctl", "--user", "stop", "muxd-hub").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service: systemctl stop: %v\n", err)
	}
	if err := exec.Command("systemctl", "--user", "disable", "muxd-hub").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service: systemctl disable: %v\n", err)
	}

	path, err := HubSystemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "service: systemctl daemon-reload: %v\n", err)
	}

	fmt.Println("Hub service uninstalled.")
	return nil
}

func hubUninstallWindows() error {
	out, err := exec.Command("reg", "delete",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v", "muxd-hub", "/f",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg delete: %s: %w", strings.TrimSpace(string(out)), err)
	}
	fmt.Println("Hub service uninstalled.")
	return nil
}

// ---------------------------------------------------------------------------
// Hub Status
// ---------------------------------------------------------------------------

func hubServiceStatus() error {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("launchctl", "list", "com.muxd.hub").CombinedOutput()
		if err != nil {
			fmt.Println("Hub service is not loaded.")
			return nil
		}
		fmt.Println(string(out))
		return nil

	case "linux":
		out, err := exec.Command("systemctl", "--user", "status", "muxd-hub").CombinedOutput()
		if err != nil {
			// systemctl status returns non-zero for inactive services; output still useful
		}
		fmt.Println(string(out))
		return nil

	case "windows":
		out, err := exec.Command("reg", "query",
			`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v", "muxd-hub",
		).CombinedOutput()
		if err != nil {
			fmt.Println("Hub service is not installed.")
		} else {
			fmt.Println("Hub startup entry found:")
			fmt.Println(strings.TrimSpace(string(out)))
		}
		// Also check hub lockfile for running hub
		lf, lfErr := hub.ReadHubLockfile()
		if lfErr == nil && lf.PID > 0 {
			fmt.Printf("Hub running: PID %d, port %d\n", lf.PID, lf.Port)
		} else {
			fmt.Println("Hub is not running.")
		}
		return nil

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// ---------------------------------------------------------------------------
// Hub Start / Stop
// ---------------------------------------------------------------------------

func hubServiceStart() error {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("launchctl", "start", "com.muxd.hub").CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl start: %s: %w", string(out), err)
		}
		fmt.Println("Hub service started.")
		return nil

	case "linux":
		out, err := exec.Command("systemctl", "--user", "start", "muxd-hub").CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl start: %s: %w", string(out), err)
		}
		fmt.Println("Hub service started.")
		return nil

	case "windows":
		exe, err := ServiceExePath()
		if err != nil {
			return fmt.Errorf("locating executable: %w", err)
		}
		cmd := exec.Command(exe, "--hub", "--hub-bind", "0.0.0.0")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting hub: %w", err)
		}
		if err := cmd.Process.Release(); err != nil {
			fmt.Fprintf(os.Stderr, "service: release process: %v\n", err)
		}
		fmt.Printf("Hub started (PID %d).\n", cmd.Process.Pid)
		return nil

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func hubServiceStop() error {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("launchctl", "stop", "com.muxd.hub").CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl stop: %s: %w", string(out), err)
		}
		fmt.Println("Hub service stopped.")
		return nil

	case "linux":
		out, err := exec.Command("systemctl", "--user", "stop", "muxd-hub").CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl stop: %s: %w", string(out), err)
		}
		fmt.Println("Hub service stopped.")
		return nil

	case "windows":
		lf, err := hub.ReadHubLockfile()
		if err != nil {
			return fmt.Errorf("no running hub found (no lockfile)")
		}
		proc, err := os.FindProcess(lf.PID)
		if err != nil {
			return fmt.Errorf("finding process: %w", err)
		}
		if err := proc.Kill(); err != nil {
			return fmt.Errorf("killing process: %w", err)
		}
		fmt.Println("Hub service stopped.")
		return nil

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
