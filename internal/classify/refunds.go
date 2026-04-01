package classify

import (
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// refundMatchKeywords are substrings that signal a credit is likely a refund.
var refundMatchKeywords = []string{
	"refund", "credit", "return", "reversal", "chargeback",
	"adjustment", "rebate", "reimbursement", "cashback",
}

// significantWordSplitter splits on spaces, dots, dashes, and underscores.
var significantWordSplitter = regexp.MustCompile(`[\s.\-_]+`)

// refundHasKeyword reports whether merchant_norm contains a refund keyword.
func refundHasKeyword(merchantNorm string) bool {
	for _, kw := range refundMatchKeywords {
		if strings.Contains(merchantNorm, kw) {
			return true
		}
	}
	return false
}

// significantWords returns words from s that are longer than two characters,
// using the same split logic as the Python port.
func significantWords(s string) []string {
	raw := significantWordSplitter.Split(s, -1)
	out := make([]string, 0, len(raw))
	for _, w := range raw {
		if len(w) > 2 {
			out = append(out, w)
		}
	}
	return out
}

// merchantsMatch reports whether two normalized merchant strings refer to the
// same merchant. It mirrors the Python _merchants_match logic exactly.
func merchantsMatch(m1, m2 string) bool {
	if m1 == "" || m2 == "" {
		return false
	}
	if m1 == m2 {
		return true
	}
	// Substring containment handles "AMAZON" vs "AMAZON MARKETPLACE".
	if strings.Contains(m1, m2) || strings.Contains(m2, m1) {
		return true
	}
	// First significant word match handles "AMAZON.COM" vs "AMAZON PRIME".
	w1 := significantWords(m1)
	w2 := significantWords(m2)
	if len(w1) > 0 && len(w2) > 0 && w1[0] == w2[0] {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Refund Match Data Structures
// ---------------------------------------------------------------------------

// RefundMatch is a matched refund-to-expense pair.
type RefundMatch struct {
	RefundFingerprint  string
	ExpenseFingerprint string
	RefundAmountCents  int64
	ExpenseAmountCents int64 // Negative (the original expense)
	MerchantNorm       string
	DaysApart          int
	Confidence         float64
	MatchReason        string
}

// IsFullRefund reports whether the refund fully covers the original expense.
func (m *RefundMatch) IsFullRefund() bool {
	return m.RefundAmountCents >= abs64(m.ExpenseAmountCents)
}

// IsPartialRefund reports whether the refund only partially covers the expense.
func (m *RefundMatch) IsPartialRefund() bool {
	exp := abs64(m.ExpenseAmountCents)
	return m.RefundAmountCents > 0 && m.RefundAmountCents < exp
}

// RefundMatchingResult holds the output of DetectRefundMatches.
type RefundMatchingResult struct {
	MatchedRefunds   []RefundMatch
	UnmatchedRefunds []string // fingerprints of credits that look like refunds but had no match
}

// MatchedFingerprints returns the set of refund fingerprints that were matched.
func (r *RefundMatchingResult) MatchedFingerprints() map[string]bool {
	out := make(map[string]bool, len(r.MatchedRefunds))
	for i := range r.MatchedRefunds {
		out[r.MatchedRefunds[i].RefundFingerprint] = true
	}
	return out
}

// ExpenseForRefund returns the expense fingerprint matched to the given refund
// fingerprint, or "" if not found.
func (r *RefundMatchingResult) ExpenseForRefund(refundFP string) string {
	for i := range r.MatchedRefunds {
		if r.MatchedRefunds[i].RefundFingerprint == refundFP {
			return r.MatchedRefunds[i].ExpenseFingerprint
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Main Matching Function
// ---------------------------------------------------------------------------

// DetectRefundMatches finds positive transactions (credits) that match prior
// expenses from the same or similar merchant.
//
// Scoring:
//   - Merchant similarity:    40%
//   - Refund keyword present: 20%
//   - Amount similarity:      30%
//   - Date proximity:         10%
//
// Minimum score to accept a match: 0.4.
//
// lookbackDays=90 and amountTolerancePct=5.0 are the recommended defaults.
func DetectRefundMatches(
	db *sql.DB,
	startDate, endDate time.Time,
	lookbackDays int,
	amountTolerancePct float64,
) *RefundMatchingResult {
	// Potential refunds: positive amounts in the analysis window.
	const refundQuery = `
		SELECT
			t.fingerprint,
			t.account_id,
			t.posted_at,
			t.amount_cents,
			TRIM(LOWER(COALESCE(NULLIF(t.merchant,''), NULLIF(t.description,''), ''))) AS merchant_norm
		FROM transactions t
		WHERE t.posted_at >= ? AND t.posted_at < ?
		  AND t.amount_cents > 0
		  AND COALESCE(t.pending, 0) = 0
		ORDER BY t.posted_at DESC
	`

	refundRows, err := db.Query(refundQuery,
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
	)
	if err != nil {
		return &RefundMatchingResult{}
	}
	defer refundRows.Close()

	type txnRow struct {
		fingerprint  string
		accountID    string
		postedAt     time.Time
		amountCents  int64
		merchantNorm string
	}

	var refunds []txnRow
	for refundRows.Next() {
		var r txnRow
		var postedAtStr string
		if err := refundRows.Scan(&r.fingerprint, &r.accountID, &postedAtStr, &r.amountCents, &r.merchantNorm); err != nil {
			continue
		}
		r.postedAt, err = parseDate(postedAtStr)
		if err != nil {
			continue
		}
		refunds = append(refunds, r)
	}
	if err := refundRows.Err(); err != nil {
		return &RefundMatchingResult{}
	}

	// Potential expenses: negative amounts going back lookbackDays further.
	expenseStart := startDate.AddDate(0, 0, -lookbackDays)
	const expenseQuery = `
		SELECT
			t.fingerprint,
			t.account_id,
			t.posted_at,
			t.amount_cents,
			TRIM(LOWER(COALESCE(NULLIF(t.merchant,''), NULLIF(t.description,''), ''))) AS merchant_norm
		FROM transactions t
		WHERE t.posted_at >= ? AND t.posted_at < ?
		  AND t.amount_cents < 0
		  AND COALESCE(t.pending, 0) = 0
		ORDER BY t.posted_at DESC
	`

	expenseRows, err := db.Query(expenseQuery,
		expenseStart.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
	)
	if err != nil {
		return &RefundMatchingResult{}
	}
	defer expenseRows.Close()

	var expenses []txnRow
	for expenseRows.Next() {
		var e txnRow
		var postedAtStr string
		if err := expenseRows.Scan(&e.fingerprint, &e.accountID, &postedAtStr, &e.amountCents, &e.merchantNorm); err != nil {
			continue
		}
		e.postedAt, err = parseDate(postedAtStr)
		if err != nil {
			continue
		}
		expenses = append(expenses, e)
	}
	if err := expenseRows.Err(); err != nil {
		return &RefundMatchingResult{}
	}

	result := &RefundMatchingResult{}
	matchedExpenseFPs := make(map[string]bool)

	for i := range refunds {
		refund := &refunds[i]
		isRefundLike := refundHasKeyword(refund.merchantNorm)

		var bestExpenseFP string
		var bestExpenseAmountCents int64
		var bestDaysApart int
		var bestScore float64

		for j := range expenses {
			expense := &expenses[j]
			if matchedExpenseFPs[expense.fingerprint] {
				continue
			}

			// Refund must be AFTER the expense.
			daysApart := int(math.Round(refund.postedAt.Sub(expense.postedAt).Hours() / 24))
			if daysApart < 0 || daysApart > lookbackDays {
				continue
			}

			expenseAbs := abs64(expense.amountCents)

			// Merchant match required unless the credit already has a refund keyword.
			mMatch := merchantsMatch(refund.merchantNorm, expense.merchantNorm)
			if !mMatch && !isRefundLike {
				continue
			}

			// Refund amount must not exceed the expense by more than the tolerance.
			tolerance := int64(math.Round(float64(expenseAbs) * amountTolerancePct / 100.0))
			if refund.amountCents > expenseAbs+tolerance {
				continue
			}

			// Score assembly
			score := 0.0

			// Merchant similarity: 40%
			if mMatch {
				score += 0.4
			}

			// Refund keyword: 20%
			if isRefundLike {
				score += 0.2
			}

			// Amount similarity: 30%
			amountDiffPct := math.Abs(float64(refund.amountCents)-float64(expenseAbs)) /
				float64(expenseAbs) * 100.0
			switch {
			case amountDiffPct < 1.0:
				score += 0.3
			case amountDiffPct < 5.0:
				score += 0.2
			case amountDiffPct < 20.0:
				score += 0.1
			}

			// Date proximity: 10%
			if daysApart <= 7 {
				score += 0.1
			} else if daysApart <= 30 {
				score += 0.05
			}

			if score > bestScore && score >= 0.4 {
				bestScore = score
				bestExpenseFP = expense.fingerprint
				bestExpenseAmountCents = expense.amountCents
				bestDaysApart = daysApart
			}
		}

		if bestExpenseFP != "" {
			match := RefundMatch{
				RefundFingerprint:  refund.fingerprint,
				ExpenseFingerprint: bestExpenseFP,
				RefundAmountCents:  refund.amountCents,
				ExpenseAmountCents: bestExpenseAmountCents,
				MerchantNorm:       refund.merchantNorm,
				DaysApart:          bestDaysApart,
				Confidence:         bestScore,
				MatchReason:        refundMatchReason(bestDaysApart, isRefundLike),
			}
			result.MatchedRefunds = append(result.MatchedRefunds, match)
			matchedExpenseFPs[bestExpenseFP] = true
		} else if isRefundLike {
			// Credit looks like a refund but no expense was found — flag as unmatched.
			result.UnmatchedRefunds = append(result.UnmatchedRefunds, refund.fingerprint)
		}
	}

	return result
}

// refundMatchReason produces a human-readable description of why a refund was
// matched to an expense.
func refundMatchReason(daysApart int, hasRefundKeyword bool) string {
	parts := make([]string, 0, 2)

	if hasRefundKeyword {
		parts = append(parts, "refund keyword")
	}

	switch {
	case daysApart == 0:
		parts = append(parts, "same day")
	default:
		parts = append(parts, fmt.Sprintf("%dd apart", daysApart))
	}

	if len(parts) == 0 {
		return "merchant match"
	}
	return strings.Join(parts, ", ")
}

// abs64 returns the absolute value of a int64.
func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
