package dates

import (
	"fmt"
	"time"

	"github.com/arclighteng/fin-go/internal/models"
)

// Today returns today's date in the given timezone (or local if empty).
func Today(tz string) time.Time {
	return todayIn(time.Now(), loadLocation(tz))
}

// todayIn is the injectable seam behind Today: it derives the civil date of now
// in loc and returns local midnight for that date. Truncate must not be used —
// it snaps to the UTC instant boundary, which is not local midnight outside UTC.
func todayIn(now time.Time, loc *time.Location) time.Time {
	return localMidnight(now, loc)
}

// EpochToDate converts Unix seconds to a date in the given timezone.
func EpochToDate(epoch int64, tz string) time.Time {
	return localMidnight(time.Unix(epoch, 0), loadLocation(tz))
}

// localMidnight returns midnight of t's civil date in loc. Unlike
// Truncate(24*time.Hour), which floors the absolute UTC instant, this snaps to
// the start of the local calendar day.
func localMidnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// PeriodBounds returns [start, endExclusive) for the period containing refDate.
func PeriodBounds(period models.TimePeriod, refDate time.Time) (start, endExclusive time.Time) {
	y, m, _ := refDate.Date()
	loc := refDate.Location()

	switch period {
	case models.PeriodMonth:
		start = time.Date(y, m, 1, 0, 0, 0, 0, loc)
		endExclusive = start.AddDate(0, 1, 0)

	case models.PeriodQuarter:
		q := (int(m) - 1) / 3
		startMonth := time.Month(q*3 + 1)
		start = time.Date(y, startMonth, 1, 0, 0, 0, 0, loc)
		endExclusive = start.AddDate(0, 3, 0)

	case models.PeriodYear:
		start = time.Date(y, 1, 1, 0, 0, 0, 0, loc)
		endExclusive = time.Date(y+1, 1, 1, 0, 0, 0, 0, loc)
	}

	return
}

// ThisMonth returns bounds for the current month.
func ThisMonth(tz string) (time.Time, time.Time) {
	return PeriodBounds(models.PeriodMonth, Today(tz))
}

// LastMonth returns bounds for the previous month.
func LastMonth(tz string) (time.Time, time.Time) {
	t := Today(tz)
	ref := t.AddDate(0, -1, 0)
	return PeriodBounds(models.PeriodMonth, ref)
}

// ThisQuarter returns bounds for the current quarter.
func ThisQuarter(tz string) (time.Time, time.Time) {
	return PeriodBounds(models.PeriodQuarter, Today(tz))
}

// ThisYear returns bounds for the current year.
func ThisYear(tz string) (time.Time, time.Time) {
	return PeriodBounds(models.PeriodYear, Today(tz))
}

// PrevPeriodStart returns the start date of the previous period.
func PrevPeriodStart(period models.TimePeriod, currentStart time.Time) time.Time {
	switch period {
	case models.PeriodMonth:
		return currentStart.AddDate(0, -1, 0)
	case models.PeriodQuarter:
		return currentStart.AddDate(0, -3, 0)
	case models.PeriodYear:
		return currentStart.AddDate(-1, 0, 0)
	}
	return currentStart
}

// PeriodLabel generates a human-readable period label.
func PeriodLabel(period models.TimePeriod, start time.Time) string {
	switch period {
	case models.PeriodMonth:
		return start.Format("Jan 2006")
	case models.PeriodQuarter:
		q := (int(start.Month())-1)/3 + 1
		return fmt.Sprintf("Q%d %d", q, start.Year())
	case models.PeriodYear:
		return fmt.Sprintf("%d", start.Year())
	}
	return ""
}

// IterPeriods iterates backwards through periods, most recent first.
func IterPeriods(period models.TimePeriod, numPeriods int, tz string) []PeriodInfo {
	anchor := Today(tz)
	start, end := PeriodBounds(period, anchor)

	periods := make([]PeriodInfo, 0, numPeriods)
	for i := 0; i < numPeriods; i++ {
		periods = append(periods, PeriodInfo{
			Start:        start,
			EndExclusive: end,
			Label:        PeriodLabel(period, start),
		})
		end = start
		start = PrevPeriodStart(period, start)
	}
	return periods
}

// PeriodInfo holds a period's bounds and label.
type PeriodInfo struct {
	Start        time.Time
	EndExclusive time.Time
	Label        string
}

// DateRangeDays counts the whole calendar days in [start, endExclusive).
func DateRangeDays(start, endExclusive time.Time) int {
	return civilDaysBetween(start, endExclusive)
}

// DaysUntilEndOfMonth returns whole calendar days remaining in the month.
func DaysUntilEndOfMonth(ref time.Time) int {
	_, end := PeriodBounds(models.PeriodMonth, ref)
	return civilDaysBetween(ref, end)
}

// civilDaysBetween counts whole calendar days between the civil dates of start
// and end. It measures the date difference in UTC (which has no DST), so a
// 23-hour or 25-hour DST day still counts as exactly one calendar day. Dividing
// an hour delta directly (end.Sub(start).Hours()/24) is off by one across a DST
// transition.
func civilDaysBetween(start, end time.Time) int {
	ys, ms, ds := start.Date()
	ye, me, de := end.Date()
	su := time.Date(ys, ms, ds, 0, 0, 0, 0, time.UTC)
	eu := time.Date(ye, me, de, 0, 0, 0, 0, time.UTC)
	return int(eu.Sub(su).Hours() / 24)
}

// IsInRange checks if a date falls within [start, endExclusive).
func IsInRange(check, start, endExclusive time.Time) bool {
	return !check.Before(start) && check.Before(endExclusive)
}

func loadLocation(tz string) *time.Location {
	if tz == "" || tz == "UTC" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Local
	}
	return loc
}
