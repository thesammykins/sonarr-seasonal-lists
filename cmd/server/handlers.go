package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/calmcacil/anilistgen/internal/cache"
	"github.com/calmcacil/anilistgen/internal/config"
	"github.com/calmcacil/anilistgen/internal/scheduler"
)

// ListFetcher is the subset of *scheduler.Scheduler that the /list handler
// uses. Defined as an interface so handler tests can inject a stub.
type ListFetcher interface {
	FetchAndStore(ctx context.Context, season string, year int, category string) error
}

// Handlers groups the HTTP handlers and the dependencies they need.
// Construct via &Handlers{DB, Sched, Cfg} and call Mux to get a
// *http.ServeMux ready for an http.Server.
type Handlers struct {
	DB    *cache.Cache
	Sched ListFetcher
	Cfg   *config.Config
}

// Mux registers the public routes (/list, /health) and, when StatsAddr
// is empty, the /cache/stats route. When StatsAddr is set the stats
// route is intentionally omitted here and is served by a separate
// http.Server bound to that address.
func (h *Handlers) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/list", h.List)
	mux.HandleFunc("/health", h.Health)
	if h.Cfg.StatsAddr == "" {
		mux.HandleFunc("/cache/stats", h.CacheStats)
	}
	return mux
}

// StatsMux returns a mux serving only /cache/stats, intended for a
// separate http.Server bound to StatsAddr. Returns nil when StatsAddr
// is empty.
func (h *Handlers) StatsMux() *http.ServeMux {
	if h.Cfg.StatsAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/cache/stats", h.CacheStats)
	return mux
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	season := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("season")))
	if season == "" {
		season = "ALL"
	}

	yearStr := r.URL.Query().Get("year")
	year := time.Now().Year()
	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil && y > 0 {
			year = y
		}
	}

	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category == "" {
		category = "series"
	}
	if category != "series" && category != "series-new" {
		category = "series"
	}

	data, _, isPending, ok, err := h.DB.Get(season, year, category)
	if err != nil {
		slog.Error("cache get failed",
			"season", season, "year", year, "category", category, "error", err)
		http.Error(w, "cache error", http.StatusInternalServerError)
		return
	}
	if !ok {
		slog.Info("cache miss, triggering backfill",
			"season", season,
			"year", year,
			"category", category,
		)

		if err := h.Sched.FetchAndStore(r.Context(), season, year, category); err != nil {
			slog.Error("trigger backfill failed", "error", err)
		}

		writeJSON(w, []byte("[]"))
		return
	}

	if isPending {
		writeJSON(w, []byte("[]"))
		return
	}

	writeJSON(w, data)
}

func (h *Handlers) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handlers) CacheStats(w http.ResponseWriter, _ *http.Request) {
	stats, err := h.DB.Stats()
	if err != nil {
		slog.Error("cache stats failed", "error", err)
		http.Error(w, "cache error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func writeJSON(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// ensure interface compliance at compile time
var _ ListFetcher = (*scheduler.Scheduler)(nil)
