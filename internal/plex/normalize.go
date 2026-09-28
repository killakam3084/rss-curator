// Package plex provides identity helpers shared by the Plex library sync and
// the reconciliation pass that annotates staged torrents.
package plex

import (
	"strings"
	"unicode"
)

// leadingArticles are dropped from the front of a title so that "The Bear" and
// "Bear" collapse to the same key. Plex and release groups disagree about
// whether to keep them.
var leadingArticles = []string{"the ", "a ", "an "}

// diacriticFold maps the accented Latin characters that realistically show up
// in media titles to their ASCII equivalents. A full Unicode normalisation
// would need golang.org/x/text; this covers the practical cases without
// adding a dependency.
var diacriticFold = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a', 'å': 'a',
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i',
	'ò': 'o', 'ó': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o', 'ø': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u',
	'ñ': 'n', 'ç': 'c', 'ý': 'y', 'ÿ': 'y',
}

// NormalizeTitle reduces a show or movie title to a comparison key.
//
// It is only used for the fallback match tier — an external-ID (GUID) match is
// always preferred. The transformation is: lowercase, fold diacritics, turn
// separator punctuation into spaces, drop remaining punctuation, strip a
// trailing year, drop a leading article, and collapse whitespace.
func NormalizeTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range strings.ToLower(s) {
		if folded, ok := diacriticFold[r]; ok {
			b.WriteRune(folded)
			continue
		}
		switch {
		case r == '.' || r == '_' || r == '-' || unicode.IsSpace(r):
			b.WriteRune(' ')
		case r == '&':
			b.WriteString(" and ")
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			// Drop apostrophes and the like without introducing a word break,
			// so "don't" and "dont" agree.
		}
	}

	out := strings.Join(strings.Fields(b.String()), " ")
	out = stripTrailingYear(out)

	for _, article := range leadingArticles {
		if strings.HasPrefix(out, article) {
			out = out[len(article):]
			break
		}
	}
	return strings.TrimSpace(out)
}

// stripTrailingYear removes a trailing 4-digit year (1900–2099) so that
// "dune part two 2024" and "dune part two" collapse together. A title that is
// nothing but a year is left alone.
func stripTrailingYear(s string) string {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return s
	}
	last := fields[len(fields)-1]
	if len(last) != 4 {
		return s
	}
	for _, r := range last {
		if r < '0' || r > '9' {
			return s
		}
	}
	if last < "1900" || last > "2099" {
		return s
	}
	return strings.Join(fields[:len(fields)-1], " ")
}
