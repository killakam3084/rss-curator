package metadata

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Cache is a SQLite-backed TTL store for ShowMetadata. It lives as a sibling
// file to the main curator database so it ends up in the same directory in
// every environment (local home dir, container volume, TrueNAS dataset, …).
type Cache struct {
	db *sql.DB
}

// NewCache opens (or creates) the metadata cache database.
//
// Path resolution order:
//  1. CURATOR_META_DB env var (explicit override for unusual layouts).
//  2. curator-meta.db placed in the same directory as storagePath (the main
//     curator.db path).  This keeps both databases co-located automatically.
//
// storagePath is the fully-resolved path that was passed to storage.New so it
// always reflects the container mount point / STORAGE_PATH value.
func NewCache(storagePath string) (*Cache, error) {
	path := os.Getenv("CURATOR_META_DB")
	if path == "" {
		// Derive a sibling file in the same directory as the main DB.
		dir := filepath.Dir(storagePath)
		path = filepath.Join(dir, "curator-meta.db")
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("metadata cache: open %s: %w", path, err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("metadata cache: migrate: %w", err)
	}

	return &Cache{db: db}, nil
}

// migrate creates the show_metadata table if it does not exist.
func migrate(db *sql.DB) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS show_metadata (
	show_key   TEXT NOT NULL PRIMARY KEY,
	provider   TEXT NOT NULL DEFAULT '',
	data       TEXT NOT NULL DEFAULT '{}',
	fetched_at INTEGER NOT NULL DEFAULT 0
);`
	_, err := db.Exec(ddl)
	return err
}

// Get returns cached metadata for showKey, or (nil, nil) on a cache miss.
func (c *Cache) Get(showKey string) (*ShowMetadata, error) {
	row := c.db.QueryRow(
		`SELECT data, fetched_at FROM show_metadata WHERE show_key = ?`, showKey,
	)

	var dataJSON string
	var fetchedUnix int64
	if err := row.Scan(&dataJSON, &fetchedUnix); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("metadata cache: get %q: %w", showKey, err)
	}

	var meta ShowMetadata
	if err := json.Unmarshal([]byte(dataJSON), &meta); err != nil {
		// Corrupt entry — treat as a miss so it gets refreshed.
		return nil, nil
	}
	meta.FetchedAt = time.Unix(fetchedUnix, 0).UTC()
	return &meta, nil
}

// Put stores or updates metadata for showKey.
func (c *Cache) Put(showKey, provider string, meta *ShowMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("metadata cache: marshal %q: %w", showKey, err)
	}

	_, err = c.db.Exec(
		`INSERT INTO show_metadata (show_key, provider, data, fetched_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(show_key) DO UPDATE SET
		   provider   = excluded.provider,
		   data       = excluded.data,
		   fetched_at = excluded.fetched_at`,
		showKey, provider, string(data), meta.FetchedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("metadata cache: put %q: %w", showKey, err)
	}
	return nil
}

// Close releases the database connection.
func (c *Cache) Close() error { return c.db.Close() }
