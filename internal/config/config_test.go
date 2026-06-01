package config

import (
	"os"
	"testing"
	"time"
)

func TestResolveSeasons_All(t *testing.T) {
	t.Parallel()

	got := ResolveSeasons([]string{"all"})
	want := []string{"WINTER", "SPRING", "SUMMER", "FALL"}
	if len(got) != len(want) {
		t.Fatalf("expected %d seasons, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("season[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveSeasons_AllCaseInsensitive(t *testing.T) {
	t.Parallel()

	got := ResolveSeasons([]string{"ALL"})
	if len(got) != 4 {
		t.Errorf("expected 4 seasons for ALL, got %d", len(got))
	}
}

func TestResolveSeasons_Specific(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"winter", "WINTER"},
		{"WINTER", "WINTER"},
		{"Spring", "SPRING"},
		{"summer", "SUMMER"},
		{"FALL", "FALL"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := ResolveSeasons([]string{tc.input})
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("ResolveSeasons(%q) = %v, want [%q]", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveSeasons_Empty(t *testing.T) {
	t.Parallel()

	got := ResolveSeasons(nil)
	if len(got) != 4 {
		t.Errorf("expected 4 seasons for nil, got %d: %v", len(got), got)
	}

	got2 := ResolveSeasons([]string{})
	if len(got2) != 4 {
		t.Errorf("expected 4 seasons for empty slice, got %d: %v", len(got2), got2)
	}
}

func TestAllSeasons(t *testing.T) {
	t.Parallel()

	got := AllSeasons()
	want := []string{"WINTER", "SPRING", "SUMMER", "FALL"}
	if len(got) != len(want) {
		t.Fatalf("expected %d seasons, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllSeasons[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAheadMonthsOrDefault(t *testing.T) {
	t.Parallel()

	t.Run("nil defaults to 3", func(t *testing.T) {
		cfg := &Config{}
		if got := cfg.AheadMonthsOrDefault(); got != 3 {
			t.Errorf("expected 3, got %d", got)
		}
	})

	t.Run("respects set value", func(t *testing.T) {
		v := 6
		cfg := &Config{AheadMonths: &v}
		if got := cfg.AheadMonthsOrDefault(); got != 6 {
			t.Errorf("expected 6, got %d", got)
		}
	})
}

func TestLoad_Defaults(t *testing.T) {
	for _, key := range []string{
		"PORT", "STATS_ADDR", "MAX_PER_SEASON", "CACHE_DB_PATH", "CACHE_STALE_DAYS",
		"REFRESH_CURRENT_DAYS", "REFRESH_PAST_DAYS", "LOG_LEVEL",
		"PREWARM_YEARS", "PREWARM_SEASONS", "AHEAD_MONTHS",
		"ALG_ANILIST_AHEAD_MONTHS", "ALG_ANILIST_TIMEOUT_MINUTES",
		"ALG_ANILIST_INCLUDE_ONA", "ALG_ANILIST_WINTER_OVERFLOW",
		"ALG_ANILIST_EXCLUDE_TAGS",
	} {
		os.Unsetenv(key)
	}

	cfg := Load()

	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.StatsAddr != "" {
		t.Errorf("StatsAddr = %q, want empty (stats listener disabled by default)", cfg.StatsAddr)
	}
	if cfg.MaxPerSeason != DefaultMaxPerSeason {
		t.Errorf("MaxPerSeason = %d, want %d", cfg.MaxPerSeason, DefaultMaxPerSeason)
	}
	if cfg.CacheDBPath != DefaultCacheDBPath {
		t.Errorf("CacheDBPath = %q, want %q", cfg.CacheDBPath, DefaultCacheDBPath)
	}
	if cfg.CacheStaleDays != DefaultCacheStaleDays {
		t.Errorf("CacheStaleDays = %d, want %d", cfg.CacheStaleDays, DefaultCacheStaleDays)
	}
	if cfg.RefreshCurrentDays != DefaultRefreshCurrentDays {
		t.Errorf("RefreshCurrentDays = %d, want %d", cfg.RefreshCurrentDays, DefaultRefreshCurrentDays)
	}
	if cfg.RefreshPastDays != DefaultRefreshPastDays {
		t.Errorf("RefreshPastDays = %d, want %d", cfg.RefreshPastDays, DefaultRefreshPastDays)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if len(cfg.PrewarmYears) != 1 || cfg.PrewarmYears[0] != time.Now().Year() {
		t.Errorf("PrewarmYears = %v, want [%d]", cfg.PrewarmYears, time.Now().Year())
	}
	if cfg.AheadMonthsOrDefault() != 3 {
		t.Errorf("AheadMonths = %d, want 3", cfg.AheadMonthsOrDefault())
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	keys := []string{
		"PORT", "STATS_ADDR", "MAX_PER_SEASON", "CACHE_STALE_DAYS", "REFRESH_CURRENT_DAYS",
		"REFRESH_PAST_DAYS", "LOG_LEVEL", "PREWARM_YEARS", "PREWARM_SEASONS",
		"AHEAD_MONTHS",
	}
	for _, key := range keys {
		os.Setenv(key, "")
		os.Unsetenv(key)
	}

	os.Setenv("PORT", "9090")
	os.Setenv("STATS_ADDR", "127.0.0.1:9091")
	os.Setenv("MAX_PER_SEASON", "50")
	os.Setenv("CACHE_STALE_DAYS", "30")
	os.Setenv("REFRESH_CURRENT_DAYS", "3")
	os.Setenv("REFRESH_PAST_DAYS", "60")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("PREWARM_YEARS", "2025,2026")
	os.Setenv("PREWARM_SEASONS", "winter,spring")
	os.Setenv("AHEAD_MONTHS", "6")
	t.Cleanup(func() {
		for _, key := range keys {
			os.Unsetenv(key)
		}
	})

	cfg := Load()

	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.StatsAddr != "127.0.0.1:9091" {
		t.Errorf("StatsAddr = %q, want 127.0.0.1:9091", cfg.StatsAddr)
	}
	if cfg.MaxPerSeason != 50 {
		t.Errorf("MaxPerSeason = %d, want 50", cfg.MaxPerSeason)
	}
	if cfg.CacheStaleDays != 30 {
		t.Errorf("CacheStaleDays = %d, want 30", cfg.CacheStaleDays)
	}
	if cfg.RefreshCurrentDays != 3 {
		t.Errorf("RefreshCurrentDays = %d, want 3", cfg.RefreshCurrentDays)
	}
	if cfg.RefreshPastDays != 60 {
		t.Errorf("RefreshPastDays = %d, want 60", cfg.RefreshPastDays)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if len(cfg.PrewarmYears) != 2 || cfg.PrewarmYears[0] != 2025 || cfg.PrewarmYears[1] != 2026 {
		t.Errorf("PrewarmYears = %v, want [2025 2026]", cfg.PrewarmYears)
	}
	if len(cfg.PrewarmSeasons) != 2 || cfg.PrewarmSeasons[0] != "WINTER" || cfg.PrewarmSeasons[1] != "SPRING" {
		t.Errorf("PrewarmSeasons = %v, want [WINTER SPRING]", cfg.PrewarmSeasons)
	}
	if cfg.AheadMonthsOrDefault() != 6 {
		t.Errorf("AheadMonths = %d, want 6", cfg.AheadMonthsOrDefault())
	}
}

func TestLoad_Booleans(t *testing.T) {
	t.Run("include_ona true", func(t *testing.T) {
		os.Setenv("ALG_ANILIST_INCLUDE_ONA", "true")
		t.Cleanup(func() { os.Unsetenv("ALG_ANILIST_INCLUDE_ONA") })
		cfg := Load()
		if !cfg.IncludeONA {
			t.Error("expected IncludeONA true")
		}
	})

	t.Run("winter_overflow false", func(t *testing.T) {
		os.Setenv("ALG_ANILIST_WINTER_OVERFLOW", "false")
		t.Cleanup(func() { os.Unsetenv("ALG_ANILIST_WINTER_OVERFLOW") })
		cfg := Load()
		if cfg.WinterOverflow {
			t.Error("expected WinterOverflow false")
		}
	})
}

func TestLoad_ExcludeTags(t *testing.T) {
	os.Setenv("ALG_ANILIST_EXCLUDE_TAGS", "hentai,guro")
	t.Cleanup(func() { os.Unsetenv("ALG_ANILIST_EXCLUDE_TAGS") })

	cfg := Load()
	if len(cfg.ExcludeTags) != 2 || cfg.ExcludeTags[0] != "hentai" {
		t.Errorf("ExcludeTags = %v, want [hentai guro]", cfg.ExcludeTags)
	}
}
