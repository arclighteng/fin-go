package money

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseToCents converts a dollar amount (string, float64, int) to integer cents
// using round-half-up (standard financial rounding).
func ParseToCents(amount any) (int64, error) {
	var dollars float64

	switch v := amount.(type) {
	case int:
		dollars = float64(v)
	case int64:
		dollars = float64(v)
	case float64:
		dollars = v
	case string:
		clean := strings.TrimSpace(strings.ReplaceAll(v, ",", ""))
		if clean == "" {
			return 0, fmt.Errorf("amount string is empty")
		}
		var err error
		dollars, err = strconv.ParseFloat(clean, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse amount %q: %w", v, err)
		}
	default:
		return 0, fmt.Errorf("unsupported amount type: %T", amount)
	}

	// Sanity check: $1M limit
	if math.Abs(dollars) > 1_000_000 {
		return 0, fmt.Errorf("amount %.2f exceeds $1,000,000 sanity limit", dollars)
	}

	return roundHalfUp(dollars * 100), nil
}

// ParseToCentsAllowLarge allows amounts over $1M.
func ParseToCentsAllowLarge(amount any) (int64, error) {
	var dollars float64

	switch v := amount.(type) {
	case int:
		dollars = float64(v)
	case int64:
		dollars = float64(v)
	case float64:
		dollars = v
	case string:
		clean := strings.TrimSpace(strings.ReplaceAll(v, ",", ""))
		if clean == "" {
			return 0, fmt.Errorf("amount string is empty")
		}
		var err error
		dollars, err = strconv.ParseFloat(clean, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse amount %q: %w", v, err)
		}
	default:
		return 0, fmt.Errorf("unsupported amount type: %T", amount)
	}

	return roundHalfUp(dollars * 100), nil
}

// CentsToDollars converts integer cents to a float64 dollar amount.
func CentsToDollars(cents int64) float64 {
	return float64(cents) / 100.0
}

// FormatUSD formats cents as a USD string (e.g., "$12.99", "-$12.99").
func FormatUSD(cents int64) string {
	abs := cents
	if abs < 0 {
		abs = -abs
	}
	dollars := float64(abs) / 100.0
	formatted := fmt.Sprintf("$%s", formatWithCommas(dollars))
	if cents < 0 {
		return "-" + formatted
	}
	return formatted
}

// FormatUSDSigned always shows + or - sign.
func FormatUSDSigned(cents int64) string {
	abs := cents
	if abs < 0 {
		abs = -abs
	}
	dollars := float64(abs) / 100.0
	formatted := fmt.Sprintf("$%s", formatWithCommas(dollars))
	if cents < 0 {
		return "-" + formatted
	}
	if cents > 0 {
		return "+" + formatted
	}
	return formatted
}

// FormatUSDCompact formats without decimals for whole dollar amounts.
func FormatUSDCompact(cents int64) string {
	abs := cents
	if abs < 0 {
		abs = -abs
	}

	var formatted string
	if cents%100 == 0 {
		formatted = fmt.Sprintf("$%s", formatIntWithCommas(abs/100))
	} else {
		dollars := float64(abs) / 100.0
		formatted = fmt.Sprintf("$%s", formatWithCommas(dollars))
	}

	if cents < 0 {
		return "-" + formatted
	}
	return formatted
}

// MultiplyCents multiplies cents by a factor with round-half-up.
func MultiplyCents(cents int64, factor float64) int64 {
	return roundHalfUp(float64(cents) * factor)
}

// DivideCents divides cents by a divisor with round-half-up.
func DivideCents(cents int64, divisor float64) (int64, error) {
	if divisor == 0 {
		return 0, fmt.Errorf("cannot divide by zero")
	}
	return roundHalfUp(float64(cents) / divisor), nil
}

// PercentOf calculates a percentage of a cent amount.
func PercentOf(cents int64, percentage float64) int64 {
	return MultiplyCents(cents, percentage/100.0)
}

// CompareWithinThreshold checks if two amounts are within a threshold.
func CompareWithinThreshold(a, b int64, thresholdCents int64, thresholdPercent float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}

	if thresholdCents > 0 && diff <= thresholdCents {
		return true
	}

	if thresholdPercent > 0 {
		absA := a
		if absA < 0 {
			absA = -absA
		}
		absB := b
		if absB < 0 {
			absB = -absB
		}
		base := absA
		if absB > base {
			base = absB
		}
		if base == 0 {
			base = 1
		}
		pctDiff := float64(diff) / float64(base) * 100
		if pctDiff <= thresholdPercent {
			return true
		}
	}

	if thresholdCents == 0 && thresholdPercent == 0 {
		return a == b
	}

	return false
}

// roundHalfUp rounds away from zero (standard financial rounding).
func roundHalfUp(f float64) int64 {
	if f >= 0 {
		return int64(math.Floor(f + 0.5))
	}
	return int64(math.Ceil(f - 0.5))
}

func formatWithCommas(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	parts := strings.Split(s, ".")
	intPart := parts[0]
	decPart := parts[1]

	if len(intPart) <= 3 {
		return intPart + "." + decPart
	}

	var result []byte
	for i, c := range intPart {
		pos := len(intPart) - i
		if pos%3 == 0 && i > 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result) + "." + decPart
}

func formatIntWithCommas(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}

	var result []byte
	for i, c := range s {
		pos := len(s) - i
		if pos%3 == 0 && i > 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
