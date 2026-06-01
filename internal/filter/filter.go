package filter

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/calmcacil/anilistgen/internal/anilist"
)

type Config struct {
	Blacklist   []string
	ExcludeTags []string
}

// Filter removes shows with short duration, matching blacklist entries, or
// excluded content tags. Returns the filtered slice.
func Filter(shows []anilist.Show, cfg Config) []anilist.Show {
	filtered := make([]anilist.Show, 0, len(shows))
	for _, show := range shows {
		title := show.DisplayTitle()
		idMal := 0
		if show.IDMal != nil {
			idMal = *show.IDMal
		}

		if show.SkipByDuration() {
			slog.Debug("skipped show (duration <= 10 min)",
				"title", title,
				"duration", show.Duration)
			continue
		}

		if isBlacklisted(title, idMal, cfg.Blacklist) {
			slog.Debug("skipped show (blacklisted)",
				"title", title,
				"idMal", idMal)
			continue
		}

		if hasExcludedTag(show, cfg.ExcludeTags) {
			slog.Debug("skipped show (excluded tag)",
				"title", title,
				"tags", show.Tags)
			continue
		}

		filtered = append(filtered, show)
	}

	skipped := len(shows) - len(filtered)
	if skipped > 0 {
		slog.Info("filtered shows",
			"total", len(shows),
			"skipped", skipped,
			"remaining", len(filtered))
	}

	return filtered
}

func isBlacklisted(title string, idMal int, blacklist []string) bool {
	for _, entry := range blacklist {
		if entry == "" {
			continue
		}
		if malID, err := strconv.Atoi(entry); err == nil && malID > 0 {
			if malID == idMal {
				return true
			}
			continue
		}
		if strings.Contains(strings.ToLower(title), strings.ToLower(entry)) {
			return true
		}
	}
	return false
}

func hasExcludedTag(show anilist.Show, tags []string) bool {
	for _, exclude := range tags {
		if exclude == "" {
			continue
		}
		if show.HasTag(exclude) {
			return true
		}
	}
	return false
}

// Future removes shows whose start date is more than aheadMonths
// months in the future. Returns the original slice if aheadMonths is <= 0.
func Future(shows []anilist.Show, aheadMonths int) []anilist.Show {
	if aheadMonths <= 0 {
		return shows
	}
	filtered := make([]anilist.Show, 0, len(shows))
	for _, show := range shows {
		title := show.DisplayTitle()
		if !show.IsWithinMonths(aheadMonths) {
			slog.Debug("skipped show (too far in the future)",
				"title", title,
				"ahead_months", aheadMonths)
			continue
		}
		filtered = append(filtered, show)
	}
	return filtered
}
