package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Plex library section item types, per the Plex Media Server HTTP API.
const (
	plexTypeMovie   = 1
	plexTypeShow    = 2
	plexTypeEpisode = 4
)

// plexPageSize is the number of items requested per paged library call.
const plexPageSize = 500

// PlexConfig configures a read-only Plex Media Server client.
type PlexConfig struct {
	BaseURL string // e.g. "http://192.168.1.10:32400"
	Token   string // X-Plex-Token
	Timeout time.Duration
}

// Plex is a read-only client for a local Plex Media Server. It never mutates
// library state — it only reads sections and their items so curator can
// reconcile staged torrents against what is already owned.
type Plex struct {
	baseURL string
	token   string
	client  *http.Client
}

// PlexIdentity is the response of GET /identity, used as a connection test.
type PlexIdentity struct {
	MachineIdentifier string `json:"machineIdentifier"`
	Version           string `json:"version"`
}

// PlexSection is a library section ("Movies", "TV Shows", …).
type PlexSection struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Type  string `json:"type"` // "movie" | "show" | "artist" | "photo"
}

// PlexItem is a single library entry: a show, a movie, or an episode.
type PlexItem struct {
	RatingKey            string
	GrandparentRatingKey string // episodes only — the owning show
	Title                string
	Year                 int
	Season               int
	Episode              int
	IMDbID               string
	TMDBID               string
	TVDBID               string
	Resolution           string
	Codec                string
	HDR                  string
	FileSize             int64
	FilePath             string
}

// NewPlex builds a Plex client. The base URL must be absolute with an http or
// https scheme; the token must be non-empty.
func NewPlex(cfg PlexConfig) (*Plex, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, fmt.Errorf("plex: base URL is required")
	}
	// url.Parse accepts a bare "host:port" as a scheme-relative path, so the
	// scheme is checked up front to keep the settings error legible.
	if !strings.Contains(base, "://") {
		return nil, fmt.Errorf("plex: base URL %q must include an http:// or https:// scheme", base)
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("plex: parse base URL %q: %w", base, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("plex: base URL must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("plex: base URL %q has no host", base)
	}
	base = strings.TrimRight(base, "/")
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("plex: token is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Plex{
		baseURL: base,
		token:   cfg.Token,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

// BaseURL returns the configured server address, for diagnostics.
func (p *Plex) BaseURL() string { return p.baseURL }

// Identity pings the server and returns its identity. Cheap enough to use as
// a connection test without touching the library.
func (p *Plex) Identity(ctx context.Context) (*PlexIdentity, error) {
	var resp struct {
		MediaContainer PlexIdentity `json:"MediaContainer"`
	}
	if err := p.get(ctx, "/identity", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.MediaContainer, nil
}

// Sections lists the server's library sections.
func (p *Plex) Sections(ctx context.Context) ([]PlexSection, error) {
	var resp struct {
		MediaContainer struct {
			Directory []PlexSection `json:"Directory"`
		} `json:"MediaContainer"`
	}
	if err := p.get(ctx, "/library/sections", nil, &resp); err != nil {
		return nil, err
	}
	return resp.MediaContainer.Directory, nil
}

// Shows streams every show in a section. Show-level GUIDs are the reliable
// join key for episodes, which is why shows are synced separately.
func (p *Plex) Shows(ctx context.Context, sectionKey string, onPage func([]PlexItem) error) error {
	return p.listSection(ctx, sectionKey, plexTypeShow, onPage)
}

// Episodes streams every episode in a section as a flat list — one paged call
// for the whole section rather than a request per show. Episode GUIDs are
// unreliable, so callers link each episode to its show via GrandparentRatingKey.
func (p *Plex) Episodes(ctx context.Context, sectionKey string, onPage func([]PlexItem) error) error {
	return p.listSection(ctx, sectionKey, plexTypeEpisode, onPage)
}

// Movies streams every movie in a section.
func (p *Plex) Movies(ctx context.Context, sectionKey string, onPage func([]PlexItem) error) error {
	return p.listSection(ctx, sectionKey, plexTypeMovie, onPage)
}

// plexMetadata is the subset of a MediaContainer entry that curator consumes.
type plexMetadata struct {
	RatingKey            string `json:"ratingKey"`
	GrandparentRatingKey string `json:"grandparentRatingKey"`
	Title                string `json:"title"`
	Year                 int    `json:"year"`
	ParentIndex          int    `json:"parentIndex"` // season number
	Index                int    `json:"index"`       // episode number
	Guid                 string `json:"guid"`
	Guids                []struct {
		ID string `json:"id"`
	} `json:"Guid"`
	Media []struct {
		VideoResolution string `json:"videoResolution"`
		VideoCodec      string `json:"videoCodec"`
		VideoProfile    string `json:"videoProfile"`
		Part            []struct {
			Size int64  `json:"size"`
			File string `json:"file"`
		} `json:"Part"`
	} `json:"Media"`
}

func (p *Plex) listSection(ctx context.Context, sectionKey string, itemType int, onPage func([]PlexItem) error) error {
	endpoint := fmt.Sprintf("/library/sections/%s/all", url.PathEscape(sectionKey))

	for start := 0; ; start += plexPageSize {
		headers := map[string]string{
			"X-Plex-Container-Start": strconv.Itoa(start),
			"X-Plex-Container-Size":  strconv.Itoa(plexPageSize),
		}
		query := fmt.Sprintf("?type=%d&includeGuids=1", itemType)

		var resp struct {
			MediaContainer struct {
				Size     int            `json:"size"`
				Metadata []plexMetadata `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		if err := p.get(ctx, endpoint+query, headers, &resp); err != nil {
			return fmt.Errorf("plex: list section %s type=%d offset=%d: %w", sectionKey, itemType, start, err)
		}

		page := resp.MediaContainer.Metadata
		if len(page) > 0 {
			items := make([]PlexItem, 0, len(page))
			for _, m := range page {
				items = append(items, m.toItem())
			}
			if err := onPage(items); err != nil {
				return err
			}
		}
		if len(page) < plexPageSize {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (m plexMetadata) toItem() PlexItem {
	item := PlexItem{
		RatingKey:            m.RatingKey,
		GrandparentRatingKey: m.GrandparentRatingKey,
		Title:                m.Title,
		Year:                 m.Year,
		Season:               m.ParentIndex,
		Episode:              m.Index,
	}

	// The legacy agents put the external id in the scalar guid field; the
	// modern agent puts an opaque plex:// URI there and the real ids in Guid[].
	assignPlexGUID(&item, m.Guid)
	for _, g := range m.Guids {
		assignPlexGUID(&item, g.ID)
	}

	if len(m.Media) > 0 {
		media := m.Media[0]
		item.Resolution = media.VideoResolution
		item.Codec = media.VideoCodec
		item.HDR = normalizePlexVideoProfile(media.VideoProfile)
		if len(media.Part) > 0 {
			item.FileSize = media.Part[0].Size
			item.FilePath = media.Part[0].File
		}
	}
	return item
}

// assignPlexGUID parses one GUID string and records it on the item. It handles
// both the modern scheme ("imdb://tt0944947", "tvdb://121361") and the legacy
// agent scheme ("com.plexapp.agents.thetvdb://121361/1/1?lang=en"). Existing
// values are never overwritten, so the first source wins.
func assignPlexGUID(item *PlexItem, guid string) {
	if guid == "" {
		return
	}
	scheme, rest, ok := strings.Cut(guid, "://")
	if !ok {
		return
	}
	// Legacy agent ids carry extra path/query segments after the id.
	rest, _, _ = strings.Cut(rest, "?")
	rest, _, _ = strings.Cut(rest, "/")
	if rest == "" {
		return
	}

	switch strings.ToLower(scheme) {
	case "imdb", "com.plexapp.agents.imdb":
		if item.IMDbID == "" {
			item.IMDbID = rest
		}
	case "tmdb", "themoviedb", "com.plexapp.agents.themoviedb":
		if item.TMDBID == "" {
			item.TMDBID = rest
		}
	case "tvdb", "thetvdb", "com.plexapp.agents.thetvdb":
		if item.TVDBID == "" {
			item.TVDBID = rest
		}
	}
}

// normalizePlexVideoProfile maps Plex's videoProfile to curator's canonical
// HDR tags. Plex does not expose a dedicated HDR field, so anything it does
// not clearly signal is reported as empty rather than guessed.
func normalizePlexVideoProfile(profile string) string {
	switch p := strings.ToLower(strings.TrimSpace(profile)); {
	case p == "":
		return ""
	case strings.Contains(p, "dvhe"), strings.Contains(p, "dvav"), strings.Contains(p, "dolby vision"):
		return "dv"
	case strings.Contains(p, "main 10"), strings.Contains(p, "main10"):
		return "hdr10"
	case strings.Contains(p, "hlg"):
		return "hlg"
	default:
		return ""
	}
}

func (p *Plex) get(ctx context.Context, endpoint string, headers map[string]string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+endpoint, nil)
	if err != nil {
		return fmt.Errorf("plex: build request: %w", err)
	}
	// Plex serves XML unless JSON is explicitly requested.
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", p.token)
	req.Header.Set("User-Agent", "rss-curator/plex")
	for k, val := range headers {
		req.Header.Set(k, val)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("plex: GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return fmt.Errorf("plex: decode %s: %w", endpoint, err)
		}
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("plex: %d from %s — check the X-Plex-Token", resp.StatusCode, endpoint)
	case http.StatusNotFound:
		return fmt.Errorf("plex: 404 from %s — section or endpoint does not exist", endpoint)
	default:
		return fmt.Errorf("plex: unexpected status %d from %s", resp.StatusCode, endpoint)
	}
}
