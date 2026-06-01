package cache

import (
	"testing"
	"time"
)

func TestOpenAndClose(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestGetMiss(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	data, fresh, isPending, ok, err := c.Get("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected miss")
	}
	if data != nil {
		t.Error("expected nil data on miss")
	}
	if fresh {
		t.Error("expected not fresh on miss")
	}
	if isPending {
		t.Error("expected not pending on miss")
	}
}

func TestGet_PendingEntryWithinTimeout_ReturnsPending(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.SetEmpty("WINTER", 2026, "series"); err != nil {
		t.Fatalf("SetEmpty: %v", err)
	}

	_, fresh, isPending, ok, err := c.Get("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit for fresh pending entry")
	}
	if isPending != true {
		t.Error("expected isPending true for fresh pending entry")
	}
	if fresh {
		t.Error("expected not fresh for pending entry")
	}
}

func TestGet_PendingEntryOlderThanTimeout_EvictsAndReturnsMiss(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.SetEmpty("WINTER", 2026, "series"); err != nil {
		t.Fatalf("SetEmpty: %v", err)
	}

	// Backdate fetched_at past the recovery timeout.
	old := time.Now().Add(-2 * DefaultPendingTimeout).Unix()
	if _, err := c.db.Exec(
		`UPDATE season_cache SET fetched_at=? WHERE season=? AND year=? AND category=?`,
		old, "WINTER", 2026, "series",
	); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	_, _, isPending, ok, err := c.Get("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected miss for stuck pending entry")
	}
	if isPending {
		t.Error("expected isPending false for evicted entry")
	}

	// Row should be gone so the next FetchAndStore kicks off a fresh refresh.
	exists, err := c.Exists("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("expected stuck pending row to be evicted")
	}
}

func TestSetEmptyAndGet(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.SetEmpty("WINTER", 2026, "series"); err != nil {
		t.Fatalf("SetEmpty: %v", err)
	}

	data, fresh, isPending, ok, err := c.Get("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit after SetEmpty")
	}
	if data != nil {
		t.Error("expected nil data for pending entry")
	}
	if fresh {
		t.Error("expected not fresh for pending entry")
	}
	if !isPending {
		t.Error("expected isPending true")
	}
}

func TestSetAndGet(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	showData := []byte(`[{"tvdbId":12345,"title":"Test Show"}]`)
	if err := c.Set("SPRING", 2026, "series", showData); err != nil {
		t.Fatalf("Set: %v", err)
	}

	data, fresh, isPending, ok, err := c.Get("SPRING", 2026, "series")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit after Set")
	}
	if string(data) != string(showData) {
		t.Errorf("data = %s, want %s", data, showData)
	}
	if !fresh {
		t.Error("expected fresh")
	}
	if isPending {
		t.Error("expected not pending")
	}
}

func TestSetOverwritesEmpty(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c.SetEmpty("WINTER", 2026, "series")

	showData := []byte(`[{"tvdbId":99999,"title":"Real Show"}]`)
	c.Set("WINTER", 2026, "series", showData)

	data, fresh, isPending, ok, err := c.Get("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected hit")
	}
	if string(data) != string(showData) {
		t.Errorf("data = %s, want %s", data, showData)
	}
	if !fresh {
		t.Error("expected fresh after overwrite")
	}
	if isPending {
		t.Error("expected not pending after overwrite")
	}
}

func TestPruneStale(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c.Set("WINTER", 2020, "series", []byte(`[]`))
	c.Set("SPRING", 2020, "series", []byte(`[]`))

	n, err := c.PruneStale(365)
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 pruned from fresh entries, got %d", n)
	}

	// Manually set last_hit far in the past to test pruning
	c.db.Exec(`UPDATE season_cache SET last_hit = 0`)
	n, err = c.PruneStale(1)
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 pruned with stale entries, got %d", n)
	}
}

func TestNeedsRefresh(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c.Set("WINTER", 2020, "series", []byte(`[]`))
	c.Set("SPRING", 2026, "series", []byte(`[]`))

	// Entries just created should NOT need refresh
	keys, err := c.NeedsRefresh(2026, 7, 30)
	if err != nil {
		t.Fatalf("NeedsRefresh: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 stale entries, got %d", len(keys))
	}
}

func TestExists(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	exists, err := c.Exists("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("expected false before Set")
	}

	c.SetEmpty("WINTER", 2026, "series")

	exists, err = c.Exists("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("expected true after SetEmpty")
	}
}

func TestExists_DbClosed_ReturnsError(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = c.Exists("WINTER", 2026, "series")
	if err == nil {
		t.Error("expected error from closed DB")
	}
}

func TestStats(t *testing.T) {
	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	c.Set("WINTER", 2026, "series", []byte(`[{"tvdbId":1}]`))
	c.Set("SPRING", 2026, "series", []byte(`[{"tvdbId":2}]`))
	c.Get("WINTER", 2026, "series")

	stats, err := c.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Entries != 2 {
		t.Errorf("entries = %d, want 2", stats.Entries)
	}
}

func TestStats_DbClosed_ReturnsError(t *testing.T) {
	t.Parallel()

	c, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = c.Stats()
	if err == nil {
		t.Error("expected error from closed DB")
	}
}
