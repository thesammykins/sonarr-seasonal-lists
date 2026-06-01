package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/calmcacil/anilistgen/internal/cache"
	"github.com/calmcacil/anilistgen/internal/config"
	"github.com/calmcacil/anilistgen/internal/scheduler"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	setupLogging(cfg.LogLevel)

	slog.Info("starting sonarr-seasonal",
		"port", cfg.Port,
		"prewarm_years", cfg.PrewarmYears,
		"prewarm_seasons", cfg.PrewarmSeasons,
	)

	db, err := cache.Open(cfg.CacheDBPath)
	if err != nil {
		return fmt.Errorf("open cache: %w", err)
	}
	defer db.Close()

	sched := scheduler.New(db, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := http.NewServeMux()
	mux.HandleFunc("/list", handleList(db, sched, cfg))
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/cache/stats", handleCacheStats(db))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	sched.Start(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	slog.Info("shutting down", "signal", sig)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	return server.Shutdown(shutdownCtx)
}

func handleList(db *cache.Cache, sched *scheduler.Scheduler, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		data, _, isPending, ok, err := db.Get(season, year, category)
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

			if err := sched.FetchAndStore(r.Context(), season, year, category); err != nil {
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
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func handleCacheStats(db *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := db.Stats()
		if err != nil {
			slog.Error("cache stats failed", "error", err)
			http.Error(w, "cache error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}

func writeJSON(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func setupLogging(level string) {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})
	slog.SetDefault(slog.New(handler))
}
