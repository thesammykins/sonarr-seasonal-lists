package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/calmcacil/anilistgen/internal/anilist"
	"github.com/calmcacil/anilistgen/internal/cache"
	"github.com/calmcacil/anilistgen/internal/config"
	"github.com/calmcacil/anilistgen/internal/filter"
	"github.com/calmcacil/anilistgen/internal/mapping"
	"golang.org/x/sync/singleflight"
)

type Scheduler struct {
	cache    *cache.Cache
	cfg      *config.Config
	client   *anilist.Client
	resolver *mapping.Resolver
	sfg      singleflight.Group
}

type Show struct {
	TVDBID int    `json:"tvdbId"`
	Title  string `json:"title,omitempty"`
}

func New(c *cache.Cache, cfg *config.Config) *Scheduler {
	return &Scheduler{
		cache:  c,
		cfg:    cfg,
		client: anilist.New(),
	}
}

func (s *Scheduler) loadResolver() {
	if s.resolver != nil {
		return
	}
	cm, err := mapping.LoadCommunityMapping(s.cfg.CommunityMappingPath)
	if err != nil {
		slog.Error("failed to load community mapping", "error", err)
	}
	s.resolver = mapping.NewResolver(cm)
}

func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		s.loadResolver()

		slog.Info("starting prewarm")
		if err := s.Prewarm(ctx); err != nil {
			slog.Error("prewarm failed", "error", err)
		}
		slog.Info("prewarm complete")
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshStale(ctx)
				s.prune(ctx)
			}
		}
	}()
}

func (s *Scheduler) Prewarm(ctx context.Context) error {
	for _, year := range s.cfg.PrewarmYears {
		for _, season := range s.cfg.PrewarmSeasons {
			for _, category := range []string{"series", "series-new"} {
				exists, err := s.cache.Exists(season, year, category)
				if err != nil {
					slog.Warn("prewarm exists check failed", "season", season, "year", year, "category", category, "error", err)
				}
				if exists {
					continue
				}
				slog.Info("prewarming", "season", season, "year", year, "category", category)
				s.refresh(ctx, season, year, category)
			}
		}
	}
	return nil
}

func (s *Scheduler) Refresh(ctx context.Context, season string, year int, category string) {
	s.refresh(ctx, season, year, category)
}

func (s *Scheduler) FetchAndStore(ctx context.Context, season string, year int, category string) error {
	exists, err := s.cache.Exists(season, year, category)
	if err != nil {
		slog.Warn("exists check failed", "season", season, "year", year, "category", category, "error", err)
	}
	if exists {
		return nil
	}
	if err := s.cache.SetEmpty(season, year, category); err != nil {
		slog.Warn("set empty failed", "season", season, "year", year, "category", category, "error", err)
	}

	// Coalesce concurrent triggers for the same key. The singleflight call
	// blocks all but the first caller until refresh returns, so we run it
	// inline rather than spawning a goroutine per request. Detach the
	// context so a Sonarr request being cancelled does not abort the
	// in-flight refresh.
	key := fmt.Sprintf("%s|%d|%s", season, year, category)
	_, _, _ = s.sfg.Do(key, func() (any, error) {
		s.refresh(context.WithoutCancel(ctx), season, year, category)
		return nil, nil
	})
	return nil
}

func (s *Scheduler) refresh(ctx context.Context, season string, year int, category string) {
	seasons := []string{season}
	if season == "ALL" {
		seasons = config.AllSeasons()
	}

	allShows := make([]Show, 0)
	formats := []string{"TV"}
	if s.cfg.IncludeONA {
		formats = append(formats, "ONA")
	}

	for _, ssn := range seasons {
		shows := s.processSeason(ctx, ssn, year, formats, category)
		allShows = append(allShows, shows...)
	}

	data, err := json.Marshal(allShows)
	if err != nil {
		slog.Error("marshal shows", "season", season, "year", year, "error", err)
		return
	}

	if err := s.cache.Set(season, year, category, data); err != nil {
		slog.Error("cache set", "season", season, "year", year, "error", err)
		return
	}

	slog.Info("cached", "season", season, "year", year, "category", category, "shows", len(allShows))
}

func (s *Scheduler) processSeason(ctx context.Context, season string, year int, formats []string, category string) []Show {
	slog.Info("fetching season", "season", season, "year", year)

	shows, err := s.client.FetchSeason(ctx, season, year, s.cfg.MaxPerSeason, formats)
	if err != nil {
		slog.Error("fetch failed", "season", season, "year", year, "error", err)
		return nil
	}

	if s.cfg.WinterOverflow && season == "WINTER" {
		shows = s.fetchWinterOverflow(ctx, year, formats, shows)
	}

	if season == "WINTER" {
		shows = filterWinterMonth(shows)
	}

	shows = filterSeries(shows)

	if category == "series-new" {
		shows = filterNewSeries(shows)
	}

	shows = filter.Filter(shows, filter.Config{
		Blacklist:   nil,
		ExcludeTags: s.cfg.ExcludeTags,
	})
	shows = filter.FilterFuture(shows, s.cfg.AheadMonthsOrDefault())

	return s.resolveShows(shows)
}

func (s *Scheduler) fetchWinterOverflow(ctx context.Context, year int, formats []string, shows []anilist.Show) []anilist.Show {
	overflowYear := year - 1
	overflow, err := s.client.FetchSeason(ctx, "WINTER", overflowYear, s.cfg.MaxPerSeason, formats)
	if err != nil {
		slog.Warn("winter overflow fetch failed", "year", overflowYear, "error", err)
		return shows
	}

	seen := make(map[int]bool, len(shows))
	for _, sh := range shows {
		seen[sh.ID] = true
	}

	var added int
	for _, sh := range overflow {
		if sh.StartDate.Month != nil && *sh.StartDate.Month == 12 && !seen[sh.ID] {
			shows = append(shows, sh)
			seen[sh.ID] = true
			added++
		}
	}

	if added > 0 {
		slog.Info("winter overflow merged", "year", year, "overflow_year", overflowYear, "added", added, "total", len(shows))
	}

	return shows
}

func (s *Scheduler) resolveShows(shows []anilist.Show) []Show {
	if s.resolver == nil {
		slog.Warn("resolver not yet loaded, skipping resolution")
		return nil
	}
	resolved := s.resolver.ResolveBatch(shows)
	var out []Show
	for _, show := range shows {
		if r, ok := resolved[show.ID]; ok && r.Resolved {
			out = append(out, Show{TVDBID: r.TVDBID, Title: r.Title})
		}
	}
	return out
}

func (s *Scheduler) refreshStale(ctx context.Context) {
	currentYear := time.Now().Year()
	keys, err := s.cache.NeedsRefresh(currentYear, s.cfg.RefreshCurrentDays, s.cfg.RefreshPastDays)
	if err != nil {
		slog.Error("needs refresh query failed", "error", err)
		return
	}
	for _, key := range keys {
		slog.Info("refreshing stale", "season", key.Season, "year", key.Year, "category", key.Category)
		s.refresh(ctx, key.Season, key.Year, key.Category)
	}
}

func (s *Scheduler) prune(ctx context.Context) {
	n, err := s.cache.PruneStale(s.cfg.CacheStaleDays)
	if err != nil {
		slog.Error("prune failed", "error", err)
		return
	}
	if n > 0 {
		slog.Info("pruned cache entries", "count", n)
	}
}

func filterSeries(shows []anilist.Show) []anilist.Show {
	var out []anilist.Show
	for _, sh := range shows {
		if sh.IsSeries() {
			out = append(out, sh)
		}
	}
	return out
}

func filterNewSeries(shows []anilist.Show) []anilist.Show {
	var out []anilist.Show
	for _, sh := range shows {
		if sh.IsNew() {
			out = append(out, sh)
		}
	}
	return out
}

func filterWinterMonth(shows []anilist.Show) []anilist.Show {
	var filtered []anilist.Show
	for _, sh := range shows {
		if sh.IsWinterStart() {
			filtered = append(filtered, sh)
		}
	}
	return filtered
}
