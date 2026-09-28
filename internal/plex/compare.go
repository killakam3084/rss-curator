package plex

import "strings"

// Verdicts produced by Compare. They mirror the annotation kinds written by
// the reconciler.
const (
	VerdictInLibrary = "in_library"
	VerdictUpgrade   = "library_upgrade"
)

// Copy describes one encoding of a title, either a staged release candidate or
// the copy already present in the Plex library.
type Copy struct {
	Quality string
	Codec   string
	HDR     []string
}

// QualityRank mirrors the hierarchy used by internal/matcher so both sides of
// a comparison agree. Unrecognised tokens rank -1 and never win a comparison.
func QualityRank(quality string) int {
	switch strings.ToUpper(strings.TrimSpace(quality)) {
	case "480P":
		return 0
	case "720P":
		return 1
	case "1080P":
		return 2
	case "2160P", "4K":
		return 3
	default:
		return -1
	}
}

// hdrRank orders the canonical HDR tags produced by the feed parser and the
// Plex client. A higher rank is a strictly richer format.
func hdrRank(tags []string) int {
	best := 0
	for _, tag := range tags {
		var r int
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "dv":
			r = 4
		case "hdr10plus":
			r = 3
		case "hdr10":
			r = 2
		case "hdr", "hlg":
			r = 1
		}
		if r > best {
			best = r
		}
	}
	return best
}

// Compare decides whether a candidate release is merely a duplicate of what is
// already in the library or a genuine upgrade over it.
//
// A candidate only wins on evidence: an unrecognised quality on either side
// can never produce an upgrade verdict, because Phase 1 exists to surface
// duplicates for bulk removal and a false "upgrade" is the costlier mistake.
func Compare(candidate, library Copy) (verdict, reason string) {
	candQ := QualityRank(candidate.Quality)
	libQ := QualityRank(library.Quality)

	if candQ >= 0 && libQ >= 0 {
		switch {
		case candQ > libQ:
			return VerdictUpgrade, "higher quality than the library copy (" +
				upper(candidate.Quality) + " over " + upper(library.Quality) + ")"
		case candQ < libQ:
			return VerdictInLibrary, "library copy is higher quality (" +
				upper(library.Quality) + " over " + upper(candidate.Quality) + ")"
		}
	}

	// Equal (or unknown) quality — a richer HDR format is the remaining
	// justification for calling this an upgrade.
	if candHDR, libHDR := hdrRank(candidate.HDR), hdrRank(library.HDR); candHDR > libHDR {
		return VerdictUpgrade, "same quality with better HDR (" +
			strings.Join(candidate.HDR, "+") + " over " + hdrLabel(library.HDR) + ")"
	}

	if candQ < 0 || libQ < 0 {
		return VerdictInLibrary, "already in the library; quality could not be compared"
	}
	return VerdictInLibrary, "already in the library at " + upper(library.Quality)
}

func hdrLabel(tags []string) string {
	if len(tags) == 0 {
		return "none"
	}
	return strings.Join(tags, "+")
}

func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
