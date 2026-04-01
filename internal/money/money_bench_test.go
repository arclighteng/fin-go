package money_test

import (
	"testing"

	"github.com/arclighteng/fin-go/internal/money"
)

var sinkStr string
var sinkInt int64

func BenchmarkFormatUSD(b *testing.B) {
	cases := []struct {
		name  string
		cents int64
	}{
		{"small_positive", 999},
		{"typical", 4299},
		{"large", 1_234_567},
		{"negative", -8750},
		{"zero", 0},
		{"max_like", 99_999_999},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var s string
			for i := 0; i < b.N; i++ {
				s = money.FormatUSD(tc.cents)
			}
			sinkStr = s
		})
	}
}

func BenchmarkParseToCents(b *testing.B) {
	cases := []struct {
		name   string
		amount any
	}{
		{"float_small", float64(9.99)},
		{"float_typical", float64(42.99)},
		{"string_no_comma", "123.45"},
		{"string_with_comma", "1,234.56"},
		{"int", int(100)},
		{"int64", int64(50000)},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var n int64
			for i := 0; i < b.N; i++ {
				n, _ = money.ParseToCents(tc.amount)
			}
			sinkInt = n
		})
	}
}
