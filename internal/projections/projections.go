// Package projections generates heuristic cash-flow forecasts.
//
// HEURISTIC MODULE — NOT CANONICAL:
// Projections use pattern-detection heuristics to predict future charges.
// Results are estimates; always display the Confidence field to users.
//
// Income estimation uses the canonical commitment table when available and
// falls back to a rolling average of the last N months.
// Subscription / bill detection queries subscription_candidates and
// commitments tables directly.
package projections

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// UpcomingCharge is a single predicted future charge.
type UpcomingCharge struct {
	Merchant         string
	DisplayName      string // human-readable label; may be empty
	ExpectedDate     time.Time
	ExpectedCents    int64
	Confidence       float64 // 0.0–1.0
	Cadence          string  // monthly, weekly, annual, quarterly, biweekly, one_time
	IsSubscription   bool
	LastChargeDate   *time.Time
}

// Projection is the full cash-flow forecast for a future period.
// The IsHeuristic flag is always true; callers must not treat these as
// canonical numbers.
type Projection struct {
	StartDate             time.Time
	EndDate               time.Time
	ExpectedIncomeCents   int64
	ExpectedFixedCents    int64
	ExpectedVariableCents int64
	ExpectedDiscretionary int64
	ExpectedNetCents      int64
	UpcomingCharges       []UpcomingCharge
	Confidence            float64
	IsHeuristic           bool // always true
}

// Alert signals a potential cash-flow issue detected in the projection.
type Alert struct {
	AlertType  string // "shortfall", "large_charge", "multiple_charges", "low_confidence"
	Severity   string // "low", "medium", "high"
	Date       time.Time
	Message    string
	AmountCents *int64
	Merchant    *string
}

// ProjectOptions controls the projection window and optional account filter.
type ProjectOptions struct {
	DaysForward   int      // default 30
	AccountFilter []string // nil means all accounts
}

// ProjectCashFlow returns a heuristic cash-flow projection for the next
// opts.DaysForward days. It queries the commitments, subscription_candidates,
// and transactions tables directly; the report engine is not invoked.
func ProjectCashFlow(db *sql.DB, opts ProjectOptions) (*Projection, error) {
	if opts.DaysForward <= 0 {
		opts.DaysForward = 30
	}

	today := startOfDay(time.Now().UTC())
	endDate := today.AddDate(0, 0, opts.DaysForward)

	upcoming, err := buildUpcomingCharges(db, today, endDate, opts.AccountFilter)
	if err != nil {
		return nil, err
	}

	// Sort by expected date ascending.
	sortChargesByDate(upcoming)

	expectedFixed := int64(0)
	// excludeMerchants collects the normalized merchant of every charge already
	// represented by a fixed commitment or detected subscription, so that
	// estimateFlexibleSpending does not double-count those transactions.
	excludeMerchants := make(map[string]struct{})
	for _, c := range upcoming {
		expectedFixed += c.ExpectedCents
		if norm := normalizeMerchant(c.Merchant); norm != "" {
			excludeMerchants[norm] = struct{}{}
		}
	}

	expectedIncome, err := estimateIncome(db, opts.DaysForward, opts.AccountFilter)
	if err != nil {
		return nil, err
	}

	expectedVariable, expectedDiscretionary, err := estimateFlexibleSpending(db, opts.DaysForward, opts.AccountFilter, excludeMerchants)
	if err != nil {
		return nil, err
	}

	expectedNet := expectedIncome - expectedFixed - expectedVariable - expectedDiscretionary

	confidence := 0.5
	if len(upcoming) > 0 {
		var sumConf float64
		for _, c := range upcoming {
			sumConf += c.Confidence
		}
		confidence = sumConf / float64(len(upcoming))
	}

	return &Projection{
		StartDate:             today,
		EndDate:               endDate,
		ExpectedIncomeCents:   expectedIncome,
		ExpectedFixedCents:    expectedFixed,
		ExpectedVariableCents: expectedVariable,
		ExpectedDiscretionary: expectedDiscretionary,
		ExpectedNetCents:      expectedNet,
		UpcomingCharges:       upcoming,
		Confidence:            confidence,
		IsHeuristic:           true,
	}, nil
}

// DetectAlerts produces cash-flow alerts from a projection.
// Pass the current-month integrity score so that low-quality data is gated.
func DetectAlerts(proj *Projection, integrityScore float64, minConfidence float64) []Alert {
	if minConfidence <= 0 {
		minConfidence = 0.5
	}

	if integrityScore < 0.8 {
		return []Alert{{
			AlertType: "resolution_needed",
			Severity:  "medium",
			Date:      proj.StartDate,
			Message: fmt.Sprintf(
				"Resolve data quality issues before viewing projections. Integrity score: %.0f%%",
				integrityScore*100,
			),
		}}
	}

	if proj.Confidence < minConfidence {
		return []Alert{{
			AlertType: "low_confidence",
			Severity:  "low",
			Date:      proj.StartDate,
			Message: fmt.Sprintf(
				"Projection confidence too low (%.0f%%). Need more transaction history for reliable predictions.",
				proj.Confidence*100,
			),
		}}
	}

	var alerts []Alert

	// Projected shortfall.
	if proj.ExpectedNetCents < 0 {
		severity := "medium"
		abs := -proj.ExpectedNetCents
		if abs > 50000 {
			severity = "high"
		}
		alerts = append(alerts, Alert{
			AlertType: "shortfall",
			Severity:  severity,
			Date:      proj.EndDate,
			Message: fmt.Sprintf(
				"Projected shortfall of $%.2f in the next %d days",
				float64(abs)/100.0,
				int(proj.EndDate.Sub(proj.StartDate).Hours()/24),
			),
			AmountCents: &proj.ExpectedNetCents,
		})
	}

	// Large upcoming charges (> 20% of expected income).
	if proj.ExpectedIncomeCents > 0 {
		for i := range proj.UpcomingCharges {
			c := &proj.UpcomingCharges[i]
			pct := float64(c.ExpectedCents) / float64(proj.ExpectedIncomeCents) * 100
			if pct > 20 {
				label := c.Merchant
				if c.DisplayName != "" {
					label = c.DisplayName
				}
				alerts = append(alerts, Alert{
					AlertType: "large_charge",
					Severity:  "medium",
					Date:      c.ExpectedDate,
					Message: fmt.Sprintf(
						"Large charge expected: %s ($%.2f)",
						label, float64(c.ExpectedCents)/100.0,
					),
					AmountCents: &c.ExpectedCents,
					Merchant:    &c.Merchant,
				})
			}
		}
	}

	// Multiple charges on the same day.
	byDate := make(map[string][]int) // date → indices into UpcomingCharges
	for i, c := range proj.UpcomingCharges {
		key := c.ExpectedDate.Format("2006-01-02")
		byDate[key] = append(byDate[key], i)
	}
	for _, indices := range byDate {
		if len(indices) < 3 {
			continue
		}
		var total int64
		for _, idx := range indices {
			total += proj.UpcomingCharges[idx].ExpectedCents
		}
		date := proj.UpcomingCharges[indices[0]].ExpectedDate
		alerts = append(alerts, Alert{
			AlertType: "multiple_charges",
			Severity:  "low",
			Date:      date,
			Message: fmt.Sprintf(
				"%d charges expected on %s: $%.2f total",
				len(indices), date.Format("2006-01-02"), float64(total)/100.0,
			),
			AmountCents: &total,
		})
	}

	return alerts
}

// ---------------------------------------------------------------------------
// Charge building
// ---------------------------------------------------------------------------

func buildUpcomingCharges(db *sql.DB, today, endDate time.Time, accountFilter []string) ([]UpcomingCharge, error) {
	var charges []UpcomingCharge
	covered := make(map[string]struct{})

	// 1. Confirmed commitments (confidence = 1.0).
	commitments, err := queryConfirmedCommitments(db, "expense")
	if err != nil {
		return nil, err
	}

	for _, c := range commitments {
		merchant := c.merchantNorm
		expected := c.expectedCents
		if expected == 0 {
			// Resolve amount from recent matching transactions.
			amt, ok, err := medianCommitmentAmount(db, merchant, c.dayOfMonth, c.cadence)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			expected = amt
		}

		nextDue := computeNextDueDate(c.cadence, c.dayOfMonth, c.referenceDate, today)
		if nextDue == nil {
			continue
		}
		if nextDue.Before(today) || nextDue.After(endDate) {
			continue
		}

		charges = append(charges, UpcomingCharge{
			Merchant:      merchant,
			DisplayName:   c.name,
			ExpectedDate:  *nextDue,
			ExpectedCents: expected,
			Confidence:    1.0,
			Cadence:       c.cadence,
			IsSubscription: false,
		})
		if merchant != "" {
			covered[lower(merchant)] = struct{}{}
		}
	}

	// 2. Detected subscriptions from subscription_candidates.
	subs, err := querySubscriptionCandidates(db, accountFilter)
	if err != nil {
		return nil, err
	}
	for _, s := range subs {
		if _, ok := covered[lower(s.merchant)]; ok {
			continue
		}

		next := nextMonthlyDate(s.lastSeen, today)
		switch s.cadence {
		case "annual":
			next = s.lastSeen.AddDate(1, 0, 0)
			for next.Before(today) {
				next = next.AddDate(1, 0, 0)
			}
		case "weekly":
			daysSince := int(today.Sub(s.lastSeen).Hours() / 24)
			advance := (7 - daysSince%7) % 7
			if advance == 0 {
				advance = 7
			}
			next = today.AddDate(0, 0, advance)
		case "biweekly":
			daysSince := int(today.Sub(s.lastSeen).Hours() / 24)
			advance := (14 - daysSince%14) % 14
			if advance == 0 {
				advance = 14
			}
			next = today.AddDate(0, 0, advance)
		case "quarterly":
			next = s.lastSeen.AddDate(0, 0, 90)
			for next.Before(today) {
				next = next.AddDate(0, 0, 90)
			}
		}

		if next.Before(today) || next.After(endDate) {
			continue
		}

		conf := 0.7
		if s.isKnown {
			conf = 0.9
		}
		lastSeen := s.lastSeen
		charges = append(charges, UpcomingCharge{
			Merchant:       s.merchant,
			DisplayName:    s.displayName,
			ExpectedDate:   next,
			ExpectedCents:  s.amountCents,
			Confidence:     conf,
			Cadence:        s.cadence,
			IsSubscription: true,
			LastChargeDate: &lastSeen,
		})
	}

	return charges, nil
}

// ---------------------------------------------------------------------------
// Income / spending estimation
// ---------------------------------------------------------------------------

func estimateIncome(db *sql.DB, daysForward int, accountFilter []string) (int64, error) {
	// Prefer confirmed income commitments when they exist.
	incomeCommitments, err := queryConfirmedCommitments(db, "income")
	if err != nil {
		return 0, err
	}

	cadenceMultipliers := map[string]float64{
		"monthly":   1.0,
		"weekly":    4.33,
		"biweekly":  2.17,
		"quarterly": 1.0 / 3.0,
		"annual":    1.0 / 12.0,
	}

	if len(incomeCommitments) > 0 {
		var monthlySum int64
		for _, c := range incomeCommitments {
			if c.cadence == "one_time" || c.expectedCents == 0 {
				continue
			}
			mult := cadenceMultipliers[c.cadence]
			if mult == 0 {
				mult = 1.0
			}
			monthlySum += int64(float64(c.expectedCents) * mult)
		}
		if monthlySum > 0 {
			return int64(float64(monthlySum) * float64(daysForward) / 30.0), nil
		}
	}

	// Fall back to rolling 3-month income average from transactions.
	// Only count transactions from income-marked merchants.
	cutoff := startOfMonth(time.Now().UTC()).AddDate(0, -3, 0)
	endMonth := startOfMonth(time.Now().UTC())

	var totalIncome int64
	// Use EXISTS rather than an INNER JOIN so a deposit matching multiple income
	// rules is counted once (no fan-out). Guard against blank merchant_pattern,
	// which would otherwise become LIKE '%%' and match every positive txn.
	rows, err := db.Query(`
		SELECT COALESCE(SUM(t.amount_cents), 0),
		       JULIANDAY(?) - JULIANDAY(?) AS total_days
		FROM transactions t
		WHERE t.posted_at >= ? AND t.posted_at < ?
		  AND t.amount_cents > 0
		  AND COALESCE(t.pending, 0) = 0
		  AND EXISTS (
		      SELECT 1 FROM merchant_rules mr
		      WHERE mr.rule_type = 'income'
		        AND TRIM(COALESCE(mr.merchant_pattern, '')) <> ''
		        AND TRIM(LOWER(COALESCE(NULLIF(t.merchant,''), NULLIF(t.description,''), '')))
		            LIKE '%' || mr.merchant_pattern || '%'
		  )`,
		endMonth.Format("2006-01-02"), cutoff.Format("2006-01-02"),
		cutoff.Format("2006-01-02"), endMonth.Format("2006-01-02"),
	)
	if err != nil {
		// Table may not exist yet; return 0 gracefully.
		return 0, nil
	}
	defer rows.Close()

	if rows.Next() {
		var totalDays float64
		if err := rows.Scan(&totalIncome, &totalDays); err != nil || totalDays <= 0 {
			return 0, nil
		}
		daily := float64(totalIncome) / totalDays
		return int64(daily * float64(daysForward)), nil
	}
	return 0, nil
}

func estimateFlexibleSpending(db *sql.DB, daysForward int, accountFilter []string, excludeMerchants map[string]struct{}) (variable, discretionary int64, err error) {
	// Rolling 3-month average for negative-amount transactions, split by
	// a simple heuristic: merchants seen ≥4 times/month → variable,
	// otherwise → discretionary. This is a best-effort estimate.
	//
	// Transactions whose normalized merchant is already represented by a fixed
	// commitment or detected subscription (excludeMerchants) are skipped so
	// those fixed charges are not double-counted against expectedFixed.
	cutoff := startOfMonth(time.Now().UTC()).AddDate(0, -3, 0)
	endMonth := startOfMonth(time.Now().UTC())

	query := `
		SELECT ABS(amount_cents),
		       TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), ''))) AS merchant_norm
		FROM transactions
		WHERE posted_at >= ? AND posted_at < ?
		  AND amount_cents < 0
		  AND COALESCE(pending, 0) = 0`
	args := []any{cutoff.Format("2006-01-02"), endMonth.Format("2006-01-02")}

	if len(accountFilter) > 0 {
		placeholders := buildPlaceholders(len(accountFilter))
		query += " AND account_id IN (" + placeholders + ")"
		for _, id := range accountFilter {
			args = append(args, id)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return 0, 0, nil
	}
	defer rows.Close()

	var total int64
	var count int
	for rows.Next() {
		var amt int64
		var merchantNorm string
		if scanErr := rows.Scan(&amt, &merchantNorm); scanErr != nil {
			continue
		}
		if _, skip := excludeMerchants[normalizeMerchant(merchantNorm)]; skip {
			continue
		}
		total += amt
		count++
	}
	if count == 0 || daysForward <= 0 {
		return 0, 0, rows.Err()
	}

	// 90-day period → monthly average → scale to daysForward.
	monthly := total / 3
	scaled := int64(float64(monthly) * float64(daysForward) / 30.0)

	// Split 40/60 between variable and discretionary as a rough estimate.
	variable = int64(float64(scaled) * 0.4)
	discretionary = scaled - variable
	return variable, discretionary, rows.Err()
}

// ---------------------------------------------------------------------------
// Next-due-date computation
// ---------------------------------------------------------------------------

// computeNextDueDate returns the next occurrence of a commitment after today,
// or nil when it cannot be determined.
func computeNextDueDate(cadence string, dayOfMonth *int, referenceDate *time.Time, today time.Time) *time.Time {
	switch cadence {
	case "monthly":
		anchor := 1
		if dayOfMonth != nil {
			anchor = *dayOfMonth
		} else if referenceDate != nil {
			anchor = referenceDate.Day()
		}
		// Clamp to valid day.
		y, m, _ := today.Date()
		lastDay := daysInMonth(y, m)
		if anchor > lastDay {
			anchor = lastDay
		}
		anchorDate := time.Date(y, m, anchor, 0, 0, 0, 0, time.UTC)
		// If this month's occurrence is still on or after today, it is the next
		// due date; otherwise roll forward to a later month.
		if !anchorDate.Before(today) {
			return &anchorDate
		}
		next := nextMonthlyDate(anchorDate, today)
		return &next

	case "one_time":
		if referenceDate == nil {
			return nil
		}
		if !referenceDate.Before(today) {
			t := *referenceDate
			return &t
		}
		return nil

	case "weekly":
		if referenceDate == nil {
			return nil
		}
		daysAhead := (int(referenceDate.Weekday()) - int(today.Weekday()) + 7) % 7
		t := today.AddDate(0, 0, daysAhead)
		return &t

	case "biweekly":
		if referenceDate == nil {
			return nil
		}
		daysSinceRef := int(today.Sub(*referenceDate).Hours() / 24)
		n := daysSinceRef/14 + 1
		if n < 1 {
			n = 1
		}
		t := referenceDate.AddDate(0, 0, 14*n)
		for !t.After(today) {
			t = t.AddDate(0, 0, 14)
		}
		return &t

	case "quarterly":
		if referenceDate == nil {
			return nil
		}
		daysSinceRef := int(today.Sub(*referenceDate).Hours() / 24)
		n := daysSinceRef/91 + 1
		if n < 1 {
			n = 1
		}
		t := referenceDate.AddDate(0, 0, 91*n)
		for !t.After(today) {
			t = t.AddDate(0, 0, 91)
		}
		return &t

	case "annual":
		if referenceDate == nil {
			return nil
		}
		daysSinceRef := int(today.Sub(*referenceDate).Hours() / 24)
		n := daysSinceRef/365 + 1
		if n < 1 {
			n = 1
		}
		t := referenceDate.AddDate(0, 0, 365*n)
		for !t.After(today) {
			t = t.AddDate(0, 0, 365)
		}
		return &t
	}
	return nil
}

// nextMonthlyDate returns the next occurrence of lastDate's day-of-month
// that is on or after the after date.
func nextMonthlyDate(lastDate, after time.Time) time.Time {
	y := lastDate.Year()
	m := lastDate.Month() + 1
	if m > 12 {
		m = 1
		y++
	}
	day := lastDate.Day()
	last := daysInMonth(y, m)
	if day > last {
		day = last
	}
	candidate := time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
	for candidate.Before(after) {
		m++
		if m > 12 {
			m = 1
			y++
		}
		last = daysInMonth(y, m)
		d := lastDate.Day()
		if d > last {
			d = last
		}
		candidate = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return candidate
}

// ---------------------------------------------------------------------------
// DB queries
// ---------------------------------------------------------------------------

type commitment struct {
	name          string
	merchantNorm  string
	expectedCents int64
	cadence       string
	dayOfMonth    *int
	referenceDate *time.Time
}

func queryConfirmedCommitments(db *sql.DB, direction string) ([]commitment, error) {
	rows, err := db.Query(`
		SELECT name, COALESCE(merchant_norm,''), COALESCE(expected_cents,0),
		       cadence, day_of_month, reference_date
		FROM commitments
		WHERE confirmed = 1 AND direction = ?`,
		direction,
	)
	if err != nil {
		return nil, fmt.Errorf("projections: query commitments: %w", err)
	}
	defer rows.Close()

	var result []commitment
	for rows.Next() {
		var c commitment
		var dom sql.NullInt64
		var refDate sql.NullString
		if err := rows.Scan(&c.name, &c.merchantNorm, &c.expectedCents, &c.cadence, &dom, &refDate); err != nil {
			return nil, fmt.Errorf("projections: scan commitment: %w", err)
		}
		if dom.Valid {
			v := int(dom.Int64)
			c.dayOfMonth = &v
		}
		if refDate.Valid && refDate.String != "" {
			t, _ := time.Parse("2006-01-02", refDate.String)
			c.referenceDate = &t
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func medianCommitmentAmount(db *sql.DB, merchantNorm string, _ *int, _ string) (int64, bool, error) {
	// Sample the most recent charges, then compute a true numeric median.
	// A wider sample than 3 gives a more stable estimate against outliers.
	rows, err := db.Query(`
		SELECT ABS(amount_cents) FROM transactions
		WHERE TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), ''))) = ?
		  AND amount_cents < 0
		  AND COALESCE(pending, 0) = 0
		ORDER BY posted_at DESC
		LIMIT 12`,
		lower(merchantNorm),
	)
	if err != nil {
		return 0, false, fmt.Errorf("projections: median amount query: %w", err)
	}
	defer rows.Close()

	var amounts []int64
	for rows.Next() {
		var a int64
		if err := rows.Scan(&a); err != nil {
			continue
		}
		amounts = append(amounts, a)
	}
	if len(amounts) == 0 {
		return 0, false, rows.Err()
	}

	// Numeric median: sort by amount, take the middle (or mean of the two
	// middle values for an even-sized sample).
	sort.Slice(amounts, func(i, j int) bool { return amounts[i] < amounts[j] })
	n := len(amounts)
	var median int64
	if n%2 == 1 {
		median = amounts[n/2]
	} else {
		median = (amounts[n/2-1] + amounts[n/2]) / 2
	}
	return median, true, rows.Err()
}

type subscriptionCandidate struct {
	merchant    string
	displayName string
	amountCents int64
	cadence     string
	lastSeen    time.Time
	isKnown     bool
}

func querySubscriptionCandidates(db *sql.DB, accountFilter []string) ([]subscriptionCandidate, error) {
	query := `
		SELECT merchant_norm,
		       COALESCE(merchant_norm,'') AS display_name,
		       monthly_cost_estimate_cents,
		       CASE WHEN interval_days <= 8 THEN 'weekly'
		            WHEN interval_days <= 16 THEN 'biweekly'
		            WHEN interval_days <= 35 THEN 'monthly'
		            WHEN interval_days <= 100 THEN 'quarterly'
		            ELSE 'annual' END AS cadence,
		       last_seen_at,
		       0 AS is_known
		FROM subscription_candidates
		WHERE confidence >= 0.5`
	var args []any

	if len(accountFilter) > 0 {
		placeholders := buildPlaceholders(len(accountFilter))
		query += " AND account_id IN (" + placeholders + ")"
		for _, id := range accountFilter {
			args = append(args, id)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("projections: query subscriptions: %w", err)
	}
	defer rows.Close()

	var result []subscriptionCandidate
	for rows.Next() {
		var s subscriptionCandidate
		var lastSeenStr string
		var isKnownInt int
		if err := rows.Scan(
			&s.merchant, &s.displayName, &s.amountCents, &s.cadence,
			&lastSeenStr, &isKnownInt,
		); err != nil {
			return nil, fmt.Errorf("projections: scan subscription: %w", err)
		}
		s.lastSeen, _ = time.Parse("2006-01-02", lastSeenStr[:10])
		s.isKnown = isKnownInt == 1
		result = append(result, s)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func startOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

func daysInMonth(year int, month time.Month) int {
	// First day of next month minus one day.
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// normalizeMerchant trims surrounding whitespace and lowercases a merchant
// label so commitment/subscription identifiers compare equal to the
// TRIM(LOWER(...)) merchant_norm computed in transaction queries.
func normalizeMerchant(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func lower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}

func sortChargesByDate(charges []UpcomingCharge) {
	for i := 1; i < len(charges); i++ {
		for j := i; j > 0 && charges[j].ExpectedDate.Before(charges[j-1].ExpectedDate); j-- {
			charges[j], charges[j-1] = charges[j-1], charges[j]
		}
	}
}

// buildPlaceholders returns a comma-separated string of n "?" placeholders
// for use in SQL IN clauses, e.g. "?, ?, ?".
func buildPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*3-2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',', ' ')
		}
		b = append(b, '?')
	}
	return string(b)
}
