package classify

import (
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// xferBankPatterns holds pre-compiled word-boundary regexps for bankKeywords.
// Bank keywords are matched as whole words to avoid false positives
// (e.g. "purchase" must not match "chase").
var xferBankPatterns []*regexp.Regexp

func init() {
	xferBankPatterns = make([]*regexp.Regexp, len(bankKeywords))
	for i, kw := range bankKeywords {
		xferBankPatterns[i] = regexp.MustCompile(`\b` + regexp.QuoteMeta(kw) + `\b`)
	}
}

// xferIsTransferPattern reports whether merchant_norm contains a transfer keyword.
func xferIsTransferPattern(merchantNorm string) bool {
	for _, kw := range transferKeywords {
		if strings.Contains(merchantNorm, kw) {
			return true
		}
	}
	return false
}

// xferIsBankPattern reports whether merchant_norm contains a bank keyword as a whole word.
func xferIsBankPattern(merchantNorm string) bool {
	for _, re := range xferBankPatterns {
		if re.MatchString(merchantNorm) {
			return true
		}
	}
	return false
}

// xferIsCCPayment reports whether merchant_norm matches a credit-card payment pattern.
func xferIsCCPayment(merchantNorm string) bool {
	for _, kw := range ccPaymentKeywords {
		if strings.Contains(merchantNorm, kw) {
			return true
		}
	}
	return false
}

// xferHasKeywords is the combined keyword check used for scoring and threshold selection.
func xferHasKeywords(merchantNorm string) bool {
	return xferIsTransferPattern(merchantNorm) ||
		xferIsBankPattern(merchantNorm) ||
		xferIsCCPayment(merchantNorm)
}

// ---------------------------------------------------------------------------
// Transfer Pair Data Structures
// ---------------------------------------------------------------------------

// TransferLeg is one side of an internal transfer.
type TransferLeg struct {
	Fingerprint  string
	AccountID    string
	PostedAt     time.Time
	AmountCents  int64
	MerchantNorm string
	IsOutflow    bool
}

// TransferPair is a matched pair of transfer legs (one outflow, one inflow).
type TransferPair struct {
	PairID          string
	Outflow         TransferLeg
	Inflow          TransferLeg
	Confidence      float64
	MatchReason     string
	AmountDiffCents int64 // 0 for exact match; small value for ACH fee
}

// NetCents returns the net of inflow + outflow (should be near 0 for balanced pairs).
func (p *TransferPair) NetCents() int64 {
	return p.Inflow.AmountCents + p.Outflow.AmountCents
}

// IsBalanced reports whether the pair nets within $5 (500 cents).
func (p *TransferPair) IsBalanced() bool {
	net := p.NetCents()
	if net < 0 {
		net = -net
	}
	return net <= 500
}

// TransferPairingResult holds the output of DetectTransferPairs.
type TransferPairingResult struct {
	MatchedPairs      []TransferPair
	UnmatchedOutflows []TransferLeg
	UnmatchedInflows  []TransferLeg
}

// PairedFingerprints returns a set of all fingerprints that are part of a matched pair.
func (r *TransferPairingResult) PairedFingerprints() map[string]bool {
	out := make(map[string]bool, len(r.MatchedPairs)*2)
	for i := range r.MatchedPairs {
		out[r.MatchedPairs[i].Outflow.Fingerprint] = true
		out[r.MatchedPairs[i].Inflow.Fingerprint] = true
	}
	return out
}

// PairID returns the pair ID for the given fingerprint, or "" if not found.
func (r *TransferPairingResult) PairID(fingerprint string) string {
	for i := range r.MatchedPairs {
		p := &r.MatchedPairs[i]
		if p.Outflow.Fingerprint == fingerprint || p.Inflow.Fingerprint == fingerprint {
			return p.PairID
		}
	}
	return ""
}

// HasUnmatched reports whether any transfer legs were left without a pair.
func (r *TransferPairingResult) HasUnmatched() bool {
	return len(r.UnmatchedOutflows) > 0 || len(r.UnmatchedInflows) > 0
}

// ---------------------------------------------------------------------------
// Main Pairing Function
// ---------------------------------------------------------------------------

// DetectTransferPairs finds matching internal transfer legs across accounts.
//
// When $1000 moves from Savings to Checking:
//   - Savings shows -$1000 (outflow)
//   - Checking shows +$1000 (inflow)
//
// Both legs must be on different accounts, within toleranceDays of each other,
// and within toleranceCents of the same absolute amount.
//
// toleranceDays=3 and toleranceCents=300 (≈$3 ACH fee) are the recommended defaults.
func DetectTransferPairs(
	db *sql.DB,
	startDate, endDate time.Time,
	toleranceDays int,
	toleranceCents int64,
) *TransferPairingResult {
	const query = `
		SELECT
			t.fingerprint,
			t.account_id,
			t.posted_at,
			t.amount_cents,
			TRIM(LOWER(COALESCE(NULLIF(t.merchant,''), NULLIF(t.description,''), ''))) AS merchant_norm
		FROM transactions t
		WHERE t.posted_at >= ? AND t.posted_at < ?
		  AND COALESCE(t.pending, 0) = 0
		ORDER BY t.posted_at, ABS(t.amount_cents) DESC
	`

	rows, err := db.Query(query, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return &TransferPairingResult{}
	}
	defer rows.Close()

	var outflows []TransferLeg
	var inflows []TransferLeg

	for rows.Next() {
		var (
			fingerprint  string
			accountID    string
			postedAtStr  string
			amountCents  int64
			merchantNorm string
		)
		if err := rows.Scan(&fingerprint, &accountID, &postedAtStr, &amountCents, &merchantNorm); err != nil {
			continue
		}

		postedAt, err := parseDate(postedAtStr)
		if err != nil {
			continue
		}

		leg := TransferLeg{
			Fingerprint:  fingerprint,
			AccountID:    accountID,
			PostedAt:     postedAt,
			AmountCents:  amountCents,
			MerchantNorm: merchantNorm,
			IsOutflow:    amountCents < 0,
		}

		switch {
		case amountCents < 0:
			outflows = append(outflows, leg)
		case amountCents > 0:
			inflows = append(inflows, leg)
		}
	}
	if err := rows.Err(); err != nil {
		return &TransferPairingResult{}
	}

	result := &TransferPairingResult{}
	matchedOutflows := make(map[string]bool)
	matchedInflows := make(map[string]bool)
	pairCounter := 0

	for i := range outflows {
		outflow := &outflows[i]
		if matchedOutflows[outflow.Fingerprint] {
			continue
		}

		outflowAbs := -outflow.AmountCents // outflow.AmountCents is negative

		var bestMatch *TransferLeg
		bestScore := 0.0
		bestReason := ""

		for j := range inflows {
			inflow := &inflows[j]
			if matchedInflows[inflow.Fingerprint] {
				continue
			}
			if inflow.AccountID == outflow.AccountID {
				continue // same account is not a transfer
			}

			// Amount tolerance gate
			amountDiff := outflowAbs - inflow.AmountCents
			if amountDiff < 0 {
				amountDiff = -amountDiff
			}
			if amountDiff > toleranceCents {
				continue
			}

			// Date tolerance gate
			daysDiff := int(math.Round(inflow.PostedAt.Sub(outflow.PostedAt).Hours() / 24))
			if daysDiff < 0 {
				daysDiff = -daysDiff
			}
			if daysDiff > toleranceDays {
				continue
			}

			// Base score: 40% date proximity + 60% amount proximity
			dateScore := 1.0 - float64(daysDiff)/float64(toleranceDays+1)
			amountScore := 1.0 - float64(amountDiff)/float64(toleranceCents+1)
			score := (dateScore * 0.4) + (amountScore * 0.6)

			// Bonus: exact amount
			if amountDiff == 0 {
				score += 0.2
			}
			// Bonus: same-day
			if daysDiff == 0 {
				score += 0.1
			}
			// Bonus: transfer-like keywords
			outflowKW := xferHasKeywords(outflow.MerchantNorm)
			inflowKW := xferHasKeywords(inflow.MerchantNorm)
			if outflowKW || inflowKW {
				score += 0.15
			}
			if outflowKW && inflowKW {
				score += 0.10 // extra bonus when both sides carry keywords
			}

			if score > bestScore {
				bestScore = score
				match := *inflow // copy to avoid aliasing
				bestMatch = &match
				bestReason = xferMatchReason(daysDiff, amountDiff)
			}
		}

		// Higher confidence threshold required when the outflow has no keywords.
		minScore := 0.5
		if !xferHasKeywords(outflow.MerchantNorm) {
			minScore = 0.7
		}

		if bestMatch != nil && bestScore >= minScore {
			pairCounter++
			pairID := fmt.Sprintf("%08d", pairCounter)

			pair := TransferPair{
				PairID:          pairID,
				Outflow:         *outflow,
				Inflow:          *bestMatch,
				Confidence:      math.Min(1.0, bestScore),
				MatchReason:     bestReason,
				AmountDiffCents: func() int64 { d := outflowAbs - bestMatch.AmountCents; if d < 0 { return -d }; return d }(),
			}
			result.MatchedPairs = append(result.MatchedPairs, pair)
			matchedOutflows[outflow.Fingerprint] = true
			matchedInflows[bestMatch.Fingerprint] = true
		}
	}

	// Collect unmatched legs
	for i := range outflows {
		if !matchedOutflows[outflows[i].Fingerprint] {
			result.UnmatchedOutflows = append(result.UnmatchedOutflows, outflows[i])
		}
	}
	for i := range inflows {
		if !matchedInflows[inflows[i].Fingerprint] {
			result.UnmatchedInflows = append(result.UnmatchedInflows, inflows[i])
		}
	}

	return result
}

// xferMatchReason produces a human-readable description of why two legs were paired.
func xferMatchReason(daysDiff int, amountDiff int64) string {
	parts := make([]string, 0, 2)

	if amountDiff == 0 {
		parts = append(parts, "exact amount")
	} else {
		parts = append(parts, fmt.Sprintf("±$%.2f", float64(amountDiff)/100.0))
	}

	switch daysDiff {
	case 0:
		parts = append(parts, "same day")
	case 1:
		parts = append(parts, "1 day apart")
	default:
		parts = append(parts, fmt.Sprintf("%d days apart", daysDiff))
	}

	return strings.Join(parts, ", ")
}

// parseDate parses a date string that may be a full RFC3339 timestamp or a plain
// YYYY-MM-DD date, returning a UTC midnight Time value.
func parseDate(s string) (time.Time, error) {
	if len(s) == 10 {
		return time.Parse("2006-01-02", s)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05", s)
	}
	return t, err
}
