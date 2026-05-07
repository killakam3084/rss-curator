package metadata

import "time"

// ShowMetadata holds enrichment data fetched from a 3rd-party TV database.
// It is always additive — no existing workflow logic depends on it.
type ShowMetadata struct {
	ProviderID   string    `json:"provider_id"`
	ProviderURL  string    `json:"provider_url,omitempty"`
	ShowName     string    `json:"show_name"`
	Genres       []string  `json:"genres,omitempty"`
	Network      string    `json:"network,omitempty"`
	Status       string    `json:"status,omitempty"`
	PremiereYear int       `json:"premiere_year,omitempty"`
	Overview     string    `json:"overview,omitempty"`
	Cast         []string  `json:"cast,omitempty"`         // top-billed actor names (up to 5)
	Creators     []string  `json:"creators,omitempty"`     // show creator names (up to 2)
	VoteAverage  float64   `json:"vote_average,omitempty"` // provider community score (0–10)
	VoteCount    int       `json:"vote_count,omitempty"`   // number of community votes
	IMDbID       string    `json:"imdb_id,omitempty"`      // e.g. "tt1234567" for deep-linking
	FetchedAt    time.Time `json:"fetched_at"`
}
