package metadata

import (
	"context"
	"os"
	"strings"
)

// MetadataProvider abstracts a 3rd-party TV database backend. Implementations
// must be safe for concurrent use.
type MetadataProvider interface {
	// Fetch retrieves metadata for the given canonical show name.
	// Returns (nil, nil) when the show is not found on the provider.
	// Returns a non-nil error only for transient / network failures.
	Fetch(ctx context.Context, showName string) (*ShowMetadata, error)

	// Name returns a short identifier for logging (e.g. "tvmaze", "tmdb").
	Name() string
}

// NewMetadataProvider constructs a MetadataProvider from environment variables.
//
//	CURATOR_META_PROVIDER   "tvmaze" (default) | "tmdb" | "tvdb" | "disabled"
//	 CURATOR_META_KEY        API key/token — required for tmdb and tvdb.
//	                          For tmdb: use the API Read Access Token
//	                          (Bearer auth) from themoviedb.org/settings/api.
//	CURATOR_META_HOST       override base URL (useful for self-hosted mirrors)
func NewMetadataProvider() MetadataProvider {
	providerType := strings.ToLower(os.Getenv("CURATOR_META_PROVIDER"))
	if providerType == "" {
		providerType = "tvmaze"
	}

	key := os.Getenv("CURATOR_META_KEY")
	host := os.Getenv("CURATOR_META_HOST")

	switch providerType {
	case "tmdb":
		return newTMDbProvider(host, key)
	case "tvdb":
		return newTVDbProvider(host, key)
	case "disabled":
		return &noopProvider{}
	default: // "tvmaze"
		return newTVMazeProvider(host)
	}
}
