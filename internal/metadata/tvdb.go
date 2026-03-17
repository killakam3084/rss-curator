package metadata

import (
	"context"
	"fmt"
)

// tvdbProvider is a stub for The TVDB API (v4).
// Set CURATOR_META_PROVIDER=tvdb and CURATOR_META_KEY=<your_api_key> to use
// this provider once it is implemented.
type tvdbProvider struct {
	host string
	key  string
}

func newTVDbProvider(host, key string) *tvdbProvider {
	if host == "" {
		host = "https://api4.thetvdb.com"
	}
	return &tvdbProvider{host: host, key: key}
}

func (p *tvdbProvider) Name() string { return "tvdb" }

func (p *tvdbProvider) Fetch(_ context.Context, showName string) (*ShowMetadata, error) {
	return nil, fmt.Errorf("tvdb: provider not yet implemented (show=%q)", showName)
}
