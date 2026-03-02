package hub

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/batalabs/muxd/internal/config"

	_ "modernc.org/sqlite"
)

// OpenHubStore opens (or creates) the hub SQLite database.
// Uses hub.db in the muxd data directory, separate from the agent store.
func OpenHubStore() (*sql.DB, error) {
	dir, err := config.DataDir()
	if err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	dsn := filepath.Join(dir, "hub.db")

	db, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open hub db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping hub db: %w", err)
	}
	if err := migrateHub(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate hub: %w", err)
	}
	return db, nil
}

func migrateHub(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			token TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'online',
			registered_at TEXT NOT NULL DEFAULT (datetime('now')),
			last_seen_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS hub_logs (
			id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL,
			level TEXT NOT NULL DEFAULT 'info',
			message TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`)
	return err
}
