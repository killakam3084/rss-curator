package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Annotation kinds written by the Plex reconciliation pass.
const (
	AnnotationInLibrary      = "in_library"
	AnnotationLibraryUpgrade = "library_upgrade"
)

// AnnotationSourcePlex marks annotations owned by the Plex reconciler, so a
// re-run can clear its own rows without disturbing annotations from any other
// source added later.
const AnnotationSourcePlex = "plex"

// PlexShow is a cached Plex library show. Shows are stored separately from
// episodes because show-level GUIDs are the reliable join key — Plex's
// episode-level GUIDs are not dependable.
type PlexShow struct {
	RatingKey  string
	SectionKey string
	Title      string
	NormTitle  string
	Year       int
	IMDbID     string
	TMDBID     string
	TVDBID     string
	SyncedAt   time.Time
}

// PlexItem is a cached Plex library entry: a movie or an episode.
type PlexItem struct {
	RatingKey     string
	SectionKey    string
	ContentType   string // "movie" | "episode"
	ShowRatingKey string // episodes only
	Title         string
	NormTitle     string
	Year          int
	Season        int
	Episode       int
	IMDbID        string
	TMDBID        string
	TVDBID        string
	Resolution    string
	Codec         string
	HDR           []string
	FileSize      int64
	FilePath      string
	SyncedAt      time.Time
}

// PlexLibrary records the last sync state of one library section.
type PlexLibrary struct {
	SectionKey   string    `json:"section_key"`
	Title        string    `json:"title"`
	Type         string    `json:"type"`
	ItemCount    int       `json:"item_count"`
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// PlexCounts summarises what is currently cached.
type PlexCounts struct {
	Shows    int `json:"shows"`
	Episodes int `json:"episodes"`
	Movies   int `json:"movies"`
}

// Annotation is a non-destructive marker attached to a staged torrent. The
// shape is deliberately generic so later features can annotate torrents
// without another schema change.
type Annotation struct {
	TorrentID int             `json:"-"`
	Kind      string          `json:"kind"`
	Source    string          `json:"source"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// UpsertPlexShows inserts or refreshes cached shows in a single transaction.
func (s *Storage) UpsertPlexShows(shows []PlexShow) error {
	if len(shows) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("upsert plex shows: begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO plex_shows (rating_key, section_key, title, norm_title, year, imdb_id, tmdb_id, tvdb_id, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(rating_key) DO UPDATE SET
			section_key = excluded.section_key,
			title       = excluded.title,
			norm_title  = excluded.norm_title,
			year        = excluded.year,
			imdb_id     = excluded.imdb_id,
			tmdb_id     = excluded.tmdb_id,
			tvdb_id     = excluded.tvdb_id,
			synced_at   = excluded.synced_at`)
	if err != nil {
		return fmt.Errorf("upsert plex shows: prepare: %w", err)
	}
	defer stmt.Close()

	for _, sh := range shows {
		if _, err := stmt.Exec(sh.RatingKey, sh.SectionKey, sh.Title, sh.NormTitle, sh.Year,
			sh.IMDbID, sh.TMDBID, sh.TVDBID, sh.SyncedAt); err != nil {
			return fmt.Errorf("upsert plex show %s: %w", sh.RatingKey, err)
		}
	}
	return tx.Commit()
}

// UpsertPlexItems inserts or refreshes cached movies/episodes in a single transaction.
func (s *Storage) UpsertPlexItems(items []PlexItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("upsert plex items: begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO plex_items (
			rating_key, section_key, content_type, show_rating_key, title, norm_title,
			year, season, episode, imdb_id, tmdb_id, tvdb_id,
			resolution, codec, hdr, file_size, file_path, synced_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(rating_key) DO UPDATE SET
			section_key     = excluded.section_key,
			content_type    = excluded.content_type,
			show_rating_key = excluded.show_rating_key,
			title           = excluded.title,
			norm_title      = excluded.norm_title,
			year            = excluded.year,
			season          = excluded.season,
			episode         = excluded.episode,
			imdb_id         = excluded.imdb_id,
			tmdb_id         = excluded.tmdb_id,
			tvdb_id         = excluded.tvdb_id,
			resolution      = excluded.resolution,
			codec           = excluded.codec,
			hdr             = excluded.hdr,
			file_size       = excluded.file_size,
			file_path       = excluded.file_path,
			synced_at       = excluded.synced_at`)
	if err != nil {
		return fmt.Errorf("upsert plex items: prepare: %w", err)
	}
	defer stmt.Close()

	for _, it := range items {
		if _, err := stmt.Exec(it.RatingKey, it.SectionKey, it.ContentType, it.ShowRatingKey,
			it.Title, it.NormTitle, it.Year, it.Season, it.Episode,
			it.IMDbID, it.TMDBID, it.TVDBID,
			it.Resolution, it.Codec, joinHDR(it.HDR), it.FileSize, it.FilePath, it.SyncedAt); err != nil {
			return fmt.Errorf("upsert plex item %s: %w", it.RatingKey, err)
		}
	}
	return tx.Commit()
}

// UpdatePlexItemMedia refreshes the media fields of a single cached item.
// Used when reconcile tops up HDR from the item detail endpoint, which
// section listings do not reliably provide.
func (s *Storage) UpdatePlexItemMedia(ratingKey, resolution, codec string, hdr []string, fileSize int64) error {
	_, err := s.db.Exec(`
		UPDATE plex_items SET resolution = ?, codec = ?, hdr = ?, file_size = ?
		WHERE rating_key = ?`,
		resolution, codec, joinHDR(hdr), fileSize, ratingKey)
	if err != nil {
		return fmt.Errorf("update plex item media %s: %w", ratingKey, err)
	}
	return nil
}

// DeleteStalePlexRows removes cached rows for a section that were not touched
// by the most recent sync, so media removed from Plex disappears from the cache.
func (s *Storage) DeleteStalePlexRows(sectionKey string, before time.Time) (int64, error) {
	var total int64
	for _, table := range []string{"plex_items", "plex_shows"} {
		res, err := s.db.Exec(
			fmt.Sprintf(`DELETE FROM %s WHERE section_key = ? AND synced_at < ?`, table),
			sectionKey, before)
		if err != nil {
			return total, fmt.Errorf("delete stale %s: %w", table, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

// LookupPlexShowByIDs finds a cached show by any external provider id.
// Returns (nil, nil) when nothing matches or no ids were supplied.
func (s *Storage) LookupPlexShowByIDs(imdbID, tmdbID, tvdbID string) (*PlexShow, error) {
	conds, args := externalIDConds(imdbID, tmdbID, tvdbID)
	if len(conds) == 0 {
		return nil, nil
	}

	row := s.db.QueryRow(`
		SELECT rating_key, section_key, title, norm_title, year, imdb_id, tmdb_id, tvdb_id, synced_at
		FROM plex_shows WHERE `+strings.Join(conds, " OR ")+` LIMIT 1`, args...)

	var sh PlexShow
	err := row.Scan(&sh.RatingKey, &sh.SectionKey, &sh.Title, &sh.NormTitle, &sh.Year,
		&sh.IMDbID, &sh.TMDBID, &sh.TVDBID, &sh.SyncedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup plex show by ids: %w", err)
	}
	return &sh, nil
}

// LookupPlexShowByTitle finds a cached show by normalized title. This is the
// lower-confidence fallback used when no external id resolved.
func (s *Storage) LookupPlexShowByTitle(normTitle string) (*PlexShow, error) {
	if normTitle == "" {
		return nil, nil
	}
	row := s.db.QueryRow(`
		SELECT rating_key, section_key, title, norm_title, year, imdb_id, tmdb_id, tvdb_id, synced_at
		FROM plex_shows WHERE norm_title = ? LIMIT 1`, normTitle)

	var sh PlexShow
	err := row.Scan(&sh.RatingKey, &sh.SectionKey, &sh.Title, &sh.NormTitle, &sh.Year,
		&sh.IMDbID, &sh.TMDBID, &sh.TVDBID, &sh.SyncedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup plex show by title: %w", err)
	}
	return &sh, nil
}

// LookupPlexEpisode finds a cached episode within a known show.
func (s *Storage) LookupPlexEpisode(showRatingKey string, season, episode int) (*PlexItem, error) {
	if showRatingKey == "" {
		return nil, nil
	}
	row := s.db.QueryRow(plexItemSelect+`
		WHERE content_type = 'episode' AND show_rating_key = ? AND season = ? AND episode = ?
		LIMIT 1`, showRatingKey, season, episode)
	return scanPlexItem(row)
}

// LookupPlexMovieByIDs finds a cached movie by any external provider id.
func (s *Storage) LookupPlexMovieByIDs(imdbID, tmdbID, tvdbID string) (*PlexItem, error) {
	conds, args := externalIDConds(imdbID, tmdbID, tvdbID)
	if len(conds) == 0 {
		return nil, nil
	}
	row := s.db.QueryRow(plexItemSelect+`
		WHERE content_type = 'movie' AND (`+strings.Join(conds, " OR ")+`) LIMIT 1`, args...)
	return scanPlexItem(row)
}

// LookupPlexMovieByTitle finds a cached movie by normalized title, accepting a
// one-year drift because release year and Plex's year can legitimately differ.
func (s *Storage) LookupPlexMovieByTitle(normTitle string, year int) (*PlexItem, error) {
	if normTitle == "" {
		return nil, nil
	}
	if year <= 0 {
		row := s.db.QueryRow(plexItemSelect+`
			WHERE content_type = 'movie' AND norm_title = ? LIMIT 1`, normTitle)
		return scanPlexItem(row)
	}
	row := s.db.QueryRow(plexItemSelect+`
		WHERE content_type = 'movie' AND norm_title = ?
		  AND (year = 0 OR ABS(year - ?) <= 1)
		ORDER BY ABS(year - ?) LIMIT 1`, normTitle, year, year)
	return scanPlexItem(row)
}

// UpsertPlexLibrary records the sync state of one section.
func (s *Storage) UpsertPlexLibrary(lib PlexLibrary) error {
	_, err := s.db.Exec(`
		INSERT INTO plex_libraries (section_key, title, type, item_count, last_synced_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(section_key) DO UPDATE SET
			title          = excluded.title,
			type           = excluded.type,
			item_count     = excluded.item_count,
			last_synced_at = excluded.last_synced_at`,
		lib.SectionKey, lib.Title, lib.Type, lib.ItemCount, lib.LastSyncedAt)
	if err != nil {
		return fmt.Errorf("upsert plex library %s: %w", lib.SectionKey, err)
	}
	return nil
}

// ListPlexLibraries returns every section that has been synced at least once.
func (s *Storage) ListPlexLibraries() ([]PlexLibrary, error) {
	rows, err := s.db.Query(`
		SELECT section_key, title, type, item_count, last_synced_at
		FROM plex_libraries ORDER BY title`)
	if err != nil {
		return nil, fmt.Errorf("list plex libraries: %w", err)
	}
	defer rows.Close()

	libs := []PlexLibrary{}
	for rows.Next() {
		var l PlexLibrary
		if err := rows.Scan(&l.SectionKey, &l.Title, &l.Type, &l.ItemCount, &l.LastSyncedAt); err != nil {
			return nil, fmt.Errorf("scan plex library: %w", err)
		}
		libs = append(libs, l)
	}
	return libs, rows.Err()
}

// GetPlexCounts returns how many shows, episodes and movies are cached.
func (s *Storage) GetPlexCounts() (PlexCounts, error) {
	var c PlexCounts
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM plex_shows`).Scan(&c.Shows); err != nil {
		return c, fmt.Errorf("plex counts (shows): %w", err)
	}
	err := s.db.QueryRow(`
		SELECT
			COALESCE(SUM(content_type = 'episode'), 0),
			COALESCE(SUM(content_type = 'movie'), 0)
		FROM plex_items`).Scan(&c.Episodes, &c.Movies)
	if err != nil {
		return c, fmt.Errorf("plex counts (items): %w", err)
	}
	return c, nil
}

// UpsertAnnotations writes annotations, replacing any existing row with the
// same (torrent_id, kind).
func (s *Storage) UpsertAnnotations(annotations []Annotation) error {
	if len(annotations) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("upsert annotations: begin: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO torrent_annotations (torrent_id, kind, source, detail, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(torrent_id, kind) DO UPDATE SET
			source     = excluded.source,
			detail     = excluded.detail,
			updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("upsert annotations: prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, a := range annotations {
		detail := string(a.Detail)
		if detail == "" {
			detail = "{}"
		}
		if _, err := stmt.Exec(a.TorrentID, a.Kind, a.Source, detail, now, now); err != nil {
			return fmt.Errorf("upsert annotation torrent=%d kind=%s: %w", a.TorrentID, a.Kind, err)
		}
	}
	return tx.Commit()
}

// DeleteAnnotationsBySource clears a source's annotations for the given
// torrents. An empty torrentIDs slice clears that source for every torrent.
func (s *Storage) DeleteAnnotationsBySource(torrentIDs []int, source string) (int64, error) {
	if len(torrentIDs) == 0 {
		res, err := s.db.Exec(`DELETE FROM torrent_annotations WHERE source = ?`, source)
		if err != nil {
			return 0, fmt.Errorf("delete annotations by source: %w", err)
		}
		n, _ := res.RowsAffected()
		return n, nil
	}

	placeholders, args := intPlaceholders(torrentIDs)
	args = append(args, source)
	res, err := s.db.Exec(
		`DELETE FROM torrent_annotations WHERE torrent_id IN (`+placeholders+`) AND source = ?`, args...)
	if err != nil {
		return 0, fmt.Errorf("delete annotations by source: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// AnnotationsForTorrents batch-loads annotations for the given torrent ids.
func (s *Storage) AnnotationsForTorrents(torrentIDs []int) (map[int][]Annotation, error) {
	out := map[int][]Annotation{}
	if len(torrentIDs) == 0 {
		return out, nil
	}

	placeholders, args := intPlaceholders(torrentIDs)
	rows, err := s.db.Query(`
		SELECT torrent_id, kind, source, detail, updated_at
		FROM torrent_annotations WHERE torrent_id IN (`+placeholders+`)
		ORDER BY torrent_id, kind`, args...)
	if err != nil {
		return nil, fmt.Errorf("annotations for torrents: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var a Annotation
		var detail string
		if err := rows.Scan(&a.TorrentID, &a.Kind, &a.Source, &detail, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan annotation: %w", err)
		}
		a.Detail = json.RawMessage(detail)
		out[a.TorrentID] = append(out[a.TorrentID], a)
	}
	return out, rows.Err()
}

// CountAnnotationsByKind returns how many torrents carry each annotation kind.
func (s *Storage) CountAnnotationsByKind() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT kind, COUNT(*) FROM torrent_annotations GROUP BY kind`)
	if err != nil {
		return nil, fmt.Errorf("count annotations by kind: %w", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, fmt.Errorf("scan annotation count: %w", err)
		}
		out[kind] = n
	}
	return out, rows.Err()
}

const plexItemSelect = `
	SELECT rating_key, section_key, content_type, show_rating_key, title, norm_title,
	       year, season, episode, imdb_id, tmdb_id, tvdb_id,
	       resolution, codec, hdr, file_size, file_path, synced_at
	FROM plex_items`

func scanPlexItem(row *sql.Row) (*PlexItem, error) {
	var it PlexItem
	var hdr string
	err := row.Scan(&it.RatingKey, &it.SectionKey, &it.ContentType, &it.ShowRatingKey,
		&it.Title, &it.NormTitle, &it.Year, &it.Season, &it.Episode,
		&it.IMDbID, &it.TMDBID, &it.TVDBID,
		&it.Resolution, &it.Codec, &hdr, &it.FileSize, &it.FilePath, &it.SyncedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan plex item: %w", err)
	}
	it.HDR = splitHDR(hdr)
	return &it, nil
}

// externalIDConds builds an OR-set over whichever provider ids were supplied.
func externalIDConds(imdbID, tmdbID, tvdbID string) ([]string, []any) {
	var conds []string
	var args []any
	for _, pair := range []struct {
		column string
		value  string
	}{
		{"imdb_id", imdbID},
		{"tmdb_id", tmdbID},
		{"tvdb_id", tvdbID},
	} {
		if pair.value != "" {
			conds = append(conds, pair.column+" = ?")
			args = append(args, pair.value)
		}
	}
	return conds, args
}

func intPlaceholders(ids []int) (string, []any) {
	marks := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	for i, id := range ids {
		marks[i] = "?"
		args = append(args, id)
	}
	return strings.Join(marks, ","), args
}

func joinHDR(tags []string) string { return strings.Join(tags, ",") }

func splitHDR(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
