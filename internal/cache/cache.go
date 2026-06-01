package cache

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	// modernc.org/sqlite registers the "sqlite" driver with database/sql.
	_ "modernc.org/sqlite"
)

type Cache struct {
	db     *sql.DB
	hits   atomic.Int64
	misses atomic.Int64
}

type Key struct {
	Season   string
	Year     int
	Category string
}

type Stats struct {
	Entries int
	Hits    int64
	Misses  int64
}

// DefaultPendingTimeout is how long a SetEmpty row can sit in the cache
// before Get evicts it and returns a miss, so the handler re-triggers a
// fresh refresh instead of returning [] forever. 30 minutes is long enough
// to ride out a brief AniList outage and short enough that one bad refresh
// does not pin Sonarr to empty lists for hours.
const DefaultPendingTimeout = 30 * time.Minute

func Open(path string) (*Cache, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS season_cache (
			season    TEXT NOT NULL,
			year      INTEGER NOT NULL,
			category  TEXT NOT NULL,
			data      BLOB NOT NULL,
			is_empty  INTEGER NOT NULL DEFAULT 0,
			fetched_at INTEGER NOT NULL,
			last_hit  INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (season, year, category)
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &Cache{db: db}, nil
}

func (c *Cache) Close() error {
	return c.db.Close()
}

func (c *Cache) Get(season string, year int, category string) (data []byte, fresh, isPending, ok bool, err error) {
	var raw []byte
	var isEmpty int
	var fetchedAt int64

	rowErr := c.db.QueryRow(
		`SELECT data, is_empty, fetched_at FROM season_cache WHERE season=? AND year=? AND category=?`,
		season, year, category,
	).Scan(&raw, &isEmpty, &fetchedAt)

	if rowErr != nil {
		c.misses.Add(1)
		return nil, false, false, false, nil
	}

	// If the entry is still pending (SetEmpty, no successful Set yet) and
	// has been pending longer than the recovery timeout, evict it so the
	// caller re-triggers a fresh refresh instead of returning [] forever.
	if isEmpty == 1 && time.Since(time.Unix(fetchedAt, 0)) > DefaultPendingTimeout {
		_, delErr := c.db.Exec(
			`DELETE FROM season_cache WHERE season=? AND year=? AND category=?`,
			season, year, category,
		)
		if delErr == nil {
			c.misses.Add(1)
			return nil, false, false, false, nil
		}
		slog.Warn("evict stuck pending entry failed",
			"season", season, "year", year, "category", category, "error", delErr)
		// fall through and return as pending; the row is still there
	}

	c.hits.Add(1)

	// Update last_hit. Surface the error to the caller rather than silently
	// dropping it: a flaky DB that stops updating last_hit will cause
	// PruneStale to evict entries the user is still requesting.
	if _, updateErr := c.db.Exec(
		`UPDATE season_cache SET last_hit=? WHERE season=? AND year=? AND category=?`,
		time.Now().Unix(), season, year, category,
	); updateErr != nil {
		return nil, false, false, false, fmt.Errorf("update last_hit: %w", updateErr)
	}

	if isEmpty == 1 {
		return nil, false, true, true, nil
	}

	fresh = time.Since(time.Unix(fetchedAt, 0)) < 24*time.Hour
	return raw, fresh, false, true, nil
}

func (c *Cache) Set(season string, year int, category string, data []byte) error {
	now := time.Now().Unix()
	_, err := c.db.Exec(
		`INSERT OR REPLACE INTO season_cache (season, year, category, data, is_empty, fetched_at, last_hit)
		 VALUES (?, ?, ?, ?, 0, ?, ?)`,
		season, year, category, data, now, now,
	)
	return err
}

func (c *Cache) SetEmpty(season string, year int, category string) error {
	now := time.Now().Unix()
	_, err := c.db.Exec(
		`INSERT OR REPLACE INTO season_cache (season, year, category, data, is_empty, fetched_at, last_hit)
		 VALUES (?, ?, ?, X'5B5D', 1, ?, ?)`,
		season, year, category, now, now,
	)
	return err
}

func (c *Cache) MarkHit(season string, year int, category string) error {
	_, err := c.db.Exec(
		`UPDATE season_cache SET last_hit=? WHERE season=? AND year=? AND category=?`,
		time.Now().Unix(), season, year, category,
	)
	return err
}

func (c *Cache) PruneStale(staleDays int) (int, error) {
	cutoff := time.Now().Add(-time.Duration(staleDays) * 24 * time.Hour).Unix()
	result, err := c.db.Exec(
		`DELETE FROM season_cache WHERE last_hit < ?`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (c *Cache) NeedsRefresh(currentYear, currentRefreshDays, pastRefreshDays int) ([]Key, error) {
	rows, err := c.db.Query(`SELECT season, year, category, fetched_at FROM season_cache WHERE is_empty = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []Key
	now := time.Now()

	for rows.Next() {
		var key Key
		var fetchedAt int64
		if err := rows.Scan(&key.Season, &key.Year, &key.Category, &fetchedAt); err != nil {
			return nil, err
		}

		ttl := time.Duration(pastRefreshDays) * 24 * time.Hour
		if key.Year == currentYear {
			ttl = time.Duration(currentRefreshDays) * 24 * time.Hour
		}

		if now.Sub(time.Unix(fetchedAt, 0)) > ttl {
			keys = append(keys, key)
		}
	}

	return keys, rows.Err()
}

func (c *Cache) Exists(season string, year int, category string) (bool, error) {
	var count int
	if err := c.db.QueryRow(
		`SELECT COUNT(*) FROM season_cache WHERE season=? AND year=? AND category=?`,
		season, year, category,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (c *Cache) Stats() (Stats, error) {
	stats := Stats{Hits: c.hits.Load(), Misses: c.misses.Load()}
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM season_cache`).Scan(&stats.Entries); err != nil {
		return stats, err
	}
	return stats, nil
}
