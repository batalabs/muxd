package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger writes timestamped log lines to ~/.local/share/muxd/muxd.log.
type Logger struct {
	mu   sync.Mutex
	file *os.File
}

// logFilePath returns the path to the muxd log file.
func logFilePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "muxd.log"), nil
}

// LogPath returns the log file path (for tools to read).
func LogPath() string {
	p, err := logFilePath()
	if err != nil {
		return ""
	}
	return p
}

// NewLogger creates a logger that appends to ~/.local/share/muxd/muxd.log.
func NewLogger() *Logger {
	l := &Logger{}

	p, err := logFilePath()
	if err != nil {
		return l
	}

	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return l
	}

	l.file = f
	return l
}

// Printf writes a timestamped log line to the log file.
func (l *Logger) Printf(format string, args ...any) {
	if l.file == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ts := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	fmt.Fprintf(l.file, ts+" "+format+"\n", args...)
}

// Close closes the log file.
func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}
