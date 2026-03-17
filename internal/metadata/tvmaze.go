package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const tvmazeDefaultHost = "https://api.tvmaze.com"

// tvmazeProvider fetches show metadata from the free TVMaze API.
// No API key is required. Rate limit: ~20 req/10s per IP (effectively unlimited
// for curator's single-instance workload with a TTL cache in front).
type tvmazeProvider struct {
	host   string
	client *http.Client
}

func newTVMazeProvider(host string) *tvmazeProvider {
	if host == "" {
		host = tvmazeDefaultHost
	}
	return &tvmazeProvider{
		host:   host,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *tvmazeProvider) Name() string { return "tvmaze" }

// tvmazeShow is the relevant subset of the TVMaze singlesearch JSON response.
type tvmazeShow struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Genres  []string `json:"genres"`
	Network *struct {
		Name string `json:"name"`
	} `json:"network"`
	WebChannel *struct {
		Name string `json:"name"`
	} `json:"webChannel"`
	Premiered string `json:"premiered"` // "YYYY-MM-DD" or ""
	Summary   string `json:"summary"`   // may contain HTML tags
}

func (p *tvmazeProvider) Fetch(ctx context.Context, showName string) (*ShowMetadata, error) {
	endpoint := fmt.Sprintf("%s/singlesearch/shows?q=%s", p.host, url.QueryEscape(showName))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("tvmaze: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "rss-curator/metadata")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tvmaze: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Show not found — not an error, just a miss.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tvmaze: unexpected status %d", resp.StatusCode)
	}

	var show tvmazeShow
	if err := json.NewDecoder(resp.Body).Decode(&show); err != nil {
		return nil, fmt.Errorf("tvmaze: decode: %w", err)
	}

	meta := &ShowMetadata{
		ProviderID: fmt.Sprintf("%d", show.ID),
		ShowName:   show.Name,
		Genres:     show.Genres,
		Status:     show.Status,
		Overview:   stripHTML(show.Summary),
		FetchedAt:  time.Now().UTC(),
	}

	// Prefer Network name; fall back to WebChannel for streaming-only shows.
	if show.Network != nil {
		meta.Network = show.Network.Name
	} else if show.WebChannel != nil {
		meta.Network = show.WebChannel.Name
	}

	// Parse premiere year from "YYYY-MM-DD".
	if len(show.Premiered) >= 4 {
		year := 0
		for _, ch := range show.Premiered[:4] {
			if ch < '0' || ch > '9' {
				year = 0
				break
			}
			year = year*10 + int(ch-'0')
		}
		meta.PremiereYear = year
	}

	return meta, nil
}

// stripHTML removes a limited set of HTML tags from TVMaze summary strings.
// TVMaze summaries typically contain only <p>, <b>, <i>, <em>, <strong>.
// We do a simple tag-strip rather than pulling in an HTML parser.
func stripHTML(s string) string {
	out := make([]byte, 0, len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '<':
			inTag = true
		case s[i] == '>':
			inTag = false
		case !inTag:
			out = append(out, s[i])
		}
	}
	return string(out)
}
