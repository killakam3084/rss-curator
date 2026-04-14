package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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

// SuggestionRow is one persistent suggestion record in the suggestions table.
// It mirrors the shape of suggester.Suggestion but lives in the storage layer
// to avoid an import cycle.
type SuggestionRow struct {
	ID          int             `json:"id"`
	ShowName    string          `json:"show_name"`
	ContentType string          `json:"content_type"`
	Reason      string          `json:"reason"`
	RuleJSON    json.RawMessage `json:"rule,omitempty"`
	MetaJSON    json.RawMessage `json:"meta,omitempty"`
	Status      string          `json:"status"` // active | dismissed
	GeneratedAt time.Time       `json:"generated_at"`
	DismissedAt *time.Time      `json:"dismissed_at,omitempty"`
}

// Store defines the interface for storage operations
type Store interface {
	Get(id int) (*models.StagedTorrent, error)
	List(status, query, contentType string) ([]models.StagedTorrent, error)
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
	CompleteJob(id int, summary any) error
	FailJob(id int, errMsg string) error
	CancelJob(id int, summary any) error
	ListJobs(limit int, statusFilter string) ([]models.JobRecord, error)
	GetJob(id int) (*models.JobRecord, error)
	// MarkStaleJobsFailed marks any job still in "running" status as "failed".
	// Call at startup to recover from an unclean shutdown.
	MarkStaleJobsFailed(reason string) (int64, error)
	// Settings
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	GetAllSettings() (map[string]string, error)
	// GetApprovalQualityProfile returns the most common quality and codec seen
	// in approved torrents (mode query). Returns empty strings when no approved
	// torrents exist yet.
	GetApprovalQualityProfile() (quality, codec string, err error)

	// Suggestion row operations — replaces the old single-blob cache.
	// Each suggestion is a persistent row; status controls visibility.
	//
	// UpsertSuggestions inserts new suggestions (deduplicating by show_name
	// COLLATE NOCASE) and ignores rows that already exist in any status.
	// Existing dismissed rows are intentionally preserved so refreshes cannot
	// un-dismiss a previously dismissed suggestion.
	UpsertSuggestions(suggestions []SuggestionRow) error
	// ListSuggestions returns all active (status='active') suggestions ordered
	// by generated_at DESC.
	ListSuggestions() ([]SuggestionRow, error)
	// DismissSuggestion transitions a suggestion to status='dismissed'.
	// No-op when show_name is not found.
	DismissSuggestion(showName string) error
	// PruneSuggestions removes active suggestions whose show_name matches any
	// entry in the provided watchlist set (case-insensitive). Called during
	// suggest_refresh to evict stale entries added before a show joined the
	// watchlist. Returns the number of rows deleted.
	PruneSuggestions(watchlistNames []string) (int64, error)
	// SuggestionCount returns the number of active suggestions.
	SuggestionCount() (int, error)
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
		// Migration 8: Suggestion cache — single-row table storing the most
		// recent background-generated suggestions as a JSON blob.
		`CREATE TABLE IF NOT EXISTS suggestion_cache (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			suggestions_json TEXT NOT NULL DEFAULT '[]',
			generated_at DATETIME NOT NULL
		)`,
		// Migration 9: content_type column for show/movie differentiation
		`ALTER TABLE staged_torrents ADD COLUMN content_type TEXT NOT NULL DEFAULT 'show'`,
		`CREATE INDEX IF NOT EXISTS idx_content_type ON staged_torrents(content_type)`,
		// Migration 10: backfill content_type for rows staged on old code where the
		// column defaulted to 'show' even for movies. Rows whose match_reason begins
		// with 'matches movie:' are authoritative movie matches.
		`UPDATE staged_torrents SET content_type = 'movie' WHERE match_reason LIKE 'matches movie:%' AND content_type != 'movie'`,
		// Migration 11: persistent suggestions table — replaces single-blob
		// suggestion_cache. Each suggestion is a row with an explicit status so
		// dismissed entries survive across refreshes and watchlist dedup is done
		// at INSERT time via UNIQUE(show_name COLLATE NOCASE).
		`CREATE TABLE IF NOT EXISTS suggestions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			show_name    TEXT NOT NULL,
			content_type TEXT NOT NULL DEFAULT 'show',
			reason       TEXT NOT NULL DEFAULT '',
			rule_json    TEXT NOT NULL DEFAULT '{}',
			meta_json    TEXT NOT NULL DEFAULT '{}',
			status       TEXT NOT NULL DEFAULT 'active',
			generated_at DATETIME NOT NULL,
			dismissed_at DATETIME,
			UNIQUE(show_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_suggestions_status ON suggestions(status)`,
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

	contentType := string(torrent.FeedItem.ContentType)
	if contentType == "" {
		contentType = "show"
	}

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO staged_torrents (link, feed_item, match_reason, staged_at, status, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason, content_type)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, torrent.FeedItem.Link, feedItemJSON, torrent.MatchReason, torrent.StagedAt, torrent.Status, torrent.AIScore, torrent.AIReason, torrent.AIScored, torrent.MatchConfidence, torrent.MatchConfidenceReason, contentType)

	return err
}

// List returns torrents optionally filtered by status, title substring, and/or content type.
// Empty strings match all values for their respective filters.
func (s *Storage) List(status, query, contentType string) ([]models.StagedTorrent, error) {
	var rows *sql.Rows
	var err error

	// Build dynamic WHERE clause
	conds := []string{}
	args := []interface{}{}
	if status != "" {
		conds = append(conds, "status = ?")
		args = append(args, status)
	}
	if query != "" {
		conds = append(conds, "json_extract(feed_item, '$.title') LIKE ?")
		args = append(args, "%"+query+"%")
	}
	if contentType != "" {
		conds = append(conds, "content_type = ?")
		args = append(args, contentType)
	}

	sqlStr := `SELECT id, link, feed_item, match_reason, staged_at, status, approved_at, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason, content_type
		FROM staged_torrents`
	if len(conds) > 0 {
		sqlStr += " WHERE "
		for i, c := range conds {
			if i > 0 {
				sqlStr += " AND "
			}
			sqlStr += c
		}
	}
	sqlStr += " ORDER BY staged_at DESC"

	rows, err = s.db.Query(sqlStr, args...)

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
		var contentTypeDB string

		err := rows.Scan(&t.ID, &link, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt, &t.AIScore, &t.AIReason, &t.AIScored, &t.MatchConfidence, &t.MatchConfidenceReason, &contentTypeDB)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(feedItemJSON), &t.FeedItem); err != nil {
			return nil, fmt.Errorf("failed to unmarshal feed item: %w", err)
		}
		// DB column is authoritative — overwrite whatever the JSON blob says.
		if contentTypeDB != "" {
			t.FeedItem.ContentType = models.ContentType(contentTypeDB)
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

	var contentTypeDB string
	err := s.db.QueryRow(`
		SELECT id, link, feed_item, match_reason, staged_at, status, approved_at, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason, content_type
		FROM staged_torrents
		WHERE id = ?
	`, id).Scan(&t.ID, &link, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt, &t.AIScore, &t.AIReason, &t.AIScored, &t.MatchConfidence, &t.MatchConfidenceReason, &contentTypeDB)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("torrent not found")
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(feedItemJSON), &t.FeedItem); err != nil {
		return nil, fmt.Errorf("failed to unmarshal feed item: %w", err)
	}
	if contentTypeDB != "" {
		t.FeedItem.ContentType = models.ContentType(contentTypeDB)
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

	var contentTypeDB string
	err := s.db.QueryRow(`
		SELECT id, feed_item, match_reason, staged_at, status, approved_at, ai_score, ai_reason, ai_scored, match_confidence, match_confidence_reason, content_type
		FROM staged_torrents
		WHERE id = ?
	`, id).Scan(&t.ID, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt, &t.AIScore, &t.AIReason, &t.AIScored, &t.MatchConfidence, &t.MatchConfidenceReason, &contentTypeDB)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(feedItemJSON), &t.FeedItem); err != nil {
		return nil, fmt.Errorf("failed to unmarshal feed item: %w", err)
	}
	if contentTypeDB != "" {
		t.FeedItem.ContentType = models.ContentType(contentTypeDB)
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
func (s *Storage) CompleteJob(id int, summary any) error {
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

// MarkStaleJobsFailed marks any job still in "running" status as "failed".
// Call at startup before starting the scheduler and queue to recover from
// an unclean shutdown. Returns the number of jobs updated.
func (s *Storage) MarkStaleJobsFailed(reason string) (int64, error) {
	summary := models.JobSummary{ErrorMessage: reason}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`
		UPDATE jobs
		SET status = 'failed', completed_at = datetime('now'), summary_json = ?
		WHERE status = 'running'
	`, string(summaryJSON))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CancelJob marks a job as cancelled with partial summary statistics.
func (s *Storage) CancelJob(id int, summary any) error {
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

// UpsertSuggestions inserts new suggestion rows, ignoring conflicts on
// show_name so existing rows (including dismissed ones) are never overwritten.
func (s *Storage) UpsertSuggestions(suggestions []SuggestionRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("UpsertSuggestions: begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO suggestions
			(show_name, content_type, reason, rule_json, meta_json, status, generated_at)
		VALUES (?, ?, ?, ?, ?, 'active', ?)
	`)
	if err != nil {
		return fmt.Errorf("UpsertSuggestions: prepare: %w", err)
	}
	defer stmt.Close()

	for _, sg := range suggestions {
		ruleJSON := sg.RuleJSON
		if ruleJSON == nil {
			ruleJSON = json.RawMessage("{}")
		}
		metaJSON := sg.MetaJSON
		if metaJSON == nil {
			metaJSON = json.RawMessage("{}")
		}
		if _, err := stmt.Exec(sg.ShowName, sg.ContentType, sg.Reason, string(ruleJSON), string(metaJSON), sg.GeneratedAt); err != nil {
			return fmt.Errorf("UpsertSuggestions: insert %q: %w", sg.ShowName, err)
		}
	}
	return tx.Commit()
}

// ListSuggestions returns all active suggestions ordered by generated_at DESC.
func (s *Storage) ListSuggestions() ([]SuggestionRow, error) {
	rows, err := s.db.Query(`
		SELECT id, show_name, content_type, reason, rule_json, meta_json, status, generated_at, dismissed_at
		FROM suggestions
		WHERE status = 'active'
		ORDER BY generated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SuggestionRow
	for rows.Next() {
		var sg SuggestionRow
		var ruleJSON, metaJSON string
		var dismissedAt sql.NullTime
		if err := rows.Scan(&sg.ID, &sg.ShowName, &sg.ContentType, &sg.Reason, &ruleJSON, &metaJSON, &sg.Status, &sg.GeneratedAt, &dismissedAt); err != nil {
			return nil, err
		}
		sg.RuleJSON = json.RawMessage(ruleJSON)
		sg.MetaJSON = json.RawMessage(metaJSON)
		if dismissedAt.Valid {
			t := dismissedAt.Time
			sg.DismissedAt = &t
		}
		result = append(result, sg)
	}
	return result, rows.Err()
}

// DismissSuggestion marks a suggestion as dismissed by show name (case-insensitive).
func (s *Storage) DismissSuggestion(showName string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE suggestions SET status='dismissed', dismissed_at=? WHERE show_name=? COLLATE NOCASE`,
		now, showName,
	)
	return err
}

// PruneSuggestions removes active suggestions whose show_name appears in the
// provided watchlist set (case-insensitive). Returns the number of rows deleted.
func (s *Storage) PruneSuggestions(watchlistNames []string) (int64, error) {
	if len(watchlistNames) == 0 {
		return 0, nil
	}
	// Build placeholders: DELETE WHERE show_name IN (?, ?, ...)
	placeholders := strings.Repeat("?,", len(watchlistNames))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(watchlistNames))
	for i, n := range watchlistNames {
		args[i] = n
	}
	res, err := s.db.Exec(
		`DELETE FROM suggestions WHERE status='active' AND lower(show_name) IN (`+strings.Repeat("?,", len(watchlistNames)-1)+`?)`,
		args...,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SuggestionCount returns the number of active suggestions.
func (s *Storage) SuggestionCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM suggestions WHERE status='active'`).Scan(&n)
	return n, err
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
