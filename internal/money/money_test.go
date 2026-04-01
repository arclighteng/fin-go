package money_test

import (
	"fmt"
	"testing"

	"github.com/arclighteng/fin-go/internal/money"
)

func TestFormatUSD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cents int64
		want  string
	}{
		{name: "zero", cents: 0, want: "$0.00"},
		{name: "one cent", cents: 1, want: "$0.01"},
		{name: "nine cents", cents: 9, want: "$0.09"},
		{name: "ten cents", cents: 10, want: "$0.10"},
		{name: "one dollar", cents: 100, want: "$1.00"},
		{name: "twelve dollars and ninety-nine cents", cents: 1299, want: "$12.99"},
		{name: "one thousand dollars", cents: 100000, want: "$1,000.00"},
		{name: "large value with commas", cents: 123456789, want: "$1,234,567.89"},
		{name: "negative one cent", cents: -1, want: "-$0.01"},
		{name: "negative one dollar", cents: -100, want: "-$1.00"},
		{name: "negative large value", cents: -123456, want: "-$1,234.56"},
		{name: "single-digit cents on round dollar", cents: 105, want: "$1.05"},
		{name: "999 cents", cents: 999, want: "$9.99"},
		{name: "ten thousand dollars", cents: 1000000, want: "$10,000.00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := money.FormatUSD(tc.cents)
			if got != tc.want {
				t.Errorf("FormatUSD(%d) = %q, want %q", tc.cents, got, tc.want)
			}
		})
	}
}

func TestFormatUSDSigned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cents int64
		want  string
	}{
		{name: "zero has no sign", cents: 0, want: "$0.00"},
		{name: "positive shows plus", cents: 500, want: "+$5.00"},
		{name: "negative shows minus", cents: -500, want: "-$5.00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := money.FormatUSDSigned(tc.cents)
			if got != tc.want {
				t.Errorf("FormatUSDSigned(%d) = %q, want %q", tc.cents, got, tc.want)
			}
		})
	}
}

func TestFormatUSDCompact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cents int64
		want  string
	}{
		{name: "zero whole dollar", cents: 0, want: "$0"},
		{name: "whole dollar no decimals", cents: 500, want: "$5"},
		{name: "non-whole shows decimals", cents: 550, want: "$5.50"},
		{name: "negative whole dollar", cents: -1000, want: "-$10"},
		{name: "negative non-whole", cents: -1050, want: "-$10.50"},
		{name: "large whole dollar with commas", cents: 100000, want: "$1,000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := money.FormatUSDCompact(tc.cents)
			if got != tc.want {
				t.Errorf("FormatUSDCompact(%d) = %q, want %q", tc.cents, got, tc.want)
			}
		})
	}
}

func TestParseToCents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		want    int64
		wantErr bool
	}{
		// String inputs
		{name: "string whole dollar", input: "1", want: 100},
		{name: "string with cents", input: "1.23", want: 123},
		{name: "string with dollar sign removed by caller note: no $ in ParseToCents", input: "1234.56", want: 123456},
		{name: "string with commas", input: "1,234.56", want: 123456},
		{name: "string negative", input: "-100.00", want: -10000},
		{name: "string zero", input: "0", want: 0},
		{name: "string empty", input: "", wantErr: true},
		{name: "string invalid", input: "abc", wantErr: true},
		{name: "string over limit", input: "1000001", wantErr: true},

		// Float64 inputs
		{name: "float64 positive", input: float64(12.34), want: 1234},
		{name: "float64 zero", input: float64(0), want: 0},
		{name: "float64 negative", input: float64(-5.50), want: -550},
		{name: "float64 over limit", input: float64(2000000), wantErr: true},

		// Int inputs
		{name: "int positive", input: int(10), want: 1000},
		{name: "int zero", input: int(0), want: 0},
		{name: "int negative", input: int(-5), want: -500},

		// Int64 inputs
		{name: "int64 positive", input: int64(50), want: 5000},
		{name: "int64 at limit", input: int64(1000000), want: 100000000}, // exactly 1M is allowed (limit is strict >)

		// Unsupported type
		{name: "bool unsupported", input: true, wantErr: true},

		// Rounding
		{name: "round half up positive", input: "1.555", want: 156},
		{name: "round half up negative", input: "-1.555", want: -156},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := money.ParseToCents(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseToCents(%v) expected error but got nil (result=%d)", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseToCents(%v) unexpected error: %v", tc.input, err)
				return
			}
			if got != tc.want {
				t.Errorf("ParseToCents(%v) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseToCentsAllowLarge(t *testing.T) {
	t.Parallel()

	// Should succeed for amounts over $1M that would fail ParseToCents.
	got, err := money.ParseToCentsAllowLarge(float64(2000000))
	if err != nil {
		t.Fatalf("ParseToCentsAllowLarge(2000000) unexpected error: %v", err)
	}
	if got != 200000000 {
		t.Errorf("ParseToCentsAllowLarge(2000000) = %d, want 200000000", got)
	}
}

func TestCentsToDollars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cents int64
		want  float64
	}{
		{cents: 0, want: 0.0},
		{cents: 100, want: 1.0},
		{cents: 123, want: 1.23},
		{cents: -500, want: -5.0},
	}

	for _, tc := range tests {
		name := fmt.Sprintf("cents_%d", tc.cents)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := money.CentsToDollars(tc.cents)
			if got != tc.want {
				t.Errorf("CentsToDollars(%d) = %f, want %f", tc.cents, got, tc.want)
			}
		})
	}
}

func TestMultiplyCents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cents  int64
		factor float64
		want   int64
	}{
		{cents: 1000, factor: 0.5, want: 500},
		{cents: 1000, factor: 1.0, want: 1000},
		{cents: 333, factor: 3.0, want: 999},
		{cents: 100, factor: 0.0, want: 0},
		{cents: 0, factor: 99.9, want: 0},
	}

	for _, tc := range tests {
		name := fmt.Sprintf("cents_%d_factor_%.1f", tc.cents, tc.factor)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := money.MultiplyCents(tc.cents, tc.factor)
			if got != tc.want {
				t.Errorf("MultiplyCents(%d, %f) = %d, want %d", tc.cents, tc.factor, got, tc.want)
			}
		})
	}
}

func TestDivideCents(t *testing.T) {
	t.Parallel()

	t.Run("divide by zero returns error", func(t *testing.T) {
		t.Parallel()
		_, err := money.DivideCents(1000, 0)
		if err == nil {
			t.Error("DivideCents(1000, 0) expected error, got nil")
		}
	})

	t.Run("normal division", func(t *testing.T) {
		t.Parallel()
		got, err := money.DivideCents(1000, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 500 {
			t.Errorf("DivideCents(1000, 2) = %d, want 500", got)
		}
	})
}

func TestCompareWithinThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		a, b        int64
		threshCents int64
		threshPct   float64
		want        bool
	}{
		{name: "exact equal no thresholds", a: 100, b: 100, threshCents: 0, threshPct: 0, want: true},
		{name: "unequal no thresholds", a: 100, b: 101, threshCents: 0, threshPct: 0, want: false},
		{name: "within cents threshold", a: 100, b: 102, threshCents: 5, threshPct: 0, want: true},
		{name: "outside cents threshold", a: 100, b: 110, threshCents: 5, threshPct: 0, want: false},
		{name: "within percent threshold", a: 1000, b: 1050, threshCents: 0, threshPct: 10, want: true},
		{name: "outside percent threshold", a: 1000, b: 1200, threshCents: 0, threshPct: 10, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := money.CompareWithinThreshold(tc.a, tc.b, tc.threshCents, tc.threshPct)
			if got != tc.want {
				t.Errorf("CompareWithinThreshold(%d, %d, %d, %f) = %v, want %v",
					tc.a, tc.b, tc.threshCents, tc.threshPct, got, tc.want)
			}
		})
	}
}
