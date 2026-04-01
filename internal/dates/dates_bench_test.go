package dates_test

import (
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/dates"
	"github.com/arclighteng/fin-go/internal/models"
)

var sinkTime time.Time
var sinkPeriods []dates.PeriodInfo

func BenchmarkPeriodBounds(b *testing.B) {
	cases := []struct {
		name    string
		period  models.TimePeriod
		refDate time.Time
	}{
		{"month_jan", models.PeriodMonth, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)},
		{"month_dec", models.PeriodMonth, time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)},
		{"quarter_q1", models.PeriodQuarter, time.Date(2025, 2, 14, 0, 0, 0, 0, time.UTC)},
		{"quarter_q4", models.PeriodQuarter, time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)},
		{"year", models.PeriodYear, time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var start, end time.Time
			for i := 0; i < b.N; i++ {
				start, end = dates.PeriodBounds(tc.period, tc.refDate)
			}
			sinkTime = start
			_ = end
		})
	}
}

func BenchmarkIterPeriods(b *testing.B) {
	cases := []struct {
		name       string
		period     models.TimePeriod
		numPeriods int
	}{
		{"month_12", models.PeriodMonth, 12},
		{"month_24", models.PeriodMonth, 24},
		{"quarter_8", models.PeriodQuarter, 8},
		{"year_5", models.PeriodYear, 5},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var periods []dates.PeriodInfo
			for i := 0; i < b.N; i++ {
				periods = dates.IterPeriods(tc.period, tc.numPeriods, "UTC")
			}
			sinkPeriods = periods
		})
	}
}
