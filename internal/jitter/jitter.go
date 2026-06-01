// Package jitter provides a small helper for breaking synchronisation
// between concurrent goroutines or replicas that would otherwise fire
// on the same wall-clock tick.
package jitter

import (
	"math/rand/v2"
	"time"
)

// Jitter returns d randomly varied by ±25% to prevent synchronized retry
// storms. Zero or negative durations are returned unchanged.
func Jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	quarter := d / 4
	offset := time.Duration(rand.Int64N(int64(2*quarter + 1))) - quarter
	return d + offset
}
