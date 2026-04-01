package server

import (
	"fmt"
	"html/template"
	"math"
	"net/http"
	"path/filepath"
	"strings"
)

// TemplateEngine loads and caches Go html/template files from a directory.
// All templates share a common set of utility functions registered at
// construction time.
type TemplateEngine struct {
	tmpl    *template.Template
	version string
}

// NewTemplateEngine parses all *.html files in dir and returns a ready
// TemplateEngine. Templates are parsed once and cached for the lifetime of
// the engine; restart the server to pick up changes.
// version is the build version string shown in the nav bar.
func NewTemplateEngine(dir, version string) (*TemplateEngine, error) {
	pattern := filepath.Join(dir, "*.html")

	tmpl := template.New("").Funcs(templateFuncs())

	parsed, err := tmpl.ParseGlob(pattern)
	if err != nil {
		return nil, fmt.Errorf("parse templates from %s: %w", dir, err)
	}

	return &TemplateEngine{tmpl: parsed, version: version}, nil
}

// Render writes the named template to w with the given data.
// On error it writes a 500 response; callers should not write to w afterward.
func (e *TemplateEngine) Render(w http.ResponseWriter, name string, data any) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := e.tmpl.ExecuteTemplate(w, name, data); err != nil {
		// Header already sent, just log — the partial response is broken.
		return fmt.Errorf("render template %q: %w", name, err)
	}
	return nil
}

// templateFuncs returns the FuncMap shared by all templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// formatUSD converts integer cents to a dollar string: 123456 -> "$1,234.56".
		"formatUSD": formatUSD,

		// formatDate formats an ISO date string "2006-01-02" to "Jan 2, 2006".
		"formatDate": formatDate,

		// formatDateTime formats an ISO datetime string to "2006-01-02 15:04".
		"formatDateTime": formatDateTime,

		// formatPercent rounds a float64 to one decimal place.
		"formatPercent": formatPercent,

		// absInt returns the absolute value of an int64.
		"absInt": absInt,

		// neg negates an int64.
		"neg": func(n int64) int64 { return -n },

		// add adds two int64 values.
		"add": func(a, b int64) int64 { return a + b },

		// sub subtracts b from a.
		"sub": func(a, b int64) int64 { return a - b },

		// mul multiplies two int64 values.
		"mul": func(a, b int64) int64 { return a * b },

		// div100 divides a by b and returns a float64 (safe: returns 0 if b==0).
		"div100": func(a, b int64) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b)
		},

		// clamp clamps a float64 to [lo, hi].
		"clamp": func(v, lo, hi float64) float64 {
			if v < lo {
				return lo
			}
			if v > hi {
				return hi
			}
			return v
		},

		// not inverts a bool.
		"not": func(b bool) bool { return !b },

		// slice returns a sub-slice of s from index lo to hi (exclusive).
		// Works on []CategoryItem and []PeriodSummary.
		"slice": templateSlice,

		// shortMonth extracts the 3-letter month abbreviation from a period label
		// like "March 2025" -> "Mar".
		"shortMonth": func(label string) string {
			parts := strings.Fields(label)
			if len(parts) == 0 {
				return label
			}
			s := parts[0]
			if len(s) > 3 {
				return s[:3]
			}
			return s
		},

		// avgNetCents computes the average NetCents across a slice of PeriodSummary.
		"avgNetCents": func(periods []PeriodSummary) int64 {
			if len(periods) == 0 {
				return 0
			}
			var sum int64
			for _, p := range periods {
				sum += p.NetCents
			}
			return sum / int64(len(periods))
		},

		// truncate returns s truncated to at most n runes.
		"truncate": func(s string, n int) string {
			runes := []rune(s)
			if len(runes) <= n {
				return s
			}
			return string(runes[:n])
		},
	}
}

// formatUSD converts integer cents to a "$1,234.56" string.
// Negative values are rendered as "-$1,234.56".
func formatUSD(cents int64) string {
	negative := cents < 0
	if negative {
		cents = -cents
	}
	dollars := cents / 100
	frac := cents % 100

	// Build comma-separated dollar part.
	dollarsStr := formatIntCommas(dollars)

	result := fmt.Sprintf("$%s.%02d", dollarsStr, frac)
	if negative {
		result = "-" + result
	}
	return result
}

// formatIntCommas formats an integer with comma thousands separators.
func formatIntCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	offset := len(s) % 3
	if offset > 0 {
		b.WriteString(s[:offset])
	}
	for i := offset; i < len(s); i += 3 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// formatDate formats an ISO 8601 date prefix "YYYY-MM-DD" to "Jan 2, 2006".
// Returns the input unchanged if it cannot be parsed.
func formatDate(iso string) string {
	if len(iso) < 10 {
		return iso
	}
	// Manual parse to avoid importing time just for a format.
	parts := strings.SplitN(iso[:10], "-", 3)
	if len(parts) != 3 {
		return iso
	}
	months := [...]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	var m int
	fmt.Sscanf(parts[1], "%d", &m)
	var d int
	fmt.Sscanf(parts[2], "%d", &d)
	if m < 1 || m > 12 {
		return iso
	}
	return fmt.Sprintf("%s %d, %s", months[m], d, parts[0])
}

// formatDateTime formats "2006-01-02T15:04:05Z" (or space-separated) to "2006-01-02 15:04".
func formatDateTime(iso string) string {
	if len(iso) < 16 {
		return iso
	}
	// Normalise T separator.
	s := strings.Replace(iso[:16], "T", " ", 1)
	return s
}

// formatPercent rounds v to zero decimal places and returns the string.
func formatPercent(v float64) string {
	return fmt.Sprintf("%.0f", math.Round(v))
}

// absInt returns the absolute value of n.
func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// templateSlice provides a type-safe slice helper for the concrete types used
// in templates. It handles []CategoryItem and []PeriodSummary.
func templateSlice(v any, lo, hi int) any {
	switch s := v.(type) {
	case []CategoryItem:
		if lo < 0 {
			lo = 0
		}
		if hi > len(s) {
			hi = len(s)
		}
		if lo > hi {
			return []CategoryItem{}
		}
		return s[lo:hi]
	case []PeriodSummary:
		if lo < 0 {
			lo = 0
		}
		if hi > len(s) {
			hi = len(s)
		}
		if lo > hi {
			return []PeriodSummary{}
		}
		return s[lo:hi]
	case []AttentionItem:
		if lo < 0 {
			lo = 0
		}
		if hi > len(s) {
			hi = len(s)
		}
		if lo > hi {
			return []AttentionItem{}
		}
		return s[lo:hi]
	default:
		return v
	}
}
