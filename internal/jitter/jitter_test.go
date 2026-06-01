package jitter

import (
	"testing"
	"time"
)

func TestJitter_NonPositive(t *testing.T) {
	t.Parallel()

	if got := Jitter(0); got != 0 {
		t.Errorf("Jitter(0) = %v, want 0", got)
	}
	if got := Jitter(-time.Second); got != -time.Second {
		t.Errorf("Jitter(-1s) = %v, want -1s", got)
	}
}

func TestJitter_StaysInBounds(t *testing.T) {
	t.Parallel()

	const base = 10 * time.Minute
	const samples = 1000
	min := base
	max := time.Duration(0)

	for range samples {
		got := Jitter(base)
		if got < min {
			min = got
		}
		if got > max {
			max = got
		}
	}

	lower := base - base/4
	upper := base + base/4
	if min < lower || max > upper {
		t.Errorf("Jitter(%v) out of bounds [%v, %v]: observed [%v, %v] over %d samples", base, lower, upper, min, max, samples)
	}
}

func TestJitter_ZeroForVerySmall(t *testing.T) {
	t.Parallel()

	for range 100 {
		got := Jitter(3 * time.Nanosecond)
		// base/4 rounds to zero, so the offset window is [-0, +0] and the
		// result is always equal to the base.
		if got != 3*time.Nanosecond {
			t.Errorf("Jitter(3ns) = %v, want 3ns", got)
		}
	}
}
