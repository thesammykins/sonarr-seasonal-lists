package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/calmcacil/anilistgen/internal/anilist"
	"github.com/calmcacil/anilistgen/internal/cache"
	"github.com/calmcacil/anilistgen/internal/config"
	"github.com/calmcacil/anilistgen/internal/mapping"
)

// stubFetcher returns canned show data and counts how many times the
// scheduler called it. Optionally sleeps before returning so tests can
// verify parallelisation or singleflight coalescing.
type stubFetcher struct {
	mu     sync.Mutex
	calls  int32
	shows  []anilist.Show
	err    error
	sleep  time.Duration
}

func (s *stubFetcher) FetchSeason(_ context.Context, _ string, _ int, _ int, _ []string) ([]anilist.Show, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.sleep > 0 {
		time.Sleep(s.sleep)
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.shows, nil
}

func (s *stubFetcher) callCount() int32 {
	return atomic.LoadInt32(&s.calls)
}

// resolverWithMappings writes a temp YAML file and returns a Resolver
// that maps the given MAL IDs to TVDB IDs.
func resolverWithMappings(t *testing.T, mappings map[int]int) *mapping.Resolver {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "tvdb-mal.yaml")
	content := "AnimeMap:\n"
	for mal, tvdb := range mappings {
		content += "  - malid: " + itoa(mal) + "\n    tvdbid: " + itoa(tvdb) + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cm, err := mapping.LoadCommunityMapping(path)
	if err != nil {
		t.Fatal(err)
	}
	return mapping.NewResolver(cm)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// newTestScheduler wires a Scheduler with a stub fetcher, an in-memory
// cache, a real resolver, and a minimal config.
func newTestScheduler(t *testing.T, fetcher anilistFetcher, resolver *mapping.Resolver) *Scheduler {
	t.Helper()
	c, err := cache.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cfg := &config.Config{
		PrewarmYears:   []int{2026},
		PrewarmSeasons: []string{"WINTER"},
		MaxPerSeason:   100,
	}
	return &Scheduler{
		cache:    c,
		cfg:      cfg,
		client:   fetcher,
		resolver: resolver,
	}
}

func TestFetchAndStore_ConcurrentCalls_Deduped(t *testing.T) {
	t.Parallel()

	stub := &stubFetcher{sleep: 50 * time.Millisecond, shows: nil}
	s := newTestScheduler(t, stub, nil)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if err := s.FetchAndStore(context.Background(), "WINTER", 2026, "series"); err != nil {
				t.Errorf("FetchAndStore: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := stub.callCount(); got != 1 {
		t.Errorf("fetchSeason called %d times, want 1 (singleflight coalesce)", got)
	}
}

func TestFetchAndStore_SkipsWhenAlreadyCached(t *testing.T) {
	t.Parallel()

	stub := &stubFetcher{shows: nil}
	s := newTestScheduler(t, stub, nil)

	if err := s.cache.Set("WINTER", 2026, "series", []byte(`[]`)); err != nil {
		t.Fatal(err)
	}

	if err := s.FetchAndStore(context.Background(), "WINTER", 2026, "series"); err != nil {
		t.Fatal(err)
	}

	if got := stub.callCount(); got != 0 {
		t.Errorf("fetchSeason called %d times, want 0 (cache hit)", got)
	}
}

func TestRefresh_HappyPath(t *testing.T) {
	t.Parallel()

	mal := 1
	stub := &stubFetcher{shows: []anilist.Show{
		{ID: 100, IDMal: &mal, Format: "TV", Title: anilist.Title{English: strPtr("Test A")}},
		{ID: 200, IDMal: &mal, Format: "MOVIE", Title: anilist.Title{English: strPtr("Filtered out")}},
	}}
	resolver := resolverWithMappings(t, map[int]int{1: 9999})
	s := newTestScheduler(t, stub, resolver)

	s.refresh(context.Background(), "WINTER", 2026, "series")

	data, _, _, ok, err := s.cache.Get("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit after refresh")
	}
	// Only the TV-format show survives the filter, and it resolves to TVDB 9999.
	if got := string(data); got != `[{"tvdbId":9999,"title":"Test A"}]` {
		t.Errorf("cached data = %s", got)
	}
}

func TestRefresh_NoResolver_CachesEmpty(t *testing.T) {
	t.Parallel()

	stub := &stubFetcher{shows: nil}
	s := newTestScheduler(t, stub, nil)

	s.refresh(context.Background(), "WINTER", 2026, "series")

	data, _, _, ok, err := s.cache.Get("WINTER", 2026, "series")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit after refresh")
	}
	if got := string(data); got != `[]` {
		t.Errorf("cached data = %s, want []", got)
	}
}

func TestRefresh_AniListError_LeavesCacheUnchanged(t *testing.T) {
	t.Parallel()

	stub := &stubFetcher{err: errStub}
	s := newTestScheduler(t, stub, nil)

	if err := s.cache.Set("WINTER", 2026, "series", []byte(`[{"tvdbId":1}]`)); err != nil {
		t.Fatal(err)
	}

	s.refresh(context.Background(), "WINTER", 2026, "series")

	data, _, _, ok, err := s.cache.Get("WINTER", 2026, "series")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cache hit (unchanged)")
	}
	if got := string(data); got != `[{"tvdbId":1}]` {
		t.Errorf("cached data changed on error: %s", got)
	}
}

func TestPrewarm_Parallelizes(t *testing.T) {
	t.Parallel()

	stub := &stubFetcher{sleep: 100 * time.Millisecond}
	s := newTestScheduler(t, stub, nil)
	s.cfg.PrewarmYears = []int{2025, 2026}
	s.cfg.PrewarmSeasons = []string{"WINTER", "SPRING"}

	// 2 years * 2 seasons * 2 categories = 8 tasks, each sleeping 100ms.
	// With concurrency=3, total wall time should be roughly 8/3 * 100ms ≈ 300ms,
	// well below the 8*100=800ms a serial loop would take.
	start := time.Now()
	if err := s.Prewarm(context.Background()); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	const serialFloor = 700 * time.Millisecond
	const ceiling = 500 * time.Millisecond
	if elapsed >= serialFloor {
		t.Errorf("Prewarm took %v, expected parallel (< 500ms) — concurrency not working", elapsed)
	}
	if elapsed > ceiling {
		t.Logf("Prewarm took %v (slower than expected, may be CI noise)", elapsed)
	}
}

func TestPrewarm_SkipsExistingEntries(t *testing.T) {
	t.Parallel()

	stub := &stubFetcher{}
	s := newTestScheduler(t, stub, nil)

	if err := s.cache.Set("WINTER", 2026, "series", []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	if err := s.cache.Set("WINTER", 2026, "series-new", []byte(`[]`)); err != nil {
		t.Fatal(err)
	}

	if err := s.Prewarm(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := stub.callCount(); got != 0 {
		t.Errorf("fetchSeason called %d times, want 0 (all entries cached)", got)
	}
}

func strPtr(s string) *string { return &s }

var errStub = stubErr("anilist unavailable")

type stubErr string

func (e stubErr) Error() string { return string(e) }
