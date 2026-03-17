package metadata

import "context"

// noopProvider satisfies MetadataProvider but always returns a nil result.
// Used when CURATOR_META_PROVIDER=disabled or as a safe fallback.
type noopProvider struct{}

func (p *noopProvider) Name() string { return "disabled" }

func (p *noopProvider) Fetch(_ context.Context, _ string) (*ShowMetadata, error) {
	return nil, nil
}
