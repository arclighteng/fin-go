package dates_test

import (
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/dates"
	"github.com/arclighteng/fin-go/internal/models"
)

// mustDate parses a YYYY-MM-DD string and returns a UTC time.Time.
func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("mustDate(%q): %v", s, err)
	}
	return d
}

func TestEpochToDate(t *testing.T) {
	t.Parallel()

	// 2024-01-15 00:00:00 UTC = 1705276800
	epoch := int64(1705276800)
	got := dates.EpochToDate(epoch, "UTC")

	want := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("EpochToDate(%d, UTC) = %v, want %v", epoch, got, want)
	}
}

func TestPeriodBounds_Month(t *testing.T) {
	t.Parallel()

	ref := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	start, end := dates.PeriodBounds(models.PeriodMonth, ref)

	wantStart := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)

	if !start.Equal(wantStart) {
		t.Errorf("PeriodBounds month start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("PeriodBounds month end = %v, want %v", end, wantEnd)
	}
}

func TestPeriodBounds_MonthBoundary(t *testing.T) {
	t.Parallel()

	// First day of month.
	ref := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	start, end := dates.PeriodBounds(models.PeriodMonth, ref)

	wantStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
}

func TestPeriodBounds_Quarter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ref       time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "Q1 (January)",
			ref:       time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "Q2 (May)",
			ref:       time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "Q3 (September)",
			ref:       time.Date(2024, 9, 30, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "Q4 (December)",
			ref:       time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start, end := dates.PeriodBounds(models.PeriodQuarter, tc.ref)
			if !start.Equal(tc.wantStart) {
				t.Errorf("start = %v, want %v", start, tc.wantStart)
			}
			if !end.Equal(tc.wantEnd) {
				t.Errorf("end = %v, want %v", end, tc.wantEnd)
			}
		})
	}
}

func TestPeriodBounds_Year(t *testing.T) {
	t.Parallel()

	ref := time.Date(2024, 7, 4, 0, 0, 0, 0, time.UTC)
	start, end := dates.PeriodBounds(models.PeriodYear, ref)

	wantStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	if !start.Equal(wantStart) {
		t.Errorf("year start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("year end = %v, want %v", end, wantEnd)
	}
}

func TestPeriodLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		period models.TimePeriod
		start  time.Time
		want   string
	}{
		{
			name:   "month label",
			period: models.PeriodMonth,
			start:  time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			want:   "Mar 2024",
		},
		{
			name:   "Q1 label",
			period: models.PeriodQuarter,
			start:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want:   "Q1 2024",
		},
		{
			name:   "Q3 label",
			period: models.PeriodQuarter,
			start:  time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			want:   "Q3 2024",
		},
		{
			name:   "year label",
			period: models.PeriodYear,
			start:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want:   "2024",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := dates.PeriodLabel(tc.period, tc.start)
			if got != tc.want {
				t.Errorf("PeriodLabel(%v, %v) = %q, want %q", tc.period, tc.start, got, tc.want)
			}
		})
	}
}

func TestPrevPeriodStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		period       models.TimePeriod
		currentStart time.Time
		want         time.Time
	}{
		{
			name:         "previous month",
			period:       models.PeriodMonth,
			currentStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			want:         time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:         "previous month crosses year",
			period:       models.PeriodMonth,
			currentStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want:         time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:         "previous quarter",
			period:       models.PeriodQuarter,
			currentStart: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			want:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:         "previous year",
			period:       models.PeriodYear,
			currentStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want:         time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := dates.PrevPeriodStart(tc.period, tc.currentStart)
			if !got.Equal(tc.want) {
				t.Errorf("PrevPeriodStart(%v, %v) = %v, want %v", tc.period, tc.currentStart, got, tc.want)
			}
		})
	}
}

func TestDateRangeDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  int
	}{
		{
			name:  "one day",
			start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			want:  1,
		},
		{
			name:  "full month January",
			start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			want:  31,
		},
		{
			name:  "full month February leap year",
			start: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			want:  29,
		},
		{
			name:  "full year",
			start: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want:  365,
		},
		{
			name:  "zero days",
			start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := dates.DateRangeDays(tc.start, tc.end)
			if got != tc.want {
				t.Errorf("DateRangeDays = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestIsInRange(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		check time.Time
		want  bool
	}{
		{name: "at start (inclusive)", check: start, want: true},
		{name: "in middle", check: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), want: true},
		{name: "at end (exclusive)", check: end, want: false},
		{name: "before start", check: time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC), want: false},
		{name: "after end", check: time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := dates.IsInRange(tc.check, start, end)
			if got != tc.want {
				t.Errorf("IsInRange(%v) = %v, want %v", tc.check, got, tc.want)
			}
		})
	}
}

func TestIterPeriods(t *testing.T) {
	t.Parallel()

	// IterPeriods is time-dependent, so we just verify structure.
	periods := dates.IterPeriods(models.PeriodMonth, 3, "UTC")
	if len(periods) != 3 {
		t.Fatalf("IterPeriods returned %d periods, want 3", len(periods))
	}

	// Each period's EndExclusive should equal the next period's start (they are contiguous going back).
	for i := 0; i < len(periods)-1; i++ {
		if !periods[i].Start.Equal(periods[i+1].EndExclusive) {
			t.Errorf("Period[%d].Start (%v) != Period[%d].EndExclusive (%v) — gaps or overlaps",
				i, periods[i].Start, i+1, periods[i+1].EndExclusive)
		}
	}

	// Labels must be non-empty.
	for i, p := range periods {
		if p.Label == "" {
			t.Errorf("Period[%d] has empty label", i)
		}
	}
}

func TestDaysUntilEndOfMonth(t *testing.T) {
	t.Parallel()

	// January 1 at 00:00 UTC: end of month is February 1 at 00:00 UTC.
	// Difference = 31 full days.
	ref := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	got := dates.DaysUntilEndOfMonth(ref)
	if got != 31 {
		t.Errorf("DaysUntilEndOfMonth(Jan 1) = %d, want 31", got)
	}

	// January 31 at 00:00 UTC: end of month is February 1 at 00:00 UTC.
	// Difference = 1 day.
	ref2 := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	got2 := dates.DaysUntilEndOfMonth(ref2)
	if got2 != 1 {
		t.Errorf("DaysUntilEndOfMonth(Jan 31) = %d, want 1", got2)
	}
}

// TestDaysUntilEndOfMonth_SpringForward guards ADA-110 bug #2: March in
// America/Chicago contains a 23-hour DST day, so end.Sub(ref).Hours()/24
// truncated to 30. Whole-calendar-day counting must return 31.
func TestDaysUntilEndOfMonth_SpringForward(t *testing.T) {
	t.Parallel()

	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("America/Chicago tzdata unavailable: %v", err)
	}

	ref := time.Date(2026, 3, 1, 0, 0, 0, 0, chicago)
	got := dates.DaysUntilEndOfMonth(ref)
	if got != 31 {
		t.Errorf("DaysUntilEndOfMonth(Mar 1 Chicago) = %d, want 31", got)
	}
}

// TestDateRangeDays_SpringForward guards the same off-by-one in DateRangeDays
// across a spring-forward month in America/Chicago.
func TestDateRangeDays_SpringForward(t *testing.T) {
	t.Parallel()

	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("America/Chicago tzdata unavailable: %v", err)
	}

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, chicago)
	end := time.Date(2026, 4, 1, 0, 0, 0, 0, chicago)
	got := dates.DateRangeDays(start, end)
	if got != 31 {
		t.Errorf("DateRangeDays(Mar 1..Apr 1 Chicago) = %d, want 31", got)
	}
}

func TestToday_ReturnsDateWithoutTime(t *testing.T) {
	t.Parallel()

	today := dates.Today("UTC")
	// Today should have no time component (truncated to day).
	if today.Hour() != 0 || today.Minute() != 0 || today.Second() != 0 || today.Nanosecond() != 0 {
		t.Errorf("Today() has time component: %v", today)
	}
}

func TestToday_UnknownTZFallsBack(t *testing.T) {
	t.Parallel()

	// An unknown timezone should not panic; it falls back to Local.
	got := dates.Today("Invalid/Timezone")
	if got.IsZero() {
		t.Error("Today with invalid TZ returned zero time")
	}
}
