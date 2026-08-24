package dates

import (
	"testing"
	"time"
)

// TestTodayIn_LocalMidnightNotUTC guards ADA-110 bug #1: Truncate(24h) snapped to
// UTC midnight, so before ~18:00–19:00 America/Chicago it returned yesterday.
func TestTodayIn_LocalMidnightNotUTC(t *testing.T) {
	t.Parallel()

	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("America/Chicago tzdata unavailable: %v", err)
	}

	// 2026-08-24 10:00 America/Chicago (== 15:00 UTC). Truncate to UTC midnight
	// would land on 2026-08-24 too here, so also pin an early-morning instant
	// where the old code demonstrably returned the previous day.
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, chicago)
	got := todayIn(now, chicago)
	want := time.Date(2026, 8, 24, 0, 0, 0, 0, chicago)
	if !got.Equal(want) {
		t.Errorf("todayIn(2026-08-24 10:00 Chicago) = %v, want %v", got, want)
	}

	// 01:00 local == 06:00 UTC. Old Truncate(24h) → 2026-08-24 00:00 UTC ==
	// 2026-08-23 19:00 Chicago, i.e. yesterday's civil date. Must be the 24th.
	earlyMorning := time.Date(2026, 8, 24, 1, 0, 0, 0, chicago)
	gotEarly := todayIn(earlyMorning, chicago)
	if !gotEarly.Equal(want) {
		t.Errorf("todayIn(2026-08-24 01:00 Chicago) = %v, want %v", gotEarly, want)
	}
	if y, m, d := gotEarly.Date(); y != 2026 || m != time.August || d != 24 {
		t.Errorf("todayIn early-morning civil date = %04d-%02d-%02d, want 2026-08-24", y, m, d)
	}
}
