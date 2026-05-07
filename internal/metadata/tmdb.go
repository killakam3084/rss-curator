package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const tmdbDefaultHost = "https://api.themoviedb.org"

// tmdbProvider implements MetadataProvider using The Movie Database API v3.
// It supports both TV shows (Fetch) and movies (FetchMovie).
//
// Configure via:
//
//	CURATOR_META_PROVIDER=tmdb
//	CURATOR_META_KEY=<API Read Access Token from themoviedb.org/settings/api>
//	CURATOR_META_HOST=<optional base URL override>
type tmdbProvider struct {
	host   string
	key    string
	client *http.Client
}

func newTMDbProvider(host, key string) *tmdbProvider {
	if host == "" {
		host = tmdbDefaultHost
	}
	return &tmdbProvider{
		host:   host,
		key:    key,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *tmdbProvider) Name() string { return "tmdb" }

// Fetch retrieves TV show metadata from TMDB (/3/search/tv → /3/tv/{id}).
// Returns (nil, nil) when the show is not found.
func (p *tmdbProvider) Fetch(ctx context.Context, showName string) (*ShowMetadata, error) {
	if p.key == "" {
		return nil, fmt.Errorf("tmdb: CURATOR_META_KEY is required")
	}

	searchURL := fmt.Sprintf("%s/3/search/tv?query=%s&language=en-US&page=1",
		p.host, url.QueryEscape(showName))

	var searchResp struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := p.get(ctx, searchURL, &searchResp); err != nil {
		return nil, fmt.Errorf("tmdb: search tv %q: %w", showName, err)
	}
	if len(searchResp.Results) == 0 {
		return nil, nil
	}
	return p.fetchTVDetail(ctx, searchResp.Results[0].ID)
}

// FetchMovie retrieves movie metadata from TMDB (/3/search/movie → /3/movie/{id}).
// Returns (nil, nil) when the movie is not found.
// This method is an optional capability beyond MetadataProvider; callers
// should type-assert to check support (e.g. via Lookup.ResolveMovie).
func (p *tmdbProvider) FetchMovie(ctx context.Context, movieName string) (*ShowMetadata, error) {
	if p.key == "" {
		return nil, fmt.Errorf("tmdb: CURATOR_META_KEY is required")
	}

	searchURL := fmt.Sprintf("%s/3/search/movie?query=%s&language=en-US&page=1",
		p.host, url.QueryEscape(movieName))

	var searchResp struct {
		Results []struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"results"`
	}
	if err := p.get(ctx, searchURL, &searchResp); err != nil {
		return nil, fmt.Errorf("tmdb: search movie %q: %w", movieName, err)
	}
	if len(searchResp.Results) == 0 {
		return nil, nil
	}
	return p.fetchMovieDetail(ctx, searchResp.Results[0].ID)
}

// tmdbGenre is the genre object shared by TV and movie detail responses.
type tmdbGenre struct {
	Name string `json:"name"`
}

func (p *tmdbProvider) fetchTVDetail(ctx context.Context, id int) (*ShowMetadata, error) {
	detailURL := fmt.Sprintf("%s/3/tv/%d?language=en-US&append_to_response=credits,external_ids", p.host, id)

	var detail struct {
		ID           int         `json:"id"`
		Name         string      `json:"name"`
		Overview     string      `json:"overview"`
		Status       string      `json:"status"`
		FirstAirDate string      `json:"first_air_date"` // "YYYY-MM-DD"
		VoteAverage  float64     `json:"vote_average"`
		VoteCount    int         `json:"vote_count"`
		Genres       []tmdbGenre `json:"genres"`
		Networks     []struct {
			Name string `json:"name"`
		} `json:"networks"`
		CreatedBy []struct {
			Name string `json:"name"`
		} `json:"created_by"`
		Credits *struct {
			Cast []struct {
				Name  string `json:"name"`
				Order int    `json:"order"`
			} `json:"cast"`
		} `json:"credits"`
		ExternalIDs *struct {
			IMDbID string `json:"imdb_id"`
		} `json:"external_ids"`
	}
	if err := p.get(ctx, detailURL, &detail); err != nil {
		return nil, fmt.Errorf("tmdb: tv detail %d: %w", id, err)
	}
	if detail.Name == "" {
		return nil, nil // treat 404 pass-through as a miss
	}

	meta := &ShowMetadata{
		ProviderID:  fmt.Sprintf("%d", detail.ID),
		ProviderURL: fmt.Sprintf("https://www.themoviedb.org/tv/%d", id),
		ShowName:    detail.Name,
		Status:      detail.Status,
		Overview:    detail.Overview,
		VoteAverage: detail.VoteAverage,
		VoteCount:   detail.VoteCount,
		FetchedAt:   time.Now().UTC(),
	}
	if detail.ExternalIDs != nil {
		meta.IMDbID = detail.ExternalIDs.IMDbID
	}
	for _, g := range detail.Genres {
		meta.Genres = append(meta.Genres, g.Name)
	}
	if len(detail.Networks) > 0 {
		meta.Network = detail.Networks[0].Name
	}
	meta.PremiereYear = parseYearFromDate(detail.FirstAirDate)
	for i, c := range detail.CreatedBy {
		if i >= 2 {
			break
		}
		meta.Creators = append(meta.Creators, c.Name)
	}
	if detail.Credits != nil {
		for i, c := range detail.Credits.Cast {
			if i >= 5 {
				break
			}
			meta.Cast = append(meta.Cast, c.Name)
		}
	}
	return meta, nil
}

func (p *tmdbProvider) fetchMovieDetail(ctx context.Context, id int) (*ShowMetadata, error) {
	detailURL := fmt.Sprintf("%s/3/movie/%d?language=en-US&append_to_response=credits", p.host, id)

	var detail struct {
		ID                  int         `json:"id"`
		Title               string      `json:"title"`
		Overview            string      `json:"overview"`
		Status              string      `json:"status"`
		ReleaseDate         string      `json:"release_date"` // "YYYY-MM-DD"
		IMDbID              string      `json:"imdb_id"`      // native field on movie detail
		VoteAverage         float64     `json:"vote_average"`
		VoteCount           int         `json:"vote_count"`
		Genres              []tmdbGenre `json:"genres"`
		ProductionCompanies []struct {
			Name string `json:"name"`
		} `json:"production_companies"`
		Credits *struct {
			Cast []struct {
				Name  string `json:"name"`
				Order int    `json:"order"`
			} `json:"cast"`
			Crew []struct {
				Name string `json:"name"`
				Job  string `json:"job"`
			} `json:"crew"`
		} `json:"credits"`
	}
	if err := p.get(ctx, detailURL, &detail); err != nil {
		return nil, fmt.Errorf("tmdb: movie detail %d: %w", id, err)
	}
	if detail.Title == "" {
		return nil, nil // treat 404 pass-through as a miss
	}

	meta := &ShowMetadata{
		ProviderID:  fmt.Sprintf("%d", detail.ID),
		ProviderURL: fmt.Sprintf("https://www.themoviedb.org/movie/%d", id),
		ShowName:    detail.Title,
		Status:      detail.Status,
		Overview:    detail.Overview,
		IMDbID:      detail.IMDbID,
		VoteAverage: detail.VoteAverage,
		VoteCount:   detail.VoteCount,
		FetchedAt:   time.Now().UTC(),
	}
	for _, g := range detail.Genres {
		meta.Genres = append(meta.Genres, g.Name)
	}
	// Use first production company as the "network" equivalent for movies.
	if len(detail.ProductionCompanies) > 0 {
		meta.Network = detail.ProductionCompanies[0].Name
	}
	meta.PremiereYear = parseYearFromDate(detail.ReleaseDate)
	if detail.Credits != nil {
		// Directors become "creators" for movies.
		for _, c := range detail.Credits.Crew {
			if c.Job == "Director" && c.Name != "" {
				meta.Creators = append(meta.Creators, c.Name)
				if len(meta.Creators) >= 2 {
					break
				}
			}
		}
		for i, c := range detail.Credits.Cast {
			if i >= 5 {
				break
			}
			meta.Cast = append(meta.Cast, c.Name)
		}
	}
	return meta, nil
}

// get performs a GET request with TMDB Bearer auth and JSON-decodes the
// response body into v. A 404 response returns nil error with v unchanged —
// callers must treat an empty/zero v as a cache miss.
func (p *tmdbProvider) get(ctx context.Context, endpoint string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "rss-curator/metadata")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(resp.Body).Decode(v)
	case http.StatusNotFound:
		return nil // caller treats empty result as a miss
	case http.StatusUnauthorized:
		return fmt.Errorf("tmdb: 401 Unauthorized — check CURATOR_META_KEY")
	default:
		return fmt.Errorf("tmdb: unexpected status %d", resp.StatusCode)
	}
}

// parseYearFromDate extracts the 4-digit year from a "YYYY-MM-DD" date string.
// Returns 0 if the string is too short or contains non-digit characters.
func parseYearFromDate(date string) int {
	if len(date) < 4 {
		return 0
	}
	year := 0
	for _, ch := range date[:4] {
		if ch < '0' || ch > '9' {
			return 0
		}
		year = year*10 + int(ch-'0')
	}
	return year
}
