package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/batalabs/muxd/internal/config"
	"github.com/batalabs/muxd/internal/domain"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database for session and message persistence.
type Store struct {
	db *sql.DB
}

// OpenStore opens (or creates) the SQLite database in the muxd data directory.
func OpenStore() (*Store, error) {
	dir, err := config.DataDir()
	if err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	dsn := filepath.Join(dir, "muxd.db")

	db, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// NewFromDB creates a Store from an existing *sql.DB and runs migrations.
// This is useful for testing with an in-memory database.
func NewFromDB(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	// Create tables (IF NOT EXISTS so we don't overwrite).
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT 'New Session',
			model TEXT NOT NULL DEFAULT '',
			total_tokens INTEGER DEFAULT 0,
			message_count INTEGER DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			tokens INTEGER DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			sequence INTEGER NOT NULL
		);
	`); err != nil {
		return err
	}

	// Add missing columns to existing DBs before creating indexes.
	// Ignore errors (column already exists).
	for _, q := range []string{
		`ALTER TABLE sessions ADD COLUMN project_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN total_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN message_count INTEGER DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN content TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN tokens INTEGER DEFAULT 0`,
		`ALTER TABLE messages ADD COLUMN content_type TEXT NOT NULL DEFAULT 'text'`,
		`ALTER TABLE sessions ADD COLUMN input_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN output_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN branch_point INTEGER DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN tags TEXT NOT NULL DEFAULT ''`,
	} {
		// ALTER TABLE errors expected — column may already exist.
		if _, err := s.db.Exec(q); err != nil {
			// expected: column already exists
		}
	}

	// Migrate from old 'message_text' column to 'content', then drop the old column.
	// This handles databases created with an older schema.
	if _, err := s.db.Exec(`UPDATE messages SET content = message_text WHERE content = '' AND message_text IS NOT NULL AND message_text != ''`); err != nil {
		// expected: message_text column may not exist
	}
	if _, err := s.db.Exec(`ALTER TABLE messages DROP COLUMN message_text`); err != nil {
		// expected: column may not exist or already dropped
	}

	// Compactions table for persistent compaction state.
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS compactions (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			summary_text TEXT NOT NULL,
			cutoff_sequence INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`); err != nil {
		return err
	}

	// Scheduled tweets queue.
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS scheduled_tweets (
			id TEXT PRIMARY KEY,
			text TEXT NOT NULL,
			scheduled_for TEXT NOT NULL,
			recurrence TEXT NOT NULL DEFAULT 'once',
			status TEXT NOT NULL DEFAULT 'pending',
			tweet_id TEXT NOT NULL DEFAULT '',
			error_text TEXT NOT NULL DEFAULT '',
			last_attempt_at TEXT,
			posted_at TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`); err != nil {
		return err
	}

	// Generic scheduled tool-call jobs.
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS scheduled_tool_jobs (
			id TEXT PRIMARY KEY,
			tool_name TEXT NOT NULL,
			tool_input_json TEXT NOT NULL,
			scheduled_for TEXT NOT NULL,
			recurrence TEXT NOT NULL DEFAULT 'once',
			status TEXT NOT NULL DEFAULT 'pending',
			attempt_count INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			last_result TEXT NOT NULL DEFAULT '',
			last_attempt_at TEXT,
			completed_at TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`); err != nil {
		return err
	}

	// Create indexes (after columns exist).
	_, err := s.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_path);
		CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);
		CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, sequence);
		CREATE INDEX IF NOT EXISTS idx_compactions_session ON compactions(session_id);
		CREATE INDEX IF NOT EXISTS idx_scheduled_tweets_due ON scheduled_tweets(status, scheduled_for);
		CREATE INDEX IF NOT EXISTS idx_scheduled_tool_jobs_due ON scheduled_tool_jobs(status, scheduled_for);
	`)
	return err
}

// ---------------------------------------------------------------------------
// Session CRUD
// ---------------------------------------------------------------------------

// CreateSession inserts a new session with the given project path and model.
func (s *Store) CreateSession(projectPath, model string) (*domain.Session, error) {
	sess := &domain.Session{
		ID:          domain.NewUUID(),
		ProjectPath: projectPath,
		Title:       "New Session",
		Model:       model,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, project_path, title, model, created_at, updated_at)
		 VALUES (?, ?, ?, ?, datetime(?), datetime(?))`,
		sess.ID, sess.ProjectPath, sess.Title, sess.Model,
		sess.CreatedAt.UTC().Format(time.RFC3339),
		sess.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// GetSession retrieves a session by its full ID.
func (s *Store) GetSession(id string) (*domain.Session, error) {
	row := s.db.QueryRow(
		`SELECT id, project_path, title, model, total_tokens, COALESCE(input_tokens,0), COALESCE(output_tokens,0), message_count, COALESCE(parent_session_id,''), COALESCE(branch_point,0), COALESCE(tags,''), created_at, updated_at
		 FROM sessions WHERE id = ?`, id)
	return scanSession(row)
}

// LatestSession returns the most recently updated session for a project path.
func (s *Store) LatestSession(projectPath string) (*domain.Session, error) {
	row := s.db.QueryRow(
		`SELECT id, project_path, title, model, total_tokens, COALESCE(input_tokens,0), COALESCE(output_tokens,0), message_count, COALESCE(parent_session_id,''), COALESCE(branch_point,0), COALESCE(tags,''), created_at, updated_at
		 FROM sessions WHERE project_path = ? ORDER BY updated_at DESC LIMIT 1`, projectPath)
	return scanSession(row)
}

// ListSessions returns the most recent sessions for a project path, up to limit.
func (s *Store) ListSessions(projectPath string, limit int) ([]domain.Session, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(
		`SELECT id, project_path, title, model, total_tokens, COALESCE(input_tokens,0), COALESCE(output_tokens,0), message_count, COALESCE(parent_session_id,''), COALESCE(branch_point,0), COALESCE(tags,''), created_at, updated_at
		 FROM sessions WHERE project_path = ? ORDER BY updated_at DESC LIMIT ?`,
		projectPath, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []domain.Session
	for rows.Next() {
		var sess domain.Session
		var createdStr, updatedStr string
		if err := rows.Scan(&sess.ID, &sess.ProjectPath, &sess.Title, &sess.Model,
			&sess.TotalTokens, &sess.InputTokens, &sess.OutputTokens,
			&sess.MessageCount, &sess.ParentSessionID, &sess.BranchPoint,
			&sess.Tags, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		if t, err := time.Parse("2006-01-02 15:04:05", createdStr); err == nil {
			sess.CreatedAt = t
		}
		if t, err := time.Parse("2006-01-02 15:04:05", updatedStr); err == nil {
			sess.UpdatedAt = t
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// DeleteSession removes a session and its messages (via ON DELETE CASCADE).
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// UpdateSessionTitle sets the title of a session.
func (s *Store) UpdateSessionTitle(id, title string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET title = ?, updated_at = datetime('now') WHERE id = ?`,
		title, id)
	return err
}

// UpdateSessionTokens sets the token counts for a session.
func (s *Store) UpdateSessionTokens(id string, inputTokens, outputTokens int) error {
	totalTokens := inputTokens + outputTokens
	_, err := s.db.Exec(
		`UPDATE sessions SET total_tokens = ?, input_tokens = ?, output_tokens = ?, updated_at = datetime('now') WHERE id = ?`,
		totalTokens, inputTokens, outputTokens, id)
	return err
}

// UpdateSessionModel sets the model for a session.
func (s *Store) UpdateSessionModel(id, model string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET model = ?, updated_at = datetime('now') WHERE id = ?`,
		model, id)
	return err
}

// UpdateSessionTags sets the tags for a session.
func (s *Store) UpdateSessionTags(id, tags string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET tags = ?, updated_at = datetime('now') WHERE id = ?`,
		tags, id)
	return err
}

// TouchSession updates the session's updated_at timestamp.
func (s *Store) TouchSession(id string) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET updated_at = datetime('now') WHERE id = ?`, id)
	return err
}

// ---------------------------------------------------------------------------
// Message CRUD
// ---------------------------------------------------------------------------

// AppendMessage stores a plain-text message for a session.
func (s *Store) AppendMessage(sessionID, role, content string, tokens int) error {
	var seq int
	row := s.db.QueryRow(
		`SELECT COALESCE(MAX(sequence), 0) FROM messages WHERE session_id = ?`, sessionID)
	if err := row.Scan(&seq); err != nil {
		return err
	}
	seq++

	_, err := s.db.Exec(
		`INSERT INTO messages (id, session_id, role, content, tokens, sequence)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		domain.NewUUID(), sessionID, role, content, tokens, seq)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE sessions SET message_count = ?, updated_at = datetime('now') WHERE id = ?`,
		seq, sessionID)
	return err
}

// AppendMessageBlocks stores a message with structured content blocks for a session.
func (s *Store) AppendMessageBlocks(sessionID, role string, blocks []domain.ContentBlock, tokens int) error {
	var seq int
	row := s.db.QueryRow(
		`SELECT COALESCE(MAX(sequence), 0) FROM messages WHERE session_id = ?`, sessionID)
	if err := row.Scan(&seq); err != nil {
		return err
	}
	seq++

	blocksJSON, err := json.Marshal(blocks)
	if err != nil {
		return fmt.Errorf("marshaling blocks: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO messages (id, session_id, role, content, content_type, tokens, sequence)
		 VALUES (?, ?, ?, ?, 'blocks', ?, ?)`,
		domain.NewUUID(), sessionID, role, string(blocksJSON), tokens, seq)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE sessions SET message_count = ?, updated_at = datetime('now') WHERE id = ?`,
		seq, sessionID)
	return err
}

// GetMessages returns all messages for a session, ordered by sequence.
func (s *Store) GetMessages(sessionID string) ([]domain.TranscriptMessage, error) {
	rows, err := s.db.Query(
		`SELECT role, content, COALESCE(content_type, 'text') FROM messages WHERE session_id = ? ORDER BY sequence`,
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []domain.TranscriptMessage
	for rows.Next() {
		var m domain.TranscriptMessage
		var contentType string
		if err := rows.Scan(&m.Role, &m.Content, &contentType); err != nil {
			return nil, err
		}
		if contentType == "blocks" {
			var blocks []domain.ContentBlock
			if err := json.Unmarshal([]byte(m.Content), &blocks); err == nil {
				m.Blocks = blocks
				var texts []string
				for _, b := range blocks {
					if b.Type == "text" {
						texts = append(texts, b.Text)
					}
				}
				m.Content = strings.Join(texts, "\n")
			}
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// ---------------------------------------------------------------------------
// Compaction persistence
// ---------------------------------------------------------------------------

// SaveCompaction persists a compaction record for a session.
func (s *Store) SaveCompaction(sessionID, summaryText string, cutoffSequence int) error {
	_, err := s.db.Exec(
		`INSERT INTO compactions (id, session_id, summary_text, cutoff_sequence)
		 VALUES (?, ?, ?, ?)`,
		domain.NewUUID(), sessionID, summaryText, cutoffSequence)
	return err
}

// LatestCompaction returns the most recent compaction for a session.
// Returns sql.ErrNoRows if no compaction exists.
func (s *Store) LatestCompaction(sessionID string) (summaryText string, cutoffSequence int, err error) {
	err = s.db.QueryRow(
		`SELECT summary_text, cutoff_sequence FROM compactions
		 WHERE session_id = ? ORDER BY rowid DESC LIMIT 1`, sessionID).
		Scan(&summaryText, &cutoffSequence)
	return
}

// GetMessagesAfterSequence returns messages with sequence > afterSequence, ordered by sequence.
func (s *Store) GetMessagesAfterSequence(sessionID string, afterSequence int) ([]domain.TranscriptMessage, error) {
	rows, err := s.db.Query(
		`SELECT role, content, COALESCE(content_type, 'text') FROM messages
		 WHERE session_id = ? AND sequence > ? ORDER BY sequence`,
		sessionID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []domain.TranscriptMessage
	for rows.Next() {
		var m domain.TranscriptMessage
		var contentType string
		if err := rows.Scan(&m.Role, &m.Content, &contentType); err != nil {
			return nil, err
		}
		if contentType == "blocks" {
			var blocks []domain.ContentBlock
			if err := json.Unmarshal([]byte(m.Content), &blocks); err == nil {
				m.Blocks = blocks
				var texts []string
				for _, b := range blocks {
					if b.Type == "text" {
						texts = append(texts, b.Text)
					}
				}
				m.Content = strings.Join(texts, "\n")
			}
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// MessageMaxSequence returns the highest message sequence number for a session, or 0 if none.
func (s *Store) MessageMaxSequence(sessionID string) (int, error) {
	var seq int
	err := s.db.QueryRow(
		`SELECT COALESCE(MAX(sequence), 0) FROM messages WHERE session_id = ?`, sessionID).
		Scan(&seq)
	return seq, err
}

// ---------------------------------------------------------------------------
// Scheduled tweets
// ---------------------------------------------------------------------------

// ScheduledToolJob is a queued tool call for deferred execution.
type ScheduledToolJob struct {
	ID            string
	ToolName      string
	ToolInput     map[string]any
	ScheduledFor  time.Time
	Recurrence    string
	Status        string
	AttemptCount  int
	LastError     string
	LastResult    string
	LastAttemptAt *time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
}

// CreateScheduledToolJob enqueues a generic tool call for future execution.
func (s *Store) CreateScheduledToolJob(toolName string, toolInput map[string]any, scheduledFor time.Time, recurrence string) (string, error) {
	if recurrence == "" {
		recurrence = "once"
	}
	payload, err := json.Marshal(toolInput)
	if err != nil {
		return "", fmt.Errorf("marshal tool input: %w", err)
	}
	id := domain.NewUUID()
	_, err = s.db.Exec(
		`INSERT INTO scheduled_tool_jobs (id, tool_name, tool_input_json, scheduled_for, recurrence, status)
		 VALUES (?, ?, ?, ?, ?, 'pending')`,
		id, strings.ToLower(strings.TrimSpace(toolName)), string(payload), scheduledFor.UTC().Format(time.RFC3339), recurrence,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListScheduledToolJobs returns queued jobs ordered by schedule time.
func (s *Store) ListScheduledToolJobs(limit int) ([]ScheduledToolJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, tool_name, tool_input_json, scheduled_for, recurrence, status, attempt_count, last_error, last_result,
		        COALESCE(last_attempt_at,''), COALESCE(completed_at,''), created_at
		   FROM scheduled_tool_jobs
		  ORDER BY scheduled_for ASC
		  LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduledToolJobs(rows)
}

// DueScheduledToolJobs returns pending jobs due for execution.
func (s *Store) DueScheduledToolJobs(now time.Time, limit int) ([]ScheduledToolJob, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, tool_name, tool_input_json, scheduled_for, recurrence, status, attempt_count, last_error, last_result,
		        COALESCE(last_attempt_at,''), COALESCE(completed_at,''), created_at
		   FROM scheduled_tool_jobs
		  WHERE status = 'pending' AND scheduled_for <= ?
		  ORDER BY scheduled_for ASC
		  LIMIT ?`,
		now.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduledToolJobs(rows)
}

// CancelScheduledToolJob marks a job as cancelled.
func (s *Store) CancelScheduledToolJob(id string) error {
	_, err := s.db.Exec(
		`UPDATE scheduled_tool_jobs
		    SET status = 'cancelled'
		  WHERE id = ? AND status IN ('pending', 'failed')`,
		id,
	)
	return err
}

// MarkScheduledToolJobSucceeded records a successful execution.
func (s *Store) MarkScheduledToolJobSucceeded(id, result string, completedAt time.Time) error {
	result = truncateStoreText(result, 4000)
	_, err := s.db.Exec(
		`UPDATE scheduled_tool_jobs
		    SET status = 'completed',
		        last_result = ?,
		        last_error = '',
		        attempt_count = attempt_count + 1,
		        last_attempt_at = ?,
		        completed_at = ?
		  WHERE id = ?`,
		result, completedAt.UTC().Format(time.RFC3339), completedAt.UTC().Format(time.RFC3339), id,
	)
	return err
}

// MarkScheduledToolJobFailed records a failed execution.
func (s *Store) MarkScheduledToolJobFailed(id, lastErr, result string, attemptedAt time.Time) error {
	lastErr = truncateStoreText(lastErr, 2000)
	result = truncateStoreText(result, 4000)
	_, err := s.db.Exec(
		`UPDATE scheduled_tool_jobs
		    SET status = 'failed',
		        last_error = ?,
		        last_result = ?,
		        attempt_count = attempt_count + 1,
		        last_attempt_at = ?,
		        completed_at = NULL
		  WHERE id = ?`,
		lastErr, result, attemptedAt.UTC().Format(time.RFC3339), id,
	)
	return err
}

// UpdateScheduledToolJob updates a pending job's tool input, scheduled time, and/or recurrence.
// Only fields with non-nil/non-empty values are updated. Only modifies jobs with status = 'pending'.
func (s *Store) UpdateScheduledToolJob(id string, toolInput map[string]any, scheduledFor *time.Time, recurrence *string) error {
	var setClauses []string
	var args []any

	if toolInput != nil {
		payload, err := json.Marshal(toolInput)
		if err != nil {
			return fmt.Errorf("marshal tool input: %w", err)
		}
		setClauses = append(setClauses, "tool_input_json = ?")
		args = append(args, string(payload))
	}
	if scheduledFor != nil {
		setClauses = append(setClauses, "scheduled_for = ?")
		args = append(args, scheduledFor.UTC().Format(time.RFC3339))
	}
	if recurrence != nil {
		setClauses = append(setClauses, "recurrence = ?")
		args = append(args, *recurrence)
	}

	if len(setClauses) == 0 {
		return fmt.Errorf("no fields to update")
	}

	args = append(args, id)
	query := fmt.Sprintf(
		"UPDATE scheduled_tool_jobs SET %s WHERE id = ? AND status = 'pending'",
		strings.Join(setClauses, ", "),
	)
	_, err := s.db.Exec(query, args...)
	return err
}

// RescheduleScheduledToolJob sets next execution time for recurring jobs.
func (s *Store) RescheduleScheduledToolJob(id string, next time.Time) error {
	_, err := s.db.Exec(
		`UPDATE scheduled_tool_jobs
		    SET status = 'pending',
		        scheduled_for = ?,
		        last_error = '',
		        completed_at = NULL
		  WHERE id = ?`,
		next.UTC().Format(time.RFC3339), id,
	)
	return err
}

// ScheduledTweet is a queued tweet for deferred posting.
type ScheduledTweet struct {
	ID            string
	Text          string
	ScheduledFor  time.Time
	Recurrence    string
	Status        string
	TweetID       string
	ErrorText     string
	LastAttemptAt *time.Time
	PostedAt      *time.Time
	CreatedAt     time.Time
}

// CreateScheduledTweet enqueues a tweet for future posting.
func (s *Store) CreateScheduledTweet(text string, scheduledFor time.Time, recurrence string) (string, error) {
	if recurrence == "" {
		recurrence = "once"
	}
	id := domain.NewUUID()
	_, err := s.db.Exec(
		`INSERT INTO scheduled_tweets (id, text, scheduled_for, recurrence, status)
		 VALUES (?, ?, ?, ?, 'pending')`,
		id, text, scheduledFor.UTC().Format(time.RFC3339), recurrence,
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListScheduledTweets returns scheduled tweets ordered by schedule time.
func (s *Store) ListScheduledTweets(limit int) ([]ScheduledTweet, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, text, scheduled_for, recurrence, status, tweet_id, error_text, COALESCE(last_attempt_at, ''), COALESCE(posted_at, ''), created_at
		 FROM scheduled_tweets
		 ORDER BY scheduled_for ASC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduledTweets(rows)
}

// DueScheduledTweets returns pending jobs whose schedule is due.
func (s *Store) DueScheduledTweets(now time.Time, limit int) ([]ScheduledTweet, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, text, scheduled_for, recurrence, status, tweet_id, error_text, COALESCE(last_attempt_at, ''), COALESCE(posted_at, ''), created_at
		 FROM scheduled_tweets
		 WHERE status = 'pending' AND scheduled_for <= ?
		 ORDER BY scheduled_for ASC
		 LIMIT ?`,
		now.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScheduledTweets(rows)
}

// CancelScheduledTweet marks a scheduled tweet as cancelled.
func (s *Store) CancelScheduledTweet(id string) error {
	_, err := s.db.Exec(
		`UPDATE scheduled_tweets
		 SET status = 'cancelled'
		 WHERE id = ? AND status IN ('pending', 'failed')`,
		id,
	)
	return err
}

// MarkScheduledTweetPosted marks a queue entry as posted.
func (s *Store) MarkScheduledTweetPosted(id, tweetID string, postedAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE scheduled_tweets
		 SET status = 'posted', tweet_id = ?, error_text = '', last_attempt_at = ?, posted_at = ?
		 WHERE id = ?`,
		tweetID, postedAt.UTC().Format(time.RFC3339), postedAt.UTC().Format(time.RFC3339), id,
	)
	return err
}

// MarkScheduledTweetFailed records a failed post attempt.
func (s *Store) MarkScheduledTweetFailed(id, errorText string, attemptedAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE scheduled_tweets
		 SET status = 'failed', error_text = ?, last_attempt_at = ?
		 WHERE id = ?`,
		errorText, attemptedAt.UTC().Format(time.RFC3339), id,
	)
	return err
}

// RescheduleRecurringTweet updates a recurring entry for the next run.
func (s *Store) RescheduleRecurringTweet(id string, next time.Time) error {
	_, err := s.db.Exec(
		`UPDATE scheduled_tweets
		 SET status = 'pending', scheduled_for = ?, tweet_id = '', error_text = '', posted_at = NULL
		 WHERE id = ?`,
		next.UTC().Format(time.RFC3339), id,
	)
	return err
}

func scanScheduledTweets(rows *sql.Rows) ([]ScheduledTweet, error) {
	var out []ScheduledTweet
	for rows.Next() {
		var item ScheduledTweet
		var scheduledForStr, lastAttemptStr, postedAtStr, createdAtStr string
		if err := rows.Scan(
			&item.ID,
			&item.Text,
			&scheduledForStr,
			&item.Recurrence,
			&item.Status,
			&item.TweetID,
			&item.ErrorText,
			&lastAttemptStr,
			&postedAtStr,
			&createdAtStr,
		); err != nil {
			return nil, err
		}
		if t, err := parseAnyTime(scheduledForStr); err == nil {
			item.ScheduledFor = t
		}
		if t, err := parseAnyTime(createdAtStr); err == nil {
			item.CreatedAt = t
		}
		if t, ok := parseOptionalTime(lastAttemptStr); ok {
			item.LastAttemptAt = &t
		}
		if t, ok := parseOptionalTime(postedAtStr); ok {
			item.PostedAt = &t
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanScheduledToolJobs(rows *sql.Rows) ([]ScheduledToolJob, error) {
	var out []ScheduledToolJob
	for rows.Next() {
		var item ScheduledToolJob
		var inputJSON, scheduledForStr, lastAttemptStr, completedAtStr, createdAtStr string
		if err := rows.Scan(
			&item.ID,
			&item.ToolName,
			&inputJSON,
			&scheduledForStr,
			&item.Recurrence,
			&item.Status,
			&item.AttemptCount,
			&item.LastError,
			&item.LastResult,
			&lastAttemptStr,
			&completedAtStr,
			&createdAtStr,
		); err != nil {
			return nil, err
		}
		if t, err := parseAnyTime(scheduledForStr); err == nil {
			item.ScheduledFor = t
		}
		if t, err := parseAnyTime(createdAtStr); err == nil {
			item.CreatedAt = t
		}
		if strings.TrimSpace(inputJSON) != "" {
			if err := json.Unmarshal([]byte(inputJSON), &item.ToolInput); err != nil {
				fmt.Fprintf(os.Stderr, "store: unmarshal tool input: %v\n", err)
			}
		}
		if item.ToolInput == nil {
			item.ToolInput = map[string]any{}
		}
		if t, ok := parseOptionalTime(lastAttemptStr); ok {
			item.LastAttemptAt = &t
		}
		if t, ok := parseOptionalTime(completedAtStr); ok {
			item.CompletedAt = &t
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func truncateStoreText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseOptionalTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := parseAnyTime(s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func parseAnyTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}

// ---------------------------------------------------------------------------
// Branching
// ---------------------------------------------------------------------------

// BranchSession creates a new session forked from fromSessionID, copying
// messages up to atSequence. If atSequence <= 0, all messages are copied.
func (s *Store) BranchSession(fromSessionID string, atSequence int) (*domain.Session, error) {
	src, err := s.GetSession(fromSessionID)
	if err != nil {
		return nil, fmt.Errorf("source session: %w", err)
	}

	if atSequence <= 0 {
		maxSeq, seqErr := s.MessageMaxSequence(fromSessionID)
		if seqErr != nil {
			return nil, fmt.Errorf("max sequence: %w", seqErr)
		}
		atSequence = maxSeq
	}

	newID := domain.NewUUID()
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Insert new session with parent reference.
	_, err = tx.Exec(
		`INSERT INTO sessions (id, project_path, title, model, total_tokens, input_tokens, output_tokens, message_count, parent_session_id, branch_point, tags, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, datetime(?), datetime(?))`,
		newID, src.ProjectPath, src.Title+" (branch)", src.Model,
		0, 0, 0,
		fromSessionID, atSequence, src.Tags,
		now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}

	// Copy messages up to atSequence with new UUIDs and renumbered sequence.
	rows, err := tx.Query(
		`SELECT role, content, COALESCE(content_type, 'text'), tokens, sequence FROM messages
		 WHERE session_id = ? AND sequence <= ? ORDER BY sequence`,
		fromSessionID, atSequence)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}

	var count int
	for rows.Next() {
		var role, content, contentType string
		var tokens, seq int
		if err := rows.Scan(&role, &content, &contentType, &tokens, &seq); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan message: %w", err)
		}
		count++
		_, err = tx.Exec(
			`INSERT INTO messages (id, session_id, role, content, content_type, tokens, sequence)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			domain.NewUUID(), newID, role, content, contentType, tokens, seq)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("copy message: %w", err)
		}
	}
	rows.Close()

	// Update the message count on the new session.
	_, err = tx.Exec(`UPDATE sessions SET message_count = ? WHERE id = ?`, count, newID)
	if err != nil {
		return nil, fmt.Errorf("update message_count: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return s.GetSession(newID)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// SessionTitle returns the title for a session, or "Unknown" if not found.
func (s *Store) SessionTitle(id string) string {
	var title string
	err := s.db.QueryRow(`SELECT title FROM sessions WHERE id = ?`, id).Scan(&title)
	if err != nil {
		return "Unknown"
	}
	return title
}

// FindSessionByPrefix matches a session by ID prefix (at least 4 chars).
func (s *Store) FindSessionByPrefix(prefix string) (*domain.Session, error) {
	row := s.db.QueryRow(
		`SELECT id, project_path, title, model, total_tokens, COALESCE(input_tokens,0), COALESCE(output_tokens,0), message_count, COALESCE(parent_session_id,''), COALESCE(branch_point,0), COALESCE(tags,''), created_at, updated_at
		 FROM sessions WHERE id LIKE ? || '%' ORDER BY updated_at DESC LIMIT 1`, prefix)
	return scanSession(row)
}

func scanSession(row *sql.Row) (*domain.Session, error) {
	var sess domain.Session
	var createdStr, updatedStr string
	err := row.Scan(&sess.ID, &sess.ProjectPath, &sess.Title, &sess.Model,
		&sess.TotalTokens, &sess.InputTokens, &sess.OutputTokens,
		&sess.MessageCount, &sess.ParentSessionID, &sess.BranchPoint,
		&sess.Tags, &createdStr, &updatedStr)
	if err != nil {
		return nil, err
	}
	if t, err := time.Parse("2006-01-02 15:04:05", createdStr); err == nil {
		sess.CreatedAt = t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", updatedStr); err == nil {
		sess.UpdatedAt = t
	}
	return &sess, nil
}
