package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
		"stats_addr", cfg.StatsAddr,
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

	h := &Handlers{DB: db, Sched: sched, Cfg: cfg}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      h.Mux(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	// Optional second listener for /cache/stats. When STATS_ADDR is empty,
	// StatsMux returns nil and we skip the second server entirely.
	var statsServer *http.Server
	if statsMux := h.StatsMux(); statsMux != nil {
		statsServer = &http.Server{
			Addr:         cfg.StatsAddr,
			Handler:      statsMux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  30 * time.Second,
		}
	}

	go func() {
		slog.Info("listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	if statsServer != nil {
		go func() {
			slog.Info("stats listening", "addr", statsServer.Addr)
			if err := statsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("stats server error", "error", err)
			}
		}()
	}

	sched.Start(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	slog.Info("shutting down", "signal", sig)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if statsServer != nil {
		if err := statsServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
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
