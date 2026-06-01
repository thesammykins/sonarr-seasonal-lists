package filter

import (
	"testing"

	"github.com/calmcacil/anilistgen/internal/anilist"
)

func makePtr[T any](v T) *T {
	return &v
}

func TestFilter_SkipsShortDuration(t *testing.T) {
	t.Parallel()

	shows := []anilist.Show{
		{Duration: makePtr(24), Episodes: makePtr(12)},
		{Duration: makePtr(6), Episodes: makePtr(1)},
		{Duration: makePtr(10), Episodes: makePtr(1)},
	}

	result := Filter(shows, Config{})
	if len(result) != 1 {
		t.Fatalf("expected 1 show after filter, got %d", len(result))
	}
	if *result[0].Duration != 24 {
		t.Errorf("expected remaining show to have duration 24, got %d", *result[0].Duration)
	}
}

func TestFilter_BlacklistByMALID(t *testing.T) {
	t.Parallel()

	shows := []anilist.Show{
		{IDMal: makePtr(16498), Title: anilist.Title{English: makePtr("Good Show")}},
		{IDMal: makePtr(99999), Title: anilist.Title{English: makePtr("Bad Show")}},
	}

	result := Filter(shows, Config{Blacklist: []string{"99999"}})
	if len(result) != 1 {
		t.Fatalf("expected 1 show, got %d", len(result))
	}
	if *result[0].IDMal != 16498 {
		t.Errorf("expected MAL 16498 to remain, got %d", *result[0].IDMal)
	}
}

func TestFilter_BlacklistByTitle(t *testing.T) {
	t.Parallel()

	shows := []anilist.Show{
		{Title: anilist.Title{English: makePtr("One Piece")}},
		{Title: anilist.Title{English: makePtr("Naruto")}},
	}

	result := Filter(shows, Config{Blacklist: []string{"One Piece"}})
	if len(result) != 1 {
		t.Fatalf("expected 1 show, got %d", len(result))
	}
}

func TestFilter_ExcludeTags(t *testing.T) {
	t.Parallel()

	shows := []anilist.Show{
		{Tags: []anilist.Tag{{Name: "Action"}}},
		{Tags: []anilist.Tag{{Name: "Hentai"}}},
		{Tags: []anilist.Tag{{Name: "Comedy"}}},
	}

	result := Filter(shows, Config{ExcludeTags: []string{"Hentai"}})
	if len(result) != 2 {
		t.Fatalf("expected 2 shows, got %d", len(result))
	}
}

func TestFilter_ExcludeTagsCaseInsensitive(t *testing.T) {
	t.Parallel()

	shows := []anilist.Show{
		{Tags: []anilist.Tag{{Name: "HENTAI"}}},
	}

	result := Filter(shows, Config{ExcludeTags: []string{"hentai"}})
	if len(result) != 0 {
		t.Error("expected show to be excluded case-insensitively")
	}
}

func TestFilterFuture_RemovesFutureShows(t *testing.T) {
	t.Parallel()

	year := 2099
	shows := []anilist.Show{
		{StartDate: anilist.FuzzyDate{Year: &year, Month: makePtr(12)}},
		{StartDate: anilist.FuzzyDate{Year: makePtr(2020), Month: makePtr(1)}},
	}

	result := Future(shows, 3)
	if len(result) != 1 {
		t.Fatalf("expected 1 show within range, got %d", len(result))
	}
}

func TestFilterFuture_NoLimit(t *testing.T) {
	t.Parallel()

	shows := []anilist.Show{
		{StartDate: anilist.FuzzyDate{Year: makePtr(2099), Month: makePtr(12)}},
	}

	result := Future(shows, 0)
	if len(result) != 1 {
		t.Errorf("expected 1 show when months=0, got %d", len(result))
	}
}

func TestIsBlacklisted(t *testing.T) {
	t.Parallel()

	if !isBlacklisted("One Piece", 0, []string{"One Piece"}) {
		t.Error("expected title match")
	}
	if !isBlacklisted("anything", 16498, []string{"16498"}) {
		t.Error("expected MAL ID match")
	}
	if isBlacklisted("Naruto", 0, []string{"One Piece"}) {
		t.Error("expected no match")
	}
	if isBlacklisted("anything", 0, []string{"", "16498"}) {
		t.Error("empty entry should be skipped")
	}
}

func TestHasExcludedTag(t *testing.T) {
	t.Parallel()

	show := anilist.Show{Tags: []anilist.Tag{{Name: "Action"}, {Name: "Hentai"}}}
	if !hasExcludedTag(show, []string{"Hentai"}) {
		t.Error("expected hentai tag to match")
	}
	if hasExcludedTag(show, []string{"Guro"}) {
		t.Error("expected guro tag not to match")
	}
	if !hasExcludedTag(show, []string{"", "Hentai"}) {
		t.Error("empty entry should not prevent matching valid entries")
	}
}
