package matcher

import (
	"fmt"
	"strings"

	"github.com/iillmaticc/rss-curator/pkg/models"
)

// Matcher filters feed items based on rules
type Matcher struct {
	showsConfig *models.ShowsConfig
	legacyRules *models.MatchRule
}

// NewMatcher creates a new matcher
func NewMatcher(showsConfig *models.ShowsConfig, legacyRules *models.MatchRule) *Matcher {
	return &Matcher{
		showsConfig: showsConfig,
		legacyRules: legacyRules,
	}
}

// Match checks if a feed item matches the configured rules
func (m *Matcher) Match(item models.FeedItem) (bool, string) {
	// If we have shows config, use that
	if m.showsConfig != nil {
		return m.matchWithShowsConfig(item)
	}
	
	// Fall back to legacy rules
	return m.matchLegacy(item)
}

// matchWithShowsConfig uses the new per-show rules
func (m *Matcher) matchWithShowsConfig(item models.FeedItem) (bool, string) {
	reasons := []string{}
	
	// Find matching show rule
	var showRule *models.ShowRule
	for i := range m.showsConfig.Shows {
		if strings.Contains(strings.ToLower(item.ShowName), strings.ToLower(m.showsConfig.Shows[i].Name)) {
			showRule = &m.showsConfig.Shows[i]
			break
		}
	}
	
	if showRule == nil {
		return false, "show not in watch list"
	}
	
	reasons = append(reasons, fmt.Sprintf("matches show: %s", showRule.Name))
	
	// Get effective rules (show-specific or defaults)
	minQuality := showRule.MinQuality
	if minQuality == "" {
		minQuality = m.showsConfig.Defaults.MinQuality
	}
	
	preferredCodec := showRule.PreferredCodec
	if preferredCodec == "" {
		preferredCodec = m.showsConfig.Defaults.PreferredCodec
	}
	
	preferredGroups := showRule.PreferredGroups
	if len(preferredGroups) == 0 {
		preferredGroups = m.showsConfig.Defaults.PreferredGroups
	}
	
	excludeGroups := showRule.ExcludeGroups
	if len(excludeGroups) == 0 {
		excludeGroups = m.showsConfig.Defaults.ExcludeGroups
	}
	
	// Check quality
	if !meetsQuality(item.Quality, minQuality) {
		return false, fmt.Sprintf("quality %s below minimum %s", item.Quality, minQuality)
	}
	reasons = append(reasons, fmt.Sprintf("quality: %s", item.Quality))
	
	// Check codec preference
	if preferredCodec != "" && strings.EqualFold(item.Codec, preferredCodec) {
		reasons = append(reasons, fmt.Sprintf("preferred codec: %s", item.Codec))
	}
	
	// Check release group exclusions
	if isExcludedGroup(item.ReleaseGroup, excludeGroups) {
		return false, fmt.Sprintf("release group %s is excluded", item.ReleaseGroup)
	}
	
	// Check preferred release groups
	if isPreferredGroup(item.ReleaseGroup, preferredGroups) {
		reasons = append(reasons, fmt.Sprintf("preferred group: %s", item.ReleaseGroup))
	}
	
	return true, strings.Join(reasons, ", ")
}

// matchLegacy uses the old env-var based rules
func (m *Matcher) matchLegacy(item models.FeedItem) (bool, string) {
	reasons := []string{}
	
	if !matchesShowName(item.ShowName, m.legacyRules.ShowNames) {
		return false, "show name not in watch list"
	}
	reasons = append(reasons, fmt.Sprintf("matches show: %s", item.ShowName))
	
	if !meetsQuality(item.Quality, m.legacyRules.MinQuality) {
		return false, fmt.Sprintf("quality %s below minimum %s", item.Quality, m.legacyRules.MinQuality)
	}
	reasons = append(reasons, fmt.Sprintf("quality: %s", item.Quality))
	
	if m.legacyRules.PreferredCodec != "" && strings.EqualFold(item.Codec, m.legacyRules.PreferredCodec) {
		reasons = append(reasons, fmt.Sprintf("preferred codec: %s", item.Codec))
	}
	
	if isExcludedGroup(item.ReleaseGroup, m.legacyRules.ExcludeGroups) {
		return false, fmt.Sprintf("release group %s is excluded", item.ReleaseGroup)
	}
	
	if isPreferredGroup(item.ReleaseGroup, m.legacyRules.PreferredGroups) {
		reasons = append(reasons, fmt.Sprintf("preferred group: %s", item.ReleaseGroup))
	}
	
	return true, strings.Join(reasons, ", ")
}

// Helper functions
func matchesShowName(showName string, showNames []string) bool {
	if len(showNames) == 0 {
		return true
	}
	
	showNameLower := strings.ToLower(showName)
	for _, name := range showNames {
		if strings.Contains(showNameLower, strings.ToLower(name)) {
			return true
		}
	}
	return false
}

func meetsQuality(quality, minQuality string) bool {
	if minQuality == "" {
		return true
	}
	
	qualityRank := map[string]int{
		"720P":  1,
		"1080P": 2,
		"2160P": 3,
		"4K":    3,
	}
	
	itemRank, ok1 := qualityRank[strings.ToUpper(quality)]
	minRank, ok2 := qualityRank[strings.ToUpper(minQuality)]
	
	if !ok1 || !ok2 {
		return true
	}
	
	return itemRank >= minRank
}

func isExcludedGroup(group string, excludeGroups []string) bool {
	groupLower := strings.ToLower(group)
	for _, excluded := range excludeGroups {
		if strings.EqualFold(groupLower, excluded) {
			return true
		}
	}
	return false
}

func isPreferredGroup(group string, preferredGroups []string) bool {
	groupLower := strings.ToLower(group)
	for _, preferred := range preferredGroups {
		if strings.EqualFold(groupLower, preferred) {
			return true
		}
	}
	return false
}

// MatchAll filters a list of feed items and returns matches
func (m *Matcher) MatchAll(items []models.FeedItem) []models.StagedTorrent {
	staged := []models.StagedTorrent{}
	
	for _, item := range items {
		if matches, reason := m.Match(item); matches {
			staged = append(staged, models.StagedTorrent{
				FeedItem:    item,
				MatchReason: reason,
				Status:      "pending",
			})
		}
	}
	
	return staged
}
