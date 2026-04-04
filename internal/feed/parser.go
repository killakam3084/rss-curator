package feed

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/pkg/models"
)

// Parser handles RSS feed parsing
type Parser struct {
	client   *http.Client
	enricher *ai.Enricher
}

// NewParser creates a new RSS parser
func NewParser() *Parser {
	return &Parser{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithEnricher attaches an AI enricher that fills in metadata when the regex
// parser leaves ShowName or Season unpopulated. This is optional — if not set
// the parser works exactly as before.
func (p *Parser) WithEnricher(e *ai.Enricher) *Parser {
	p.enricher = e
	return p
}

// RSS feed structures
type rss struct {
	Channel channel `xml:"channel"`
}

type channel struct {
	Items []item `xml:"item"`
}

type enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type item struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	GUID        string    `xml:"guid"`
	PubDate     string    `xml:"pubDate"`
	Description string    `xml:"description"`
	Enclosure   enclosure `xml:"enclosure"`
}

// Parse fetches and parses an RSS feed. The contentType is applied to every
// item returned so callers can route show and movie feeds differently.
func (p *Parser) Parse(feedURL string, contentType models.ContentType) ([]models.FeedItem, error) {
	resp, err := p.client.Get(feedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var feed rss
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	items := make([]models.FeedItem, 0, len(feed.Channel.Items))
	for _, rssItem := range feed.Channel.Items {
		item := models.FeedItem{
			Title:       rssItem.Title,
			Link:        rssItem.Link,
			GUID:        rssItem.GUID,
			Description: rssItem.Description,
		}

		// Parse pub date — try common RSS date formats in order
		pubDateFormats := []string{
			time.RFC1123Z,                    // Mon, 02 Jan 2006 15:04:05 -0700
			time.RFC1123,                     // Mon, 02 Jan 2006 15:04:05 MST
			"Mon, 2 Jan 2006 15:04:05 -0700", // single-digit day, numeric tz
			"Mon, 2 Jan 2006 15:04:05 MST",   // single-digit day, named tz
			time.RFC3339,
		}
		for _, layout := range pubDateFormats {
			if t, err := time.Parse(layout, strings.TrimSpace(rssItem.PubDate)); err == nil {
				item.PubDate = t
				break
			}
		}

		// Parse size: prefer <enclosure length="..."> (bytes), fall back to description text
		if rssItem.Enclosure.Length > 0 {
			item.Size = rssItem.Enclosure.Length
		} else {
			item.Size = parseSize(rssItem.Description)
		}

		// Extract metadata from title
		item.ContentType = contentType
		ParseParserMetadata(&item)

		// Optionally enrich missing fields via AI.
		if p.enricher != nil {
			p.enricher.Enrich(&item)
		}

		// Log the parsed link for observability
		if strings.Contains(item.Link, "download") {
			fmt.Printf("[Feed] Parsed authenticated download link: %s\n", item.Link)
		} else if strings.Contains(item.Link, "/t/") {
			fmt.Printf("[Feed] WARNING: Parsed info page link (not authenticated): %s\n", item.Link)
		}

		items = append(items, item)
	}

	return items, nil
}

// parseSize extracts size from description like "1.44 GB; TV/Web-DL"
func parseSize(description string) int64 {
	// Match patterns like "1.44 GB", "500 MB", "21 GB"
	re := regexp.MustCompile(`([\d.]+)\s*(GB|MB|KB)`)
	matches := re.FindStringSubmatch(description)

	if len(matches) != 3 {
		return 0
	}

	size, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	// Convert to bytes
	unit := strings.ToUpper(matches[2])
	switch unit {
	case "GB":
		return int64(size * 1024 * 1024 * 1024)
	case "MB":
		return int64(size * 1024 * 1024)
	case "KB":
		return int64(size * 1024)
	default:
		return 0
	}
}

// ParseTitleMetadata resets parsed metadata fields and reparses them from the
// title using the current regex rules. The item's ContentType must already be
// set before calling so movie vs. show logic is routed correctly. Intended for
// normal feed ingestion and for rematch/re-evaluation workflows.
func ParseTitleMetadata(item *models.FeedItem) {
	item.ShowName = ""
	item.Season = 0
	item.Episode = 0
	item.ReleaseYear = 0
	item.Quality = ""
	item.Codec = ""
	item.Source = ""
	item.ReleaseGroup = ""

	extractMetadata(item)
}

// ParseParserMetadata is an alias for ParseTitleMetadata kept for internal use
// inside Parse so the ContentType is preserved after being set by the caller.
func ParseParserMetadata(item *models.FeedItem) {
	item.ShowName = ""
	item.Season = 0
	item.Episode = 0
	item.ReleaseYear = 0
	item.Quality = ""
	item.Codec = ""
	item.Source = ""
	item.ReleaseGroup = ""

	extractMetadata(item)
}

// extractMetadata parses show name, season, episode, quality, etc. from title
func extractMetadata(item *models.FeedItem) {
	title := item.Title

	// Extract quality (1080p, 2160p, 720p, 4K)
	qualityRe := regexp.MustCompile(`(?i)\b(2160p|1080p|720p|4K)\b`)
	if matches := qualityRe.FindStringSubmatch(title); len(matches) > 0 {
		item.Quality = strings.ToUpper(matches[1])
	}

	// Extract codec (x264, x265, H264/H.264/H 264, H265/H.265/H 265, HEVC)
	codecRe := regexp.MustCompile(`(?i)\b(x264|x265|H[\s\.]?264|H[\s\.]?265|HEVC)\b`)
	if matches := codecRe.FindStringSubmatch(title); len(matches) > 0 {
		codec := strings.ToUpper(matches[1])
		// Normalize codec names
		if strings.Contains(codec, "265") || codec == "HEVC" {
			item.Codec = "x265"
		} else if strings.Contains(codec, "264") {
			item.Codec = "x264"
		} else {
			item.Codec = codec
		}
	}

	// Extract source (WEB-DL, BluRay, HDTV, WEBRip, etc.)
	sourceRe := regexp.MustCompile(`(?i)\b(WEB-DL|BluRay|HDTV|WEBRip|BDRip|DVDRip|AMZN|NF|DSNP|HMAX|ATVP)\b`)
	if matches := sourceRe.FindStringSubmatch(title); len(matches) > 0 {
		item.Source = matches[1]
	}

	// Extract release group (text after last dash or in brackets)
	groupRe := regexp.MustCompile(`-([A-Za-z0-9]+)(?:\[.*\])?$`)
	if matches := groupRe.FindStringSubmatch(title); len(matches) > 1 {
		item.ReleaseGroup = matches[1]
	}

	// Extract show name, season, episode (shows) or movie name + year (movies)
	if item.ContentType == models.ContentTypeMovie {
		// Movie title formats:
		//   "Transfusion 2023 1080p ..." (bare year)
		//   "Junk Films (2007) 1080p ..." (parenthesised year)
		yearRe := regexp.MustCompile(`(?:^|\s)\(?((19|20)\d{2})\)?(?:\s|$)`)
		if m := yearRe.FindStringSubmatchIndex(title); m != nil {
			yearStr := title[m[2]:m[3]]
			if yr, err := strconv.Atoi(yearStr); err == nil {
				item.ReleaseYear = yr
			}
			// Movie name is everything before the year token
			movieName := strings.TrimSpace(title[:m[0]])
			movieNameCleaned := strings.ReplaceAll(movieName, ".", " ")
			movieNameCleaned = strings.TrimSpace(movieNameCleaned)
			if movieNameCleaned == "" {
				movieNameCleaned = movieName
			}
			item.ShowName = movieNameCleaned
		} else {
			// Fallback: strip after resolution token
			movName := regexp.MustCompile(`(?i)\b(2160p|1080p|720p|4K)\b.*`).ReplaceAllString(title, "")
			movName = strings.ReplaceAll(movName, ".", " ")
			item.ShowName = strings.TrimSpace(movName)
		}
		return
	}

	// Show: extract show name, season, episode
	// Pattern: Show.Name.S01E02 or Show.Name.S01 or Show Name S01E02
	seasonEpisodeRe := regexp.MustCompile(`^(.+?)[\s.]+[Ss](\d+)(?:[Ee](\d+))?`)
	if matches := seasonEpisodeRe.FindStringSubmatch(title); len(matches) >= 3 {
		// Show name is everything before S01E02
		showName := matches[1]
		showName = strings.ReplaceAll(showName, ".", " ")
		showName = strings.TrimSpace(showName)
		item.ShowName = showName

		// Season number
		if season, err := strconv.Atoi(matches[2]); err == nil {
			item.Season = season
		}

		// Episode number (if present)
		if len(matches) > 3 && matches[3] != "" {
			if episode, err := strconv.Atoi(matches[3]); err == nil {
				item.Episode = episode
			}
		}
	} else {
		// If no season/episode pattern, just use the title as show name
		// Clean up common separators
		showName := title
		showName = regexp.MustCompile(`\d{4}p.*`).ReplaceAllString(showName, "")
		showName = strings.ReplaceAll(showName, ".", " ")
		showName = strings.TrimSpace(showName)
		item.ShowName = showName
	}
}
