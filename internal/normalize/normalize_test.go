package normalize_test

import (
	"testing"

	"github.com/arclighteng/fin-go/internal/normalize"
)

func TestNormalizeMerchant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Basic cases
		{name: "empty string", input: "", want: ""},
		{name: "whitespace only", input: "   ", want: ""},
		{name: "basic lowercase", input: "AMAZON", want: "amazon"},
		{name: "trims surrounding whitespace", input: "  Target  ", want: "target"},
		{name: "mixed case", input: "Whole Foods Market", want: "whole foods market"},

		// Ampersand conversion
		{name: "ampersand to and", input: "Bed & Bath", want: "bed and bath"},
		{name: "ampersand no spaces", input: "AT&T", want: "atandt"},

		// Noise punctuation removal
		{name: "period removed", input: "Amazon.com", want: "amazoncom"},
		{name: "comma removed", input: "Smith, Jones", want: "smith jones"},
		{name: "hash removed", input: "Store #42", want: "store 42"},
		{name: "multiple punctuation", input: "A.B.C., Inc.", want: "abc"},

		// Legal suffix stripping - single
		{name: "strip LLC", input: "Acme LLC", want: "acme"},
		{name: "strip Inc", input: "Acme Inc", want: "acme"},
		{name: "strip Corp", input: "Acme Corp", want: "acme"},
		{name: "strip Ltd", input: "Acme Ltd", want: "acme"},
		{name: "strip Co", input: "Acme Co", want: "acme"},
		{name: "strip Company", input: "Acme Company", want: "acme"},
		{name: "strip Corporation", input: "Acme Corporation", want: "acme"},
		{name: "strip Incorporated", input: "Acme Incorporated", want: "acme"},
		{name: "strip Limited", input: "Acme Limited", want: "acme"},
		{name: "strip PLC", input: "Acme PLC", want: "acme"},
		{name: "strip LP", input: "Acme LP", want: "acme"},
		{name: "strip LLP", input: "Acme LLP", want: "acme"},
		{name: "strip NA", input: "Bank NA", want: "bank"},
		{name: "strip DBA", input: "Store DBA", want: "store"},

		// Legal suffix stripping - stacked
		{name: "stacked Corp LLC", input: "Acme Corp LLC", want: "acme"},
		{name: "stacked Inc Corp", input: "Acme Inc Corp", want: "acme"}, // iterative: "corp" stripped first, then "inc"
		{name: "Ltd Co stacked", input: "FooBar Ltd Co", want: "foobar"},

		// Multi-word suffixes
		{name: "strip limited liability company", input: "Acme Limited Liability Company", want: "acme"},
		{name: "strip limited liability co", input: "Acme Limited Liability Co", want: "acme"},
		{name: "strip limited partnership", input: "Acme Limited Partnership", want: "acme"},

		// No false stripping (suffix as substring, not word)
		{name: "llc in middle not stripped", input: "allcost", want: "allcost"},
		{name: "name with no suffix", input: "starbucks", want: "starbucks"},

		// Internal whitespace collapse
		{name: "internal double space", input: "Foo  Bar", want: "foo bar"},
		{name: "tabs and spaces", input: "Foo\t Bar", want: "foo bar"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalize.NormalizeMerchant(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeMerchant(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeMerchantPair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		merchant    string
		description string
		want        string
	}{
		{name: "merchant takes priority", merchant: "Amazon", description: "AMZN*Purchase", want: "amazon"},
		{name: "falls back to description when merchant empty", merchant: "", description: "NETFLIX.COM", want: "netflixcom"},
		{name: "falls back to description when merchant is whitespace", merchant: "   ", description: "Spotify", want: "spotify"},
		{name: "both empty returns empty", merchant: "", description: "", want: ""},
		{name: "both whitespace returns empty", merchant: "  ", description: "  ", want: ""},
		{name: "merchant with suffix, no description needed", merchant: "Walmart Inc", description: "WMT #1234", want: "walmart"},
		{name: "description used when merchant missing", merchant: "", description: "Target Corp", want: "target"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalize.NormalizeMerchantPair(tc.merchant, tc.description)
			if got != tc.want {
				t.Errorf("NormalizeMerchantPair(%q, %q) = %q, want %q", tc.merchant, tc.description, got, tc.want)
			}
		})
	}
}

func TestMerchantNormExpr(t *testing.T) {
	t.Parallel()
	// Just verify the constant is non-empty and contains expected SQL fragments.
	expr := normalize.MerchantNormExpr
	if expr == "" {
		t.Fatal("MerchantNormExpr must not be empty")
	}
	for _, fragment := range []string{"TRIM", "LOWER", "COALESCE", "merchant", "description"} {
		found := false
		for i := 0; i <= len(expr)-len(fragment); i++ {
			if expr[i:i+len(fragment)] == fragment {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("MerchantNormExpr does not contain expected fragment %q", fragment)
		}
	}
}
