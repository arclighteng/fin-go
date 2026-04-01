// Package normalize provides merchant name normalization for transaction grouping.
//
// The canonical SQL expression (MerchantNormExpr) is used at query time;
// NormalizeMerchant applies the same logic in Go when working with in-memory data.
package normalize

import (
	"regexp"
	"strings"
)

// MerchantNormExpr is the canonical SQLite expression that computes a normalized
// merchant name from a transactions row. Use this in all queries instead of
// recomputing in application code.
//
// It resolves the display name by preferring merchant over description, falls back
// to an empty string, then trims whitespace and lowercases the result.
const MerchantNormExpr = "TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), '')))"

// suffixesToStrip is the ordered set of legal/corporate suffixes removed during
// normalization. Longer multi-word suffixes are listed first so they are stripped
// before their constituent single words.
var suffixesToStrip = []string{
	// Multi-word first
	"limited liability company",
	"limited liability co",
	"limited partnership",
	"limited liability",
	// Single-word abbreviations and full forms
	"llc",
	"inc",
	"incorporated",
	"corp",
	"corporation",
	"ltd",
	"limited",
	"co",
	"company",
	"plc",
	"lp",
	"llp",
	"na",  // "N.A." as in "Bank N.A."
	"dba", // "doing business as"
}

// reMultiSpace matches runs of two or more whitespace characters.
var reMultiSpace = regexp.MustCompile(`\s{2,}`)

// rePunctuation matches punctuation characters that are noise in merchant names:
// periods, commas, hash signs, and ampersands (which are normalized to "and").
var rePunctSingleChar = regexp.MustCompile(`[.,#]`)

// NormalizeMerchant cleans a raw merchant or description string for consistent
// grouping across transactions. The result is suitable for use as a map key or
// for display in grouped views.
//
// Steps applied in order:
//  1. Trim surrounding whitespace and lowercase.
//  2. Strip trailing legal suffixes (LLC, Inc, Corp, Ltd, etc.).
//  3. Collapse internal whitespace runs to a single space.
//  4. Final trim.
//
// An empty or whitespace-only input returns an empty string.
func NormalizeMerchant(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Lowercase for all comparisons and storage.
	s = strings.ToLower(s)

	// Replace ampersands with "and" for consistent grouping
	// (e.g. "Bed & Bath" and "Bed and Bath" group together).
	s = strings.ReplaceAll(s, "&", "and")

	// Remove noisy punctuation (periods, commas, hash signs).
	s = rePunctSingleChar.ReplaceAllString(s, "")

	// Strip trailing legal suffixes, iterating until no further stripping occurs.
	// Multiple suffixes can stack (e.g. "Acme Corp. LLC" → "acme corp llc" → "acme corp" → "acme").
	for {
		stripped := stripTrailingSuffix(s, suffixesToStrip)
		if stripped == s {
			break
		}
		s = stripped
	}

	// Collapse internal whitespace and do a final trim.
	s = reMultiSpace.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	return s
}

// stripTrailingSuffix removes one instance of the first matching suffix from the
// end of s. The suffix must appear as a whole word (preceded by a space or be the
// entire string). Returns the trimmed result, or s unchanged if no suffix matched.
func stripTrailingSuffix(s string, suffixes []string) string {
	for _, suffix := range suffixes {
		// Try exact full-string match first (edge case: name IS just the suffix).
		if s == suffix {
			return ""
		}
		// Otherwise the suffix must be preceded by a word boundary (a space).
		candidate := s + " " // sentinel to simplify the HasSuffix check
		tail := " " + suffix
		if strings.HasSuffix(candidate, tail+" ") {
			// Trim from the end: remove " " + suffix.
			return strings.TrimSpace(s[:len(s)-len(suffix)])
		}
	}
	return s
}

// NormalizeMerchantPair returns the normalized name computed from a (merchant, description)
// pair, mirroring the SQL expression MerchantNormExpr.
//
// Rules:
//  - Use merchant when it is non-empty after trimming.
//  - Fall back to description when merchant is empty.
//  - Return "" when both are empty.
func NormalizeMerchantPair(merchant, description string) string {
	src := strings.TrimSpace(merchant)
	if src == "" {
		src = strings.TrimSpace(description)
	}
	return NormalizeMerchant(src)
}
