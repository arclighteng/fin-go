package classify

// Recurring / subscription detection.
//
// This is the detection half of the product's differentiated primitive: "we
// find your subscriptions automatically." It scans posted expense transactions,
// groups them by normalised merchant, and reports merchants that charge on a
// regular cadence for a stable amount — i.e. subscriptions and recurring bills.
//
// It is intentionally conservative. Habitual-but-irregular spend (groceries,
// coffee, gas) has neither a regular interval nor a stable amount and is
// rejected, so it is not misreported as a subscription. The cadence buckets
// mirror the thresholds already used by internal/projections when it reads
// subscription_candidates, and the median math mirrors ADA-113's fix in
// projections (true numeric median, mean of the two middle values for an
// even-sized sample) so amounts stay honest and consistent across the codebase.

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// DetectedCommitment is a subscription or recurring bill inferred from
// transaction history. All money is integer cents; AmountCents is the median
// absolute charge amount (always positive).
type DetectedCommitment struct {
	MerchantNorm string
	Name         string
	AmountCents  int64 // median absolute charge, in cents
	Cadence      string // weekly, biweekly, monthly, quarterly, annual
	IntervalDays int    // median gap between charges, in days
	Confidence   float64 // 0.0–1.0
	LastSeen     time.Time
	DayOfMonth   int    // day-of-month of the most recent charge (1–31)
	Direction    string // always "expense" for now
}

// RecurringOptions tunes the detector. The zero value is a sensible default.
type RecurringOptions struct {
	// MinOccurrences is the minimum number of charges (on distinct days) a
	// merchant must have before it can be considered recurring. Default 3.
	MinOccurrences int
	// IntervalToleranceFrac is how far a gap may deviate from the median gap
	// and still count as "regular". Default 0.25 (±25%).
	IntervalToleranceFrac float64
	// AmountToleranceFrac is how far a charge may deviate from the median
	// amount and still count as "stable". Default 0.25 (±25%).
	AmountToleranceFrac float64
	// MinConfidence is the minimum confidence to report a candidate. Default 0.5.
	MinConfidence float64
	// Since, when non-zero, restricts detection to charges on or after this date.
	Since time.Time
}

func (o RecurringOptions) withDefaults() RecurringOptions {
	if o.MinOccurrences <= 0 {
		o.MinOccurrences = 3
	}
	if o.IntervalToleranceFrac <= 0 {
		o.IntervalToleranceFrac = 0.25
	}
	if o.AmountToleranceFrac <= 0 {
		o.AmountToleranceFrac = 0.25
	}
	if o.MinConfidence <= 0 {
		o.MinConfidence = 0.5
	}
	return o
}

type recurringCharge struct {
	date   time.Time
	amount int64 // absolute cents
}

// DetectRecurring scans posted (non-pending) expense transactions and returns
// the merchants that charge on a regular cadence for a stable amount. Results
// are sorted by descending confidence, then merchant name, for determinism.
func DetectRecurring(db *sql.DB, opts RecurringOptions) ([]DetectedCommitment, error) {
	opts = opts.withDefaults()

	query := `
		SELECT TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), ''))) AS merchant_norm,
		       posted_at,
		       ABS(amount_cents) AS amount
		FROM transactions
		WHERE amount_cents < 0
		  AND COALESCE(pending, 0) = 0`
	var args []any
	if !opts.Since.IsZero() {
		query += "\n  AND posted_at >= ?"
		args = append(args, opts.Since.Format("2006-01-02"))
	}
	query += "\n  ORDER BY merchant_norm, posted_at"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("classify: detect recurring query: %w", err)
	}
	defer rows.Close()

	byMerchant := make(map[string][]recurringCharge)
	for rows.Next() {
		var merchant, postedAt string
		var amount int64
		if err := rows.Scan(&merchant, &postedAt, &amount); err != nil {
			continue
		}
		if merchant == "" {
			continue
		}
		t, perr := parseDate(postedAt)
		if perr != nil {
			continue
		}
		byMerchant[merchant] = append(byMerchant[merchant], recurringCharge{date: t, amount: amount})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("classify: detect recurring scan: %w", err)
	}

	var out []DetectedCommitment
	for merchant, charges := range byMerchant {
		if dc, ok := evaluateMerchant(merchant, charges, opts); ok {
			out = append(out, dc)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].MerchantNorm < out[j].MerchantNorm
	})
	return out, nil
}

// evaluateMerchant decides whether a single merchant's charges form a recurring
// subscription/bill and, if so, returns the detected commitment.
func evaluateMerchant(merchant string, charges []recurringCharge, opts RecurringOptions) (DetectedCommitment, bool) {
	// Sort by date so gaps are meaningful.
	sort.Slice(charges, func(i, j int) bool { return charges[i].date.Before(charges[j].date) })

	// Collapse multiple charges on the same calendar day into one occurrence for
	// cadence purposes (they would otherwise produce spurious zero-day gaps), but
	// keep every charge amount for the amount-stability check.
	var uniqueDates []time.Time
	var amounts []int64
	var lastDay string
	for _, c := range charges {
		amounts = append(amounts, c.amount)
		day := c.date.Format("2006-01-02")
		if day != lastDay {
			uniqueDates = append(uniqueDates, c.date)
			lastDay = day
		}
	}

	if len(uniqueDates) < opts.MinOccurrences {
		return DetectedCommitment{}, false
	}

	// Gaps between consecutive charge dates, in whole days.
	gaps := make([]float64, 0, len(uniqueDates)-1)
	for i := 1; i < len(uniqueDates); i++ {
		d := uniqueDates[i].Sub(uniqueDates[i-1]).Hours() / 24.0
		gaps = append(gaps, d)
	}
	if len(gaps) == 0 {
		return DetectedCommitment{}, false
	}

	medianGap := medianFloat(gaps)
	// Reject cadences that are too tight (daily-ish, not a subscription) or too
	// wide (beyond a yearly bill) to model honestly.
	if medianGap < 5 || medianGap > 400 {
		return DetectedCommitment{}, false
	}

	regFrac := fractionWithin(gaps, medianGap, opts.IntervalToleranceFrac)
	if regFrac < 0.5 {
		return DetectedCommitment{}, false
	}

	medianAmt := medianInt64(amounts)
	if medianAmt <= 0 {
		return DetectedCommitment{}, false
	}
	amtFrac := fractionWithinInt64(amounts, medianAmt, opts.AmountToleranceFrac)
	if amtFrac < 0.5 {
		return DetectedCommitment{}, false
	}

	occ := len(uniqueDates)
	countScore := float64(occ) / 6.0
	if countScore > 1.0 {
		countScore = 1.0
	}
	confidence := 0.5*regFrac + 0.3*amtFrac + 0.2*countScore
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < opts.MinConfidence {
		return DetectedCommitment{}, false
	}

	intervalDays := int(medianGap + 0.5)
	last := uniqueDates[len(uniqueDates)-1]

	return DetectedCommitment{
		MerchantNorm: merchant,
		Name:         merchant,
		AmountCents:  medianAmt,
		Cadence:      cadenceForInterval(intervalDays),
		IntervalDays: intervalDays,
		Confidence:   confidence,
		LastSeen:     last,
		DayOfMonth:   last.Day(),
		Direction:    "expense",
	}, true
}

// cadenceForInterval maps a median interval in days to a cadence label. The
// thresholds match internal/projections' subscription_candidates reader so the
// two layers agree on what "monthly" (etc.) means.
func cadenceForInterval(days int) string {
	switch {
	case days <= 8:
		return "weekly"
	case days <= 16:
		return "biweekly"
	case days <= 35:
		return "monthly"
	case days <= 100:
		return "quarterly"
	default:
		return "annual"
	}
}

// fractionWithin returns the fraction of values within ±tol of center.
func fractionWithin(values []float64, center, tol float64) float64 {
	if len(values) == 0 {
		return 0
	}
	lo := center * (1 - tol)
	hi := center * (1 + tol)
	n := 0
	for _, v := range values {
		if v >= lo && v <= hi {
			n++
		}
	}
	return float64(n) / float64(len(values))
}

func fractionWithinInt64(values []int64, center int64, tol float64) float64 {
	if len(values) == 0 {
		return 0
	}
	lo := float64(center) * (1 - tol)
	hi := float64(center) * (1 + tol)
	n := 0
	for _, v := range values {
		f := float64(v)
		if f >= lo && f <= hi {
			n++
		}
	}
	return float64(n) / float64(len(values))
}

// medianFloat returns the true numeric median (mean of the two middle values
// for an even-sized sample).
func medianFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// medianInt64 mirrors projections.medianCommitmentAmount: sort ascending, take
// the middle value, or the integer mean of the two middle values.
func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	s := append([]int64(nil), values...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
