package metadata

import (
	"context"
	"fmt"
)

// tmdbProvider is a stub for The Movie Database API.
// Set CURATOR_META_PROVIDER=tmdb and CURATOR_META_KEY=<your_api_key> to use
// this provider once it is implemented.
type tmdbProvider struct {
	host string
	key  string
}

func newTMDbProvider(host, key string) *tmdbProvider {
	if host == "" {
		host = "https://api.themoviedb.org"
	}
	return &tmdbProvider{host: host, key: key}
}

func (p *tmdbProvider) Name() string { return "tmdb" }

func (p *tmdbProvider) Fetch(_ context.Context, showName string) (*ShowMetadata, error) {
	return nil, fmt.Errorf("tmdb: provider not yet implemented (show=%q)", showName)
}
