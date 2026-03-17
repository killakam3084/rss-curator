package metadata

import "time"

// ShowMetadata holds enrichment data fetched from a 3rd-party TV database.
// It is always additive — no existing workflow logic depends on it.
type ShowMetadata struct {
	ProviderID   string    `json:"provider_id"`
	ShowName     string    `json:"show_name"`
	Genres       []string  `json:"genres,omitempty"`
	Network      string    `json:"network,omitempty"`
	Status       string    `json:"status,omitempty"`
	PremiereYear int       `json:"premiere_year,omitempty"`
	Overview     string    `json:"overview,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
}
