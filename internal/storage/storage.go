package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
	_ "github.com/mattn/go-sqlite3"
)

// WindowStats holds counts for a rolling time window.
type WindowStats struct {
	Hours    int `json:"hours"`
	Seen     int `json:"seen"`     // raw feed items pulled within window
	Staged   int `json:"staged"`   // torrents staged within window
	Approved int `json:"approved"` // activity_log approve actions within window
	Rejected int `json:"rejected"` // activity_log reject actions within window
	Queued   int `json:"queued"`   // activity_log queue actions within window
	Pending  int `json:"pending"`  // current staged_torrents with status='pending'
}

// Store defines the interface for storage operations
type Store interface {
	Get(id int) (*models.StagedTorrent, error)
	List(status string) ([]models.StagedTorrent, error)
	Add(torrent models.StagedTorrent) error
	UpdateStatus(id int, status string) error
	LogActivity(torrentID int, title, action, matchReason string) error
	GetActivity(limit int, offset int, action string) ([]models.Activity, error)
	GetActivityCount(action string) (int, error)
	GetWindowStats(hours int) (*WindowStats, error)
	DeleteOld(olderThan time.Duration) error
	CleanupStaleLinks(patterns []string) (int64, error)
	Close() error
	GetByID(id int) (*models.StagedTorrent, error)
	AddRawFeedItem(item models.RawFeedItem) error
	GetRawFeedItems(limit int) ([]models.RawFeedItem, error)
	CleanupExpiredRawFeedItems() error
	UpdateAIScore(id int, score float64, reason string, confidence float64, confidenceReason string) error
	UpdateAfterRematch(id int, item models.FeedItem, matchReason, status string) error
	// Jobs
	CreateJob(jobType string) (int, error)
	CompleteJob(id int, summary models.JobSummary) error
	FailJob(id int, errMsg string) error
	CancelJob(id int, summary models.JobSummary) error
	ListJobs(limit int, statusFilter string) ([]models.JobRecord, error)
	GetJob(id int) (*models.JobRecord, error)
	// Settings
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	GetAllSettings() (map[string]string, error)
	// GetApprovalQualityProfile returns the most common quality and codec seen
	// in approved torrents (mode query). Returns empty strings when no approved
	// torrents exist yet.
	GetApprovalQualityProfile() (quality, codec string, err error)
}

// Storage handles persistent storage of staged torrents
type Storage struct {
	db *sql.DB
}

// New creates a new storage instance
func New(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	return s, nil
}

// migrate creates the necessary tables and applies schema upgrades
func (s *Storage) migrate() error {
	// Create initial schema
	schema := `
	CREATE TABLE IF NOT EXISTS staged_torrents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		link TEXT UNIQUE NOT NULL,
		feed_item TEXT NOT NULL,
		match_reason TEXT NOT NULL,
		staged_at DATETIME NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		approved_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS activity_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		torrent_id INTEGER NOT NULL,
		torrent_title TEXT NOT NULL,
		action TEXT NOT NULL,
		action_at DATETIME NOT NULL,
		match_reason TEXT NOT NULL,
		FOREIGN KEY (torrent_id) REFERENCES staged_torrents(id)
	);

	CREATE TABLE IF NOT EXISTS raw_feed_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		feed_item TEXT NOT NULL,
		pulled_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_status ON staged_torrents(status);
	CREATE INDEX IF NOT EXISTS idx_link ON staged_torrents(link);
	CREATE INDEX IF NOT EXISTS idx_activity_action_at ON activity_log(action_at DESC);
	CREATE INDEX IF NOT EXISTS idx_activity_action ON activity_log(action);
	CREATE INDEX IF NOT EXISTS idx_raw_feed_pulled_at ON raw_feed_items(pulled_at DESC);
	CREATE INDEX IF NOT EXISTS idx_raw_feed_expires_at ON raw_feed_items(expires_at);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Apply schema migrations (idempotent)
	migrations := []string{
		// Migration 1: Ensure raw_feed_items table exists (in case DB was created before this feature)
		`CREATE TABLE IF NOT EXISTS raw_feed_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			feed_item TEXT NOT NULL,
			pulled_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL
		)`,
		// Create indexes if they don't exist
		`CREATE INDEX IF NOT EXISTS idx_raw_feed_pulled_at ON raw_feed_items(pulled_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_raw_feed_expires_at ON raw_feed_items(expires_at)`,
		// Migration 2: Add AI score columns
		`ALTER TABLE staged_torrents ADD COLUMN ai_score REAL DEFAULT 0`,
		`ALTER TABLE staged_torrents ADD COLUMN ai_reason TEXT DEFAULT ''`,
		// Migration 3: Add ai_scored flag to distinguish "never scored" from "scored with low confidence"
		`ALTER TABLE staged_torrents ADD COLUMN ai_scored INTEGER DEFAULT 0`,
		// Migration 4: Add match_confidence signal (-1 = not assessed)
		`ALTER TABLE staged_torrents ADD COLUMN match_confidence REAL DEFAULT -1`,
		// Migration 5: Add match_confidence_reason
		`ALTER TABLE staged_torrents ADD COLUMN match_confidence_reason TEXT DEFAULT ''`,
		// Migration 6: Jobs table for background operation tracking
		`CREATE TABLE IF NOT EXISTS jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'running',
			started_at DATETIME NOT NULL,
			completed_at DATETIME,
			summary_json TEXT NOT NULL DEFAULT '{}'
		)`,
		// Migration 7: Settings table for runtime-configurable key/value pairs
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
	}

	for _, migration := range migrations {
		if _, err := s.db.Exec(migration); err != nil {
			// Log but don't fail - migration may have already been applied
			fmt.Printf("Migration note: %v\n", err)
		}
	}

	return nil
}

// Add adds a new staged torrent (or ignores if Link already exists)
func (s *Storage) Add(torrent models.StagedTorrent) error {
	feedItemJSON, err := json.Marshal(torrent.FeedItem)
	if err != nil {
		return fmt.Errorf("failed to marshal feed item: %w", err)
	}

	torrent.StagedAt = time.Now()

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO staged_torrents (link, feed_item, match_reason, staged_at, status, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, torrent.FeedItem.Link, feedItemJSON, torrent.MatchReason, torrent.StagedAt, torrent.Status, torrent.AIScore, torrent.AIReason, torrent.AIScored, torrent.MatchConfidence, torrent.MatchConfidenceReason)

	return err
}

// List returns torrents filtered by status
func (s *Storage) List(status string) ([]models.StagedTorrent, error) {
	var rows *sql.Rows
	var err error

	if status == "" {
		rows, err = s.db.Query(`
			SELECT id, link, feed_item, match_reason, staged_at, status, approved_at, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason
			FROM staged_torrents
			ORDER BY staged_at DESC
		`)
	} else {
		rows, err = s.db.Query(`
			SELECT id, link, feed_item, match_reason, staged_at, status, approved_at, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason
			FROM staged_torrents
			WHERE status = ?
			ORDER BY staged_at DESC
		`, status)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var torrents []models.StagedTorrent
	for rows.Next() {
		var t models.StagedTorrent
		var feedItemJSON string
		var link string
		var approvedAt sql.NullTime

		err := rows.Scan(&t.ID, &link, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt, &t.AIScore, &t.AIReason, &t.AIScored, &t.MatchConfidence, &t.MatchConfidenceReason)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(feedItemJSON), &t.FeedItem); err != nil {
			return nil, fmt.Errorf("failed to unmarshal feed item: %w", err)
		}

		if approvedAt.Valid {
			t.ApprovedAt = &approvedAt.Time
		}

		torrents = append(torrents, t)
	}

	return torrents, rows.Err()
}

// Get retrieves a torrent by ID
func (s *Storage) Get(id int) (*models.StagedTorrent, error) {
	var t models.StagedTorrent
	var feedItemJSON string
	var link string
	var approvedAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, link, feed_item, match_reason, staged_at, status, approved_at, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason
		FROM staged_torrents
		WHERE id = ?
	`, id).Scan(&t.ID, &link, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt, &t.AIScore, &t.AIReason, &t.AIScored, &t.MatchConfidence, &t.MatchConfidenceReason)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("torrent not found")
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(feedItemJSON), &t.FeedItem); err != nil {
		return nil, fmt.Errorf("failed to unmarshal feed item: %w", err)
	}

	if approvedAt.Valid {
		t.ApprovedAt = &approvedAt.Time
	}

	return &t, nil
}

// UpdateStatus updates the status of a torrent
func (s *Storage) UpdateStatus(id int, status string) error {
	var approvedAt *time.Time
	if status == "approved" {
		now := time.Now()
		approvedAt = &now
	}

	_, err := s.db.Exec(`
		UPDATE staged_torrents
		SET status = ?, approved_at = ?
		WHERE id = ?
	`, status, approvedAt, id)

	return err
}

// DeleteOld removes torrents older than the specified duration
func (s *Storage) DeleteOld(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	_, err := s.db.Exec(`
		DELETE FROM staged_torrents
		WHERE staged_at < ? AND status IN ('approved', 'rejected')
	`, cutoff)
	return err
}

// CleanupStaleLinks removes torrent entries with stale/invalid link formats
// This is useful when the RSS feed format changes (e.g., from info pages to authenticated URLs)
func (s *Storage) CleanupStaleLinks(patterns []string) (int64, error) {
	if len(patterns) == 0 {
		return 0, fmt.Errorf("no patterns specified for cleanup")
	}

	// Build query with OR conditions for each pattern
	query := "DELETE FROM staged_torrents WHERE status = 'pending' AND ("
	var args []interface{}
	for i, pattern := range patterns {
		if i > 0 {
			query += " OR "
		}
		query += "link LIKE ?"
		args = append(args, pattern)
	}
	query += ")"

	result, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Close closes the database connection
func (s *Storage) Close() error {
	return s.db.Close()
}

// GetByID retrieves a single torrent by ID
func (s *Storage) GetByID(id int) (*models.StagedTorrent, error) {
	var t models.StagedTorrent
	var feedItemJSON string
	var approvedAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, feed_item, match_reason, staged_at, status, approved_at, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason
		FROM staged_torrents
		WHERE id = ?
	`, id).Scan(&t.ID, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt, &t.AIScore, &t.AIReason, &t.AIScored, &t.MatchConfidence, &t.MatchConfidenceReason)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(feedItemJSON), &t.FeedItem); err != nil {
		return nil, fmt.Errorf("failed to unmarshal feed item: %w", err)
	}

	if approvedAt.Valid {
		t.ApprovedAt = &approvedAt.Time
	}

	return &t, nil
}

// LogActivity records an action taken on a torrent
func (s *Storage) LogActivity(torrentID int, title, action, matchReason string) error {
	_, err := s.db.Exec(`
		INSERT INTO activity_log (torrent_id, torrent_title, action, action_at, match_reason)
		VALUES (?, ?, ?, ?, ?)
	`, torrentID, title, action, time.Now(), matchReason)
	return err
}

// GetActivity retrieves activity log entries with optional filtering
func (s *Storage) GetActivity(limit int, offset int, action string) ([]models.Activity, error) {
	query := `
		SELECT id, torrent_id, torrent_title, action, action_at, match_reason
		FROM activity_log
	`
	args := []interface{}{}

	if action != "" {
		query += ` WHERE action = ?`
		args = append(args, action)
	}

	query += ` ORDER BY action_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []models.Activity
	for rows.Next() {
		var a models.Activity
		err := rows.Scan(&a.ID, &a.TorrentID, &a.TorrentTitle, &a.Action, &a.ActionAt, &a.MatchReason)
		if err != nil {
			return nil, err
		}
		activities = append(activities, a)
	}

	return activities, rows.Err()
}

// GetActivityCount returns total count of activities (with optional filter)
func (s *Storage) GetActivityCount(action string) (int, error) {
	query := `SELECT COUNT(*) FROM activity_log`
	args := []interface{}{}

	if action != "" {
		query += ` WHERE action = ?`
		args = append(args, action)
	}

	var count int
	err := s.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// AddRawFeedItem stores a raw feed item pulled from RSS (with TTL)
func (s *Storage) AddRawFeedItem(item models.RawFeedItem) error {
	feedItemJSON, err := json.Marshal(item.FeedItem)
	if err != nil {
		return fmt.Errorf("failed to marshal feed item: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO raw_feed_items (feed_item, pulled_at, expires_at)
		VALUES (?, ?, ?)
	`, feedItemJSON, item.PulledAt, item.ExpiresAt)

	return err
}

// GetRawFeedItems returns recent raw feed items (non-expired)
func (s *Storage) GetRawFeedItems(limit int) ([]models.RawFeedItem, error) {
	rows, err := s.db.Query(`
		SELECT id, feed_item, pulled_at, expires_at
		FROM raw_feed_items
		WHERE expires_at > datetime('now')
		ORDER BY pulled_at DESC
		LIMIT ?
	`, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.RawFeedItem
	for rows.Next() {
		var item models.RawFeedItem
		var feedItemJSON string

		err := rows.Scan(&item.ID, &feedItemJSON, &item.PulledAt, &item.ExpiresAt)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(feedItemJSON), &item.FeedItem); err != nil {
			return nil, fmt.Errorf("failed to unmarshal feed item: %w", err)
		}

		items = append(items, item)
	}

	return items, rows.Err()
}

// CleanupExpiredRawFeedItems removes expired raw feed items
func (s *Storage) CleanupExpiredRawFeedItems() error {
	_, err := s.db.Exec(`
		DELETE FROM raw_feed_items
		WHERE expires_at <= datetime('now')
	`)
	return err
}

// UpdateAIScore persists the AI-generated score, reason, and match confidence for a staged torrent.
func (s *Storage) UpdateAIScore(id int, score float64, reason string, confidence float64, confidenceReason string) error {
	_, err := s.db.Exec(`
		UPDATE staged_torrents
		SET ai_score = ?, ai_reason = ?, ai_scored = 1, match_confidence = ?, match_confidence_reason = ?
		WHERE id = ?
	`, score, reason, confidence, confidenceReason, id)
	return err
}

// UpdateAfterRematch persists the re-parsed feed item, refreshed match reason,
// and reconciled status for an existing staged torrent. It also clears AI score
// fields so stale prior scores are not shown when match context changed.
func (s *Storage) UpdateAfterRematch(id int, item models.FeedItem, matchReason, status string) error {
	feedItemJSON, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal feed item: %w", err)
	}

	_, err = s.db.Exec(`
		UPDATE staged_torrents
		SET feed_item = ?,
		    match_reason = ?,
		    status = ?,
		    approved_at = CASE WHEN ? = 'accepted' THEN approved_at ELSE NULL END,
		    ai_score = 0,
		    ai_reason = '',
		    ai_scored = 0,
		    match_confidence = -1,
		    match_confidence_reason = ''
		WHERE id = ?
	`, string(feedItemJSON), matchReason, status, status, id)
	return err
}

// CreateJob inserts a new job record with status "running" and returns its ID.
func (s *Storage) CreateJob(jobType string) (int, error) {
	res, err := s.db.Exec(`
		INSERT INTO jobs (type, status, started_at, summary_json)
		VALUES (?, 'running', ?, '{}')
	`, jobType, time.Now())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

// CompleteJob marks a job as completed with summary statistics.
func (s *Storage) CompleteJob(id int, summary models.JobSummary) error {
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		UPDATE jobs SET status = 'completed', completed_at = ?, summary_json = ? WHERE id = ?
	`, time.Now(), string(summaryJSON), id)
	return err
}

// FailJob marks a job as failed with an error message.
func (s *Storage) FailJob(id int, errMsg string) error {
	summary := models.JobSummary{ErrorMessage: errMsg}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		UPDATE jobs SET status = 'failed', completed_at = ?, summary_json = ? WHERE id = ?
	`, time.Now(), string(summaryJSON), id)
	return err
}

// CancelJob marks a job as cancelled with partial summary statistics.
func (s *Storage) CancelJob(id int, summary models.JobSummary) error {
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		UPDATE jobs SET status = 'cancelled', completed_at = ?, summary_json = ? WHERE id = ?
	`, time.Now(), string(summaryJSON), id)
	return err
}

// ListJobs returns the most recent job records, optionally filtered by status.
func (s *Storage) ListJobs(limit int, statusFilter string) ([]models.JobRecord, error) {
	query := `SELECT id, type, status, started_at, completed_at, summary_json FROM jobs`
	args := []any{}
	if statusFilter != "" {
		query += ` WHERE status = ?`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []models.JobRecord
	for rows.Next() {
		var j models.JobRecord
		var completedAt sql.NullTime
		var summaryJSON string
		if err := rows.Scan(&j.ID, &j.Type, &j.Status, &j.StartedAt, &completedAt, &summaryJSON); err != nil {
			return nil, err
		}
		if completedAt.Valid {
			j.CompletedAt = &completedAt.Time
		}
		if err := json.Unmarshal([]byte(summaryJSON), &j.Summary); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// GetJob retrieves a single job by ID. Returns nil, nil if not found.
func (s *Storage) GetJob(id int) (*models.JobRecord, error) {
	var j models.JobRecord
	var completedAt sql.NullTime
	var summaryJSON string
	err := s.db.QueryRow(`
		SELECT id, type, status, started_at, completed_at, summary_json FROM jobs WHERE id = ?
	`, id).Scan(&j.ID, &j.Type, &j.Status, &j.StartedAt, &completedAt, &summaryJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		j.CompletedAt = &completedAt.Time
	}
	if err := json.Unmarshal([]byte(summaryJSON), &j.Summary); err != nil {
		return nil, err
	}
	return &j, nil
}

// GetSetting retrieves a single runtime setting by key. Returns "", nil when the key does not exist.
func (s *Storage) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetSetting persists a single runtime setting by key, upserting the row.
func (s *Storage) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key, value, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now(),
	)
	return err
}

// GetAllSettings returns all stored runtime settings as a key/value map.
func (s *Storage) GetAllSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// GetApprovalQualityProfile returns the quality and codec that appear most
// frequently across all approved torrents. Empty strings are returned when the
// approved set is empty.
func (s *Storage) GetApprovalQualityProfile() (quality, codec string, err error) {
	var q, c sql.NullString
	row := s.db.QueryRow(`
		SELECT
			json_extract(feed_item, '$.quality') AS q,
			json_extract(feed_item, '$.codec')   AS c,
			COUNT(*) AS cnt
		FROM staged_torrents
		WHERE status = 'approved'
		GROUP BY 1, 2
		ORDER BY cnt DESC
		LIMIT 1
	`)
	if scanErr := row.Scan(&q, &c, new(int)); scanErr != nil {
		// sql.ErrNoRows means no approved torrents yet — not an error for callers.
		if scanErr == sql.ErrNoRows {
			return "", "", nil
		}
		return "", "", scanErr
	}
	if q.Valid {
		quality = q.String
	}
	if c.Valid {
		codec = c.String
	}
	return quality, codec, nil
}

// GetWindowStats returns activity counts for a rolling window of the given hours,
// plus the current pending queue depth (no time filter).
func (s *Storage) GetWindowStats(hours int) (*WindowStats, error) {
	ws := &WindowStats{Hours: hours}

	countQuery := func(query string, args ...any) (int, error) {
		var n int
		if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}

	var err error

	ws.Seen, err = countQuery(
		`SELECT COUNT(*) FROM raw_feed_items WHERE pulled_at >= datetime('now', ? || ' hours')`,
		fmt.Sprintf("-%d", hours),
	)
	if err != nil {
		return nil, fmt.Errorf("window stats seen: %w", err)
	}

	ws.Staged, err = countQuery(
		`SELECT COUNT(*) FROM staged_torrents WHERE staged_at >= datetime('now', ? || ' hours')`,
		fmt.Sprintf("-%d", hours),
	)
	if err != nil {
		return nil, fmt.Errorf("window stats staged: %w", err)
	}

	for _, pair := range []struct {
		dest   *int
		action string
	}{
		{&ws.Approved, "approve"},
		{&ws.Rejected, "reject"},
		{&ws.Queued, "queue"},
	} {
		*pair.dest, err = countQuery(
			`SELECT COUNT(*) FROM activity_log WHERE action = ? AND action_at >= datetime('now', ? || ' hours')`,
			pair.action,
			fmt.Sprintf("-%d", hours),
		)
		if err != nil {
			return nil, fmt.Errorf("window stats %s: %w", pair.action, err)
		}
	}

	ws.Pending, err = countQuery(`SELECT COUNT(*) FROM staged_torrents WHERE status = 'pending'`)
	if err != nil {
		return nil, fmt.Errorf("window stats pending: %w", err)
	}

	return ws, nil
}
