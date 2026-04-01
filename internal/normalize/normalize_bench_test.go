package normalize_test

import (
	"testing"

	"github.com/arclighteng/fin-go/internal/normalize"
)

var sink string // prevent compiler from eliminating benchmark calls

var merchantCases = []string{
	"STARBUCKS",                            // short, uppercase
	"Amazon.com",                           // mid-length, punctuation
	"Whole Foods Market LLC",               // suffix to strip
	"THE HOME DEPOT #0562 ATLANTA GA",      // long with store number
	"NETFLIX.COM",                          // dot, short
	"CHEVRON 9402   ",                      // trailing whitespace + spaces
	"Bank of America Transfer From Chase",  // transfer pattern
	"Acme Widget Corporation Limited LLC",  // stacked suffixes
}

func BenchmarkNormalizeMerchant(b *testing.B) {
	for _, tc := range merchantCases {
		b.Run(tc[:min(len(tc), 20)], func(b *testing.B) {
			b.ReportAllocs()
			var s string
			for i := 0; i < b.N; i++ {
				s = normalize.NormalizeMerchant(tc)
			}
			sink = s
		})
	}
}

func BenchmarkNormalizeMerchantPair(b *testing.B) {
	cases := []struct {
		name     string
		merchant string
		desc     string
	}{
		{"merchant_wins", "STARBUCKS", "COFFEE PURCHASE"},
		{"desc_fallback", "", "ONLINE PAYMENT TO NETFLIX"},
		{"both_empty", "", ""},
		{"merchant_with_suffix", "Acme Corp LLC", ""},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var s string
			for i := 0; i < b.N; i++ {
				s = normalize.NormalizeMerchantPair(tc.merchant, tc.desc)
			}
			sink = s
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
