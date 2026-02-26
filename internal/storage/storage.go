package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/iillmaticc/rss-curator/pkg/models"
	_ "github.com/mattn/go-sqlite3"
)

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

// migrate creates the necessary tables
func (s *Storage) migrate() error {
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

	CREATE INDEX IF NOT EXISTS idx_status ON staged_torrents(status);
	CREATE INDEX IF NOT EXISTS idx_link ON staged_torrents(link);
	CREATE INDEX IF NOT EXISTS idx_activity_action_at ON activity_log(action_at DESC);
	CREATE INDEX IF NOT EXISTS idx_activity_action ON activity_log(action);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Add adds a new staged torrent (or ignores if Link already exists)
func (s *Storage) Add(torrent models.StagedTorrent) error {
	feedItemJSON, err := json.Marshal(torrent.FeedItem)
	if err != nil {
		return fmt.Errorf("failed to marshal feed item: %w", err)
	}

	torrent.StagedAt = time.Now()

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO staged_torrents (link, feed_item, match_reason, staged_at, status)
		VALUES (?, ?, ?, ?, ?)
	`, torrent.FeedItem.Link, feedItemJSON, torrent.MatchReason, torrent.StagedAt, torrent.Status)

	return err
}

// List returns torrents filtered by status
func (s *Storage) List(status string) ([]models.StagedTorrent, error) {
	var rows *sql.Rows
	var err error

	if status == "" {
		rows, err = s.db.Query(`
			SELECT id, link, feed_item, match_reason, staged_at, status, approved_at
			FROM staged_torrents
			ORDER BY staged_at DESC
		`)
	} else {
		rows, err = s.db.Query(`
			SELECT id, link, feed_item, match_reason, staged_at, status, approved_at
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

		err := rows.Scan(&t.ID, &link, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt)
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
		SELECT id, link, feed_item, match_reason, staged_at, status, approved_at
		FROM staged_torrents
		WHERE id = ?
	`, id).Scan(&t.ID, &link, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt)

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
		SELECT id, feed_item, match_reason, staged_at, status, approved_at
		FROM staged_torrents
		WHERE id = ?
	`, id).Scan(&t.ID, &feedItemJSON, &t.MatchReason, &t.StagedAt, &t.Status, &approvedAt)

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
