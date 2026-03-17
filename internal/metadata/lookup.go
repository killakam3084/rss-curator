package metadata

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultTTLHours = 168 // 7 days

// Lookup is the primary entry point for consumer code. It wraps a
// MetadataProvider with a Cache and implements a cache-first resolution
// strategy with TTL-based refresh.
//
// Resolve never returns an error — metadata is always additive and must never
// block the caller. Failures are silently swallowed and a nil is returned so
// callers can do a simple nil-guard.
type Lookup struct {
	provider MetadataProvider
	cache    *Cache
	ttl      time.Duration
}

// NewLookup creates a Lookup. Either provider or cache may be nil (the noop
// provider and a disabled cache are substituted respectively), making wiring
// in main.go safe even when metadata is turned off.
func NewLookup(provider MetadataProvider, cache *Cache) *Lookup {
	if provider == nil {
		provider = &noopProvider{}
	}

	ttlHours := defaultTTLHours
	if v := os.Getenv("CURATOR_META_TTL_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ttlHours = n
		}
	}

	return &Lookup{
		provider: provider,
		cache:    cache,
		ttl:      time.Duration(ttlHours) * time.Hour,
	}
}

// Resolve returns metadata for the given show name using the cache-first
// strategy:
//
//  1. Cache hit whose FetchedAt is within TTL → return cached record.
//  2. Cache miss or expired → fetch from provider.
//  3. Non-nil fetch result → store in cache, return result.
//  4. Any error at any step → return nil (never propagated).
//
// The showName is normalised to lowercase for cache key consistency.
func (l *Lookup) Resolve(ctx context.Context, showName string) *ShowMetadata {
	if showName == "" {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(showName))

	// 1. Cache lookup.
	if l.cache != nil {
		if cached, err := l.cache.Get(key); err == nil && cached != nil {
			age := time.Since(cached.FetchedAt)
			if age < l.ttl {
				return cached
			}
			// Stale — fall through to refresh.
		}
	}

	// 2. Fetch fresh data (with the caller's context for cancellation).
	meta, err := l.provider.Fetch(ctx, showName)
	if err != nil || meta == nil {
		return nil
	}
	meta.FetchedAt = time.Now().UTC()

	// 3. Persist in cache (best-effort).
	if l.cache != nil {
		_ = l.cache.Put(key, l.provider.Name(), meta)
	}

	return meta
}
