package service

import (
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// HandleCommand dispatch
// ---------------------------------------------------------------------------

func TestHandleCommand_invalid(t *testing.T) {
	err := HandleCommand("invalid")
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
	if !strings.Contains(err.Error(), "unknown service action") {
		t.Errorf("error = %q, want 'unknown service action'", err)
	}
}

func TestHandleCommand_caseInsensitive(t *testing.T) {
	// Only test with "status" — it's read-only on all platforms.
	// Other actions (install, uninstall, start, stop) have real side effects.
	for _, action := range []string{"STATUS", "Status", "status"} {
		t.Run(action, func(t *testing.T) {
			err := HandleCommand(action)
			if err != nil && strings.Contains(err.Error(), "unknown service action") {
				t.Errorf("HandleCommand(%q) returned unknown action error", action)
			}
		})
	}
}

func TestHandleCommand_allActionsRecognized(t *testing.T) {
	// Verify the switch recognizes all valid action strings.
	// We test with mixed case and check that none return "unknown service action".
	// NOTE: install/start/stop/uninstall have real side effects, so we only verify
	// via "status" (safe) and "stop" (fails fast before doing anything when no lockfile).
	for _, action := range []string{"status", "stop"} {
		t.Run(action, func(t *testing.T) {
			err := HandleCommand(action)
			if err != nil && strings.Contains(err.Error(), "unknown service action") {
				t.Errorf("HandleCommand(%q) not recognized", action)
			}
		})
	}
}

func TestHandleCommand_stop_noLockfile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific stop path")
	}
	err := HandleCommand("stop")
	if err == nil {
		t.Fatal("expected error when no daemon is running")
	}
	if !strings.Contains(err.Error(), "lockfile") && !strings.Contains(err.Error(), "daemon") {
		t.Errorf("error = %q, expected lockfile/daemon related error", err)
	}
}

func TestHandleCommand_status(t *testing.T) {
	// Status is read-only on all platforms — should not error.
	err := HandleCommand("status")
	if err != nil {
		t.Errorf("HandleCommand(status) = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// Platform paths — these work on any OS (just path construction)
// ---------------------------------------------------------------------------

func TestServiceExePath(t *testing.T) {
	path, err := ServiceExePath()
	if err != nil {
		t.Fatalf("ServiceExePath() error: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty executable path")
	}
}

func TestLaunchdPlistPath(t *testing.T) {
	path, err := LaunchdPlistPath()
	if err != nil {
		t.Fatalf("LaunchdPlistPath() error: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty plist path")
	}
	if !strings.Contains(path, "com.muxd.daemon.plist") {
		t.Errorf("path = %q, expected to contain plist filename", path)
	}
	if !strings.Contains(path, "LaunchAgents") {
		t.Errorf("path = %q, expected to contain LaunchAgents", path)
	}
}

func TestSystemdUnitPath(t *testing.T) {
	path, err := SystemdUnitPath()
	if err != nil {
		t.Fatalf("SystemdUnitPath() error: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty unit path")
	}
	if !strings.Contains(path, "muxd.service") {
		t.Errorf("path = %q, expected to contain muxd.service", path)
	}
	if !strings.Contains(path, "systemd") {
		t.Errorf("path = %q, expected to contain systemd", path)
	}
}

func TestDaemonLogPath(t *testing.T) {
	path := DaemonLogPath()
	if path == "" {
		t.Error("expected non-empty log path")
	}
	if !strings.HasSuffix(path, "daemon.log") && !strings.HasSuffix(path, "muxd-daemon.log") {
		t.Errorf("path = %q, expected to end with daemon log filename", path)
	}
}

func TestDaemonLogPath_containsDir(t *testing.T) {
	path := DaemonLogPath()
	// Should be an absolute path (not just a filename).
	if !strings.Contains(path, string('/')) && !strings.Contains(path, string('\\')) {
		t.Errorf("path = %q, expected an absolute path", path)
	}
}

// ---------------------------------------------------------------------------
// Hub platform paths
// ---------------------------------------------------------------------------

func TestHubLaunchdPlistPath(t *testing.T) {
	path, err := HubLaunchdPlistPath()
	if err != nil {
		t.Fatalf("HubLaunchdPlistPath() error: %v", err)
	}
	if !strings.Contains(path, "com.muxd.hub.plist") {
		t.Errorf("path = %q, expected to contain com.muxd.hub.plist", path)
	}
	if !strings.Contains(path, "LaunchAgents") {
		t.Errorf("path = %q, expected to contain LaunchAgents", path)
	}
}

func TestHubSystemdUnitPath(t *testing.T) {
	path, err := HubSystemdUnitPath()
	if err != nil {
		t.Fatalf("HubSystemdUnitPath() error: %v", err)
	}
	if !strings.Contains(path, "muxd-hub.service") {
		t.Errorf("path = %q, expected to contain muxd-hub.service", path)
	}
	if !strings.Contains(path, "systemd") {
		t.Errorf("path = %q, expected to contain systemd", path)
	}
}

func TestHubLogPath(t *testing.T) {
	path := HubLogPath()
	if path == "" {
		t.Error("expected non-empty log path")
	}
	if !strings.Contains(path, "hub.log") {
		t.Errorf("path = %q, expected to contain hub.log", path)
	}
}

func TestHandleCommand_hubActionsRecognized(t *testing.T) {
	// status-hub and stop-hub are safe to call — they just read state / fail fast.
	for _, action := range []string{"status-hub", "stop-hub"} {
		t.Run(action, func(t *testing.T) {
			err := HandleCommand(action)
			if err != nil && strings.Contains(err.Error(), "unknown service action") {
				t.Errorf("HandleCommand(%q) not recognized", action)
			}
		})
	}
}

func TestHandleCommand_statusHub(t *testing.T) {
	err := HandleCommand("status-hub")
	if err != nil {
		t.Errorf("HandleCommand(status-hub) = %v, want nil", err)
	}
}

func TestHandleCommand_stopHub_noLockfile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific stop-hub path")
	}
	err := HandleCommand("stop-hub")
	if err == nil {
		t.Fatal("expected error when no hub is running")
	}
	if !strings.Contains(err.Error(), "lockfile") && !strings.Contains(err.Error(), "hub") {
		t.Errorf("error = %q, expected lockfile/hub related error", err)
	}
}
