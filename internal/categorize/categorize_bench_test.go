package categorize_test

import (
	"testing"

	"github.com/arclighteng/fin-go/internal/categorize"
)

var sinkCatID string
var sinkConf float64

// knownMerchants are merchants expected to match at least one rule.
var knownMerchants = []struct {
	merchantNorm string
	description  string
}{
	{"starbucks coffee", "starbucks purchase"},
	{"netflix", "netflix monthly subscription"},
	{"whole foods market", "grocery purchase"},
	{"uber eats", "food delivery"},
	{"shell gas station", "fuel purchase"},
	{"cvs pharmacy", "prescription pickup"},
	{"amazon", "online shopping"},
	{"geico insurance", "monthly premium"},
	{"direct deposit payroll", "salary"},
	{"chase credit card payment", "autopay"},
}

// unknownMerchants produce no rule match.
var unknownMerchants = []struct {
	merchantNorm string
	description  string
}{
	{"acme widget supply", "b2b invoice"},
	{"xyz123 holdings", "misc payment"},
	{"local hardware shop", "tools"},
	{"", ""},
}

func BenchmarkCategorize(b *testing.B) {
	for _, tc := range knownMerchants {
		b.Run(tc.merchantNorm, func(b *testing.B) {
			b.ReportAllocs()
			var catID string
			var conf float64
			for i := 0; i < b.N; i++ {
				catID, conf = categorize.CategorizeMerchant(tc.merchantNorm, tc.description)
			}
			sinkCatID = catID
			sinkConf = conf
		})
	}
}

func BenchmarkCategorizeUnknown(b *testing.B) {
	for _, tc := range unknownMerchants {
		label := tc.merchantNorm
		if label == "" {
			label = "empty"
		}
		b.Run(label, func(b *testing.B) {
			b.ReportAllocs()
			var catID string
			var conf float64
			for i := 0; i < b.N; i++ {
				catID, conf = categorize.CategorizeMerchant(tc.merchantNorm, tc.description)
			}
			sinkCatID = catID
			sinkConf = conf
		})
	}
}
