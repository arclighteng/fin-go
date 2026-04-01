package server

import (
	"testing"
)

// ---------------------------------------------------------------------------
// formatUSD
// ---------------------------------------------------------------------------

func TestFormatUSD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		cents int64
		want  string
	}{
		{"zero", 0, "$0.00"},
		{"one cent", 1, "$0.01"},
		{"one dollar", 100, "$1.00"},
		{"ten dollars", 1000, "$10.00"},
		{"one hundred dollars", 10000, "$100.00"},
		{"one thousand dollars", 100000, "$1,000.00"},
		{"ten thousand dollars", 1000000, "$10,000.00"},
		{"one hundred thousand dollars", 10000000, "$100,000.00"},
		{"one million dollars", 100000000, "$1,000,000.00"},
		{"with cents", 123456, "$1,234.56"},
		{"exact dollars and cents", 100099, "$1,000.99"},
		{"negative one dollar", -100, "-$1.00"},
		{"negative with commas", -100000, "-$1,000.00"},
		{"negative zero fractions", -50, "-$0.50"},
		{"large negative", -123456789, "-$1,234,567.89"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatUSD(tc.cents)
			if got != tc.want {
				t.Errorf("formatUSD(%d): want %q, got %q", tc.cents, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatDate
// ---------------------------------------------------------------------------

func TestFormatDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		iso  string
		want string
	}{
		{"standard date", "2025-03-15", "Mar 15, 2025"},
		{"jan first", "2025-01-01", "Jan 1, 2025"},
		{"december", "2024-12-31", "Dec 31, 2024"},
		{"february", "2024-02-28", "Feb 28, 2024"},
		{"date with time suffix", "2025-03-15T10:30:00Z", "Mar 15, 2025"},
		{"short string passthrough", "2025", "2025"},
		{"empty string passthrough", "", ""},
		{"invalid month passthrough", "2025-13-01", "2025-13-01"},
		{"invalid format passthrough", "not-a-date", "not-a-date"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatDate(tc.iso)
			if got != tc.want {
				t.Errorf("formatDate(%q): want %q, got %q", tc.iso, tc.want, got)
			}
		})
	}
}

// TestFormatDateAllMonths verifies all 12 month abbreviations.
func TestFormatDateAllMonths(t *testing.T) {
	t.Parallel()
	months := []struct {
		num  string
		abbr string
	}{
		{"01", "Jan"}, {"02", "Feb"}, {"03", "Mar"}, {"04", "Apr"},
		{"05", "May"}, {"06", "Jun"}, {"07", "Jul"}, {"08", "Aug"},
		{"09", "Sep"}, {"10", "Oct"}, {"11", "Nov"}, {"12", "Dec"},
	}
	for _, m := range months {
		iso := "2025-" + m.num + "-15"
		want := m.abbr + " 15, 2025"
		got := formatDate(iso)
		if got != want {
			t.Errorf("formatDate(%q): want %q, got %q", iso, want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// formatDateTime
// ---------------------------------------------------------------------------

func TestFormatDateTime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		iso  string
		want string
	}{
		{"with T separator", "2025-03-15T10:30:00Z", "2025-03-15 10:30"},
		{"with space separator", "2025-03-15 10:30:00", "2025-03-15 10:30"},
		{"exact 16 chars", "2025-03-15 10:30", "2025-03-15 10:30"},
		{"too short passthrough", "2025-03-1", "2025-03-1"},
		{"empty passthrough", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatDateTime(tc.iso)
			if got != tc.want {
				t.Errorf("formatDateTime(%q): want %q, got %q", tc.iso, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// titleCase
// ---------------------------------------------------------------------------

func TestTitleCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain lowercase", "dining", "Dining"},
		{"snake case", "debt_payment", "Debt Payment"},
		{"multi word", "variable_essentials_cents", "Variable Essentials Cents"},
		{"already spaced", "food and dining", "Food And Dining"},
		{"empty", "", ""},
		{"single char", "a", "A"},
		{"all underscores", "one_two_three", "One Two Three"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := titleCase(tc.input)
			if got != tc.want {
				t.Errorf("titleCase(%q): want %q, got %q", tc.input, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// appendAccountFilter
// ---------------------------------------------------------------------------

func TestAppendAccountFilter(t *testing.T) {
	t.Parallel()
	t.Run("EmptyFilter", func(t *testing.T) {
		t.Parallel()
		q := "SELECT * FROM transactions WHERE 1=1"
		args := []any{"2025-01-01", "2025-02-01"}

		gotQ, gotArgs := appendAccountFilter(q, args, nil)

		if gotQ != q {
			t.Errorf("query should be unchanged for empty filter: got %q", gotQ)
		}
		if len(gotArgs) != len(args) {
			t.Errorf("args should be unchanged: want %d, got %d", len(args), len(gotArgs))
		}
	})

	t.Run("EmptySlice", func(t *testing.T) {
		t.Parallel()
		q := "SELECT * FROM t WHERE 1=1"
		args := []any{}
		gotQ, gotArgs := appendAccountFilter(q, args, []string{})
		if gotQ != q {
			t.Errorf("empty slice: query should be unchanged")
		}
		if len(gotArgs) != 0 {
			t.Errorf("empty slice: args should be unchanged")
		}
	})

	t.Run("SingleAccount", func(t *testing.T) {
		t.Parallel()
		q := "SELECT * FROM t WHERE 1=1"
		args := []any{}
		gotQ, gotArgs := appendAccountFilter(q, args, []string{"acct-1"})

		expectedSuffix := " AND account_id IN (?)"
		if len(gotQ) < len(q)+len(expectedSuffix) {
			t.Errorf("query not extended for single account: %q", gotQ)
		}
		if len(gotArgs) != 1 {
			t.Errorf("args: want 1, got %d", len(gotArgs))
		}
		if gotArgs[0] != "acct-1" {
			t.Errorf("args[0]: want %q, got %v", "acct-1", gotArgs[0])
		}
	})

	t.Run("MultipleAccounts", func(t *testing.T) {
		t.Parallel()
		q := "SELECT * FROM t WHERE 1=1"
		args := []any{"start"}
		filter := []string{"acct-1", "acct-2", "acct-3"}

		gotQ, gotArgs := appendAccountFilter(q, args, filter)

		// Should have 1 original arg + 3 account IDs.
		if len(gotArgs) != 4 {
			t.Errorf("args: want 4, got %d", len(gotArgs))
		}
		// Each placeholder should appear.
		for _, id := range filter {
			found := false
			for _, a := range gotArgs {
				if a == id {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("account %q not found in args", id)
			}
		}
		// Query should contain IN clause.
		if gotQ == q {
			t.Error("query was not modified for multiple accounts")
		}
	})
}

// ---------------------------------------------------------------------------
// computeSavingsTier
// ---------------------------------------------------------------------------

func TestComputeSavingsTier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pct  float64
		want string
	}{
		{"exactly 30", 30.0, "wealth-building"},
		{"above 30", 45.0, "wealth-building"},
		{"exactly 20", 20.0, "progress"},
		{"between 20 and 30", 25.0, "progress"},
		{"just below 30", 29.9, "progress"},
		{"exactly 0", 0.0, "survival"},
		{"between 0 and 20", 15.0, "survival"},
		{"just above 0", 0.1, "survival"},
		{"negative", -5.0, "negative"},
		{"large negative", -100.0, "negative"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := computeSavingsTier(tc.pct)
			if got != tc.want {
				t.Errorf("computeSavingsTier(%.1f): want %q, got %q", tc.pct, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatIntCommas
// ---------------------------------------------------------------------------

func TestFormatIntCommas(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1,000"},
		{9999, "9,999"},
		{10000, "10,000"},
		{100000, "100,000"},
		{1000000, "1,000,000"},
		{1234567, "1,234,567"},
		{123456789, "123,456,789"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got := formatIntCommas(tc.n)
			if got != tc.want {
				t.Errorf("formatIntCommas(%d): want %q, got %q", tc.n, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatPercent
// ---------------------------------------------------------------------------

func TestFormatPercent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		v    float64
		want string
	}{
		{0.0, "0"},
		{50.0, "50"},
		{99.9, "100"},
		{33.4, "33"},
		{33.5, "34"},
		{-10.0, "-10"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			got := formatPercent(tc.v)
			if got != tc.want {
				t.Errorf("formatPercent(%.1f): want %q, got %q", tc.v, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// absInt
// ---------------------------------------------------------------------------

func TestAbsInt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int64
		want int64
	}{
		{0, 0},
		{5, 5},
		{-5, 5},
		{-1000000, 1000000},
		{1000000, 1000000},
	}

	for _, tc := range tests {
		got := absInt(tc.n)
		if got != tc.want {
			t.Errorf("absInt(%d): want %d, got %d", tc.n, tc.want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// monthPeriodLabel
// ---------------------------------------------------------------------------

func TestMonthPeriodLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		isoDate string
		want    string
	}{
		{"2025-03-01", "March 2025"},
		{"2025-01-01", "January 2025"},
		{"2024-12-15", "December 2024"},
		{"2025-06-01", "June 2025"},
		{"short", "short"},  // too short
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.isoDate, func(t *testing.T) {
			t.Parallel()
			got := monthPeriodLabel(tc.isoDate)
			if got != tc.want {
				t.Errorf("monthPeriodLabel(%q): want %q, got %q", tc.isoDate, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DashboardData.IsAccountSelected
// ---------------------------------------------------------------------------

func TestIsAccountSelected(t *testing.T) {
	t.Parallel()
	t.Run("NoSelection_AllSelected", func(t *testing.T) {
		t.Parallel()
		d := &DashboardData{}
		if !d.IsAccountSelected("any-account") {
			t.Error("want true when no accounts selected")
		}
	})

	t.Run("ExplicitMatch", func(t *testing.T) {
		t.Parallel()
		d := &DashboardData{SelectedAccounts: []string{"acct-1", "acct-2"}}
		if !d.IsAccountSelected("acct-1") {
			t.Error("want true for acct-1 in selection")
		}
		if !d.IsAccountSelected("acct-2") {
			t.Error("want true for acct-2 in selection")
		}
	})

	t.Run("ExplicitNonMatch", func(t *testing.T) {
		t.Parallel()
		d := &DashboardData{SelectedAccounts: []string{"acct-1"}}
		if d.IsAccountSelected("acct-99") {
			t.Error("want false for acct-99 not in selection")
		}
	})
}
