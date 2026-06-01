package scheduler

import (
	"context"

	"github.com/calmcacil/anilistgen/internal/anilist"
)

// anilistFetcher is the subset of *anilist.Client that the scheduler uses.
// Defining it here lets tests inject a stub that returns canned data
// without hitting the network.
type anilistFetcher interface {
	FetchSeason(ctx context.Context, season string, year int, maxResults int, formats []string) ([]anilist.Show, error)
}

var _ anilistFetcher = (*anilist.Client)(nil)
