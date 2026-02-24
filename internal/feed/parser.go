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

	"github.com/iillmaticc/rss-curator/pkg/models"
)

// Parser handles RSS feed parsing
type Parser struct {
	client *http.Client
}

// NewParser creates a new RSS parser
func NewParser() *Parser {
	return &Parser{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// RSS feed structures
type rss struct {
	Channel channel `xml:"channel"`
}

type channel struct {
	Items []item `xml:"item"`
}

type item struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

// Parse fetches and parses an RSS feed
func (p *Parser) Parse(feedURL string) ([]models.FeedItem, error) {
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

		// Parse pub date
		if pubDate, err := time.Parse(time.RFC1123Z, rssItem.PubDate); err == nil {
			item.PubDate = pubDate
		}

		// Parse size from description (e.g., "1.44 GB; TV/Web-DL")
		item.Size = parseSize(rssItem.Description)

		// Extract metadata from title
		extractMetadata(&item)

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

// extractMetadata parses show name, season, episode, quality, etc. from title
func extractMetadata(item *models.FeedItem) {
	title := item.Title

	// Extract quality (1080p, 2160p, 720p, 4K)
	qualityRe := regexp.MustCompile(`(?i)\b(2160p|1080p|720p|4K)\b`)
	if matches := qualityRe.FindStringSubmatch(title); len(matches) > 0 {
		item.Quality = strings.ToUpper(matches[1])
	}

	// Extract codec (x264, x265, H264, H265, HEVC)
	codecRe := regexp.MustCompile(`(?i)\b(x264|x265|H\.?264|H\.?265|HEVC)\b`)
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

	// Extract show name, season, episode
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
