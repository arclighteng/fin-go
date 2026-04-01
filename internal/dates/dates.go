package dates

import (
	"fmt"
	"time"

	"github.com/arclighteng/fin-go/internal/models"
)

// Today returns today's date in the given timezone (or local if empty).
func Today(tz string) time.Time {
	loc := loadLocation(tz)
	return time.Now().In(loc).Truncate(24 * time.Hour)
}

// EpochToDate converts Unix seconds to a date in the given timezone.
func EpochToDate(epoch int64, tz string) time.Time {
	loc := loadLocation(tz)
	return time.Unix(epoch, 0).In(loc).Truncate(24 * time.Hour)
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

// DateRangeDays counts the days in a range.
func DateRangeDays(start, endExclusive time.Time) int {
	return int(endExclusive.Sub(start).Hours() / 24)
}

// DaysUntilEndOfMonth returns days remaining in the month.
func DaysUntilEndOfMonth(ref time.Time) int {
	_, end := PeriodBounds(models.PeriodMonth, ref)
	return int(end.Sub(ref).Hours() / 24)
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
