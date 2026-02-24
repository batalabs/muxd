package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAndReadLockfile(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, LockfileName)

	// Write lockfile to temp path
	lf := LockfileData{
		PID:       os.Getpid(),
		Port:      4096,
		StartedAt: time.Now(),
	}
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		t.Fatalf("marshaling lockfile: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatalf("writing lockfile: %v", err)
	}

	// Read it back
	readData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lockfile: %v", err)
	}
	var readLf LockfileData
	if err := json.Unmarshal(readData, &readLf); err != nil {
		t.Fatalf("parsing lockfile: %v", err)
	}

	if readLf.PID != os.Getpid() {
		t.Errorf("PID mismatch: got %d, want %d", readLf.PID, os.Getpid())
	}
	if readLf.Port != 4096 {
		t.Errorf("Port mismatch: got %d, want 4096", readLf.Port)
	}
}

func TestRemoveLockfileFromTempDir(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, LockfileName)

	// Create a file
	if err := os.WriteFile(lockPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Remove it
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("removing lockfile: %v", err)
	}

	// Verify gone
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("lockfile still exists after remove")
	}
}

func TestIsProcessAlive(t *testing.T) {
	t.Run("current process is alive", func(t *testing.T) {
		if !IsProcessAlive(os.Getpid()) {
			t.Error("expected current process to be alive")
		}
	})

	t.Run("non-existent process is not alive", func(t *testing.T) {
		if IsProcessAlive(9999999) {
			t.Error("expected non-existent process to not be alive")
		}
	})
}

func TestIsLockfileStale(t *testing.T) {
	t.Run("stale with dead PID", func(t *testing.T) {
		lf := &LockfileData{
			PID:       9999999,
			Port:      4096,
			StartedAt: time.Now().Add(-time.Hour),
		}
		if !IsLockfileStale(lf) {
			t.Error("expected lockfile to be stale with dead PID")
		}
	})

	t.Run("stale with alive PID but no server", func(t *testing.T) {
		lf := &LockfileData{
			PID:       os.Getpid(),
			Port:      59999,
			StartedAt: time.Now(),
		}
		if !IsLockfileStale(lf) {
			t.Error("expected lockfile to be stale when health check fails")
		}
	})
}
