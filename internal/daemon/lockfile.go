package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/batalabs/muxd/internal/config"
)

// LockfileData is the JSON structure stored in the daemon lockfile.
type LockfileData struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Token     string    `json:"token,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// LockfileName is the filename of the daemon lockfile.
const LockfileName = "server.lock"

// LockfilePath returns the path to the daemon lockfile.
func LockfilePath() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", fmt.Errorf("lockfile path: %w", err)
	}
	return filepath.Join(dir, LockfileName), nil
}

// WriteLockfile writes the daemon lockfile with the current PID, port, and timestamp.
func WriteLockfile(port int, token string) error {
	p, err := LockfilePath()
	if err != nil {
		return err
	}
	data := LockfileData{
		PID:       os.Getpid(),
		Port:      port,
		Token:     token,
		StartedAt: time.Now(),
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling lockfile: %w", err)
	}
	return os.WriteFile(p, b, 0o600)
}

// ReadLockfile reads and parses the daemon lockfile.
// Returns an error if the file does not exist or cannot be parsed.
func ReadLockfile() (*LockfileData, error) {
	p, err := LockfilePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("reading lockfile: %w", err)
	}
	var lf LockfileData
	if err := json.Unmarshal(b, &lf); err != nil {
		return nil, fmt.Errorf("parsing lockfile: %w", err)
	}
	return &lf, nil
}

// RemoveLockfile removes the daemon lockfile.
func RemoveLockfile() error {
	p, err := LockfilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing lockfile: %w", err)
	}
	return nil
}

// IsLockfileStale checks whether the lockfile refers to a running, healthy daemon.
// Returns true if the lockfile is stale (process dead or not responding).
func IsLockfileStale(lf *LockfileData) bool {
	if !IsProcessAlive(lf.PID) {
		return true
	}
	// PID is alive -- verify with HTTP health check
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/health", lf.Port))
	if err != nil {
		return true
	}
	resp.Body.Close()
	return resp.StatusCode != http.StatusOK
}
