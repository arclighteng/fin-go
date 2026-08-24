// Package planner analyses historical spending by bucket and generates a
// budget plan with trend detection and financial health scoring.
//
// TRUTH CONTRACT:
//   - FIXED_OBLIGATIONS: predictable-cadence subscriptions and utilities.
//   - VARIABLE_ESSENTIALS: habitual but irregular necessities (groceries, gas).
//   - DISCRETIONARY: optional / lifestyle spending.
//   - ONE_OFFS: truly one-time purchases and annual fees.
//
// Habitual spending (e.g. groceries 6×/month) is variable essentials, NOT
// fixed obligations.
package planner

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/arclighteng/fin-go/internal/classify"
)

// BucketSummary aggregates spending for one bucket over the analysis window.
type BucketSummary struct {
	Bucket           classify.SpendingBucket
	Label            string
	Description      string
	MonthlyAvgCents  int64
	MonthlyMinCents  int64
	MonthlyMaxCents  int64
	Trend            string  // "stable", "increasing", "decreasing"
	TrendPercent     float64 // positive = increasing, negative = decreasing
	Predictability   float64 // 0.0–1.0 (lower CV = more predictable)
	MerchantCount    int
	TransactionCount int
}

// Plan is the budget analysis for a rolling window of months.
type Plan struct {
	PeriodMonths          int
	TotalMonthlyIncome    int64
	TotalMonthlySpend     int64
	NetMonthlyCents       int64
	Buckets               []BucketSummary
	SavingsRate           float64 // percentage, may be negative
	Suggestions           []string
	HealthScore           float64 // 0.0–1.0
}

// BucketDetail is a drill-down view for one bucket.
type BucketDetail struct {
	Bucket        classify.SpendingBucket
	Merchants     []MerchantRow
	MonthlyTotals []MonthTotal
}

// MerchantRow is one merchant's contribution to a bucket.
type MerchantRow struct {
	Merchant      string
	MonthlyCents  int64
	TotalCents    int64
	Count         int
	ActiveMonths  int
}

// MonthTotal is a single month's bucket total.
type MonthTotal struct {
	Month       string // "YYYY-MM"
	AmountCents int64
}

// PlanOptions controls how many months of history to analyse.
type PlanOptions struct {
	Months        int      // default 6
	AccountFilter []string // nil means all accounts
}

var bucketMeta = []struct {
	bucket      classify.SpendingBucket
	label       string
	description string
}{
	{classify.BucketFixedObligations, "Fixed Obligations", "Predictable subscriptions & utilities"},
	{classify.BucketVariableEssentials, "Variable Essentials", "Groceries, gas, medicine"},
	{classify.BucketDiscretionary, "Discretionary", "Dining, entertainment, shopping"},
	{classify.BucketOneOffs, "One-offs", "Large purchases, annual fees"},
}

// SpendingPlan analyses spending over opts.Months months and returns a Plan.
// It classifies transactions in Go using the same rules as the canonical
// classifier, without invoking an external report service.
func SpendingPlan(db *sql.DB, opts PlanOptions) (*Plan, error) {
	if opts.Months <= 0 {
		opts.Months = 6
	}

	today := time.Now().UTC()
	endDate := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	startDate := endDate.AddDate(0, -opts.Months, 0)

	overrides := classify.NewOverrideRegistry()
	if err := overrides.LoadFromDB(db); err != nil {
		return nil, fmt.Errorf("planner: load overrides: %w", err)
	}

	rows, err := queryTransactions(db, startDate, endDate, opts.AccountFilter)
	if err != nil {
		return nil, err
	}

	// Accumulate by bucket and month.
	type bucketMonthKey struct {
		bucket classify.SpendingBucket
		month  string
	}
	byMonth := make(map[bucketMonthKey]int64)
	merchantSet := make(map[classify.SpendingBucket]map[string]struct{})
	txnCount := make(map[classify.SpendingBucket]int)
	incomeByMonth := make(map[string]int64)

	for _, row := range rows {
		month := row.postedAt[:7] // "YYYY-MM"

		result := classify.ClassifyTransaction(
			row.amountCents,
			row.merchantNorm,
			false, // isCreditCardAccount: conservative default
			nil,   // pattern: not available at this layer
			overrides,
			row.fingerprint,
			false, // isTransferPaired
			"",    // matchedRefundOf
		)

		switch result.TxnType {
		case classify.TxnIncome:
			incomeByMonth[month] += row.amountCents

		case classify.TxnExpense:
			if result.SpendingBucket == nil {
				continue
			}
			b := *result.SpendingBucket
			key := bucketMonthKey{b, month}
			abs := row.amountCents
			if abs < 0 {
				abs = -abs
			}
			byMonth[key] += abs

			if merchantSet[b] == nil {
				merchantSet[b] = make(map[string]struct{})
			}
			merchantSet[b][row.merchantNorm] = struct{}{}
			txnCount[b]++
		}
	}

	// Build per-bucket monthly slices.
	type monthlyData struct {
		amounts []int64
	}
	bucketMonthly := make(map[classify.SpendingBucket]*monthlyData)
	for _, meta := range bucketMeta {
		bucketMonthly[meta.bucket] = &monthlyData{}
	}

	// Collect unique months present in data.
	monthSet := make(map[string]struct{})
	for k := range byMonth {
		monthSet[k.month] = struct{}{}
	}
	for k := range incomeByMonth {
		monthSet[k] = struct{}{}
	}
	months := sortedKeys(monthSet)

	for _, b := range []classify.SpendingBucket{
		classify.BucketFixedObligations,
		classify.BucketVariableEssentials,
		classify.BucketDiscretionary,
		classify.BucketOneOffs,
	} {
		for _, m := range months {
			amt := byMonth[bucketMonthKey{b, m}]
			bucketMonthly[b].amounts = append(bucketMonthly[b].amounts, amt)
		}
	}

	// Build BucketSummary for each bucket.
	var summaries []BucketSummary
	var totalMonthlySpend int64

	for _, meta := range bucketMeta {
		amounts := bucketMonthly[meta.bucket].amounts
		var nonZero []int64
		for _, a := range amounts {
			if a > 0 {
				nonZero = append(nonZero, a)
			}
		}

		var avg, min, max int64
		var trend string
		var trendPct float64
		var predictability float64

		if len(nonZero) > 0 {
			var sum int64
			min = nonZero[0]
			max = nonZero[0]
			for _, a := range nonZero {
				sum += a
				if a < min {
					min = a
				}
				if a > max {
					max = a
				}
			}
			// Denominator convention: monthly averages divide the window total by
			// the WINDOW LENGTH in months (opts.Months), not the count of active
			// months. This amortizes infrequent/annual charges across the window
			// (e.g. a single $1,200 charge in a 6-month window → $200/mo, not
			// $1,200/mo) and keeps spend and income on the same denominator.
			avg = sum / int64(opts.Months)
			// activeAvg is the mean over months that actually had activity; used
			// only for the predictability (coefficient-of-variation) estimate so
			// amortization does not distort its spread measure.
			activeAvg := sum / int64(len(nonZero))

			// Trend: compare recent 2 months vs older months.
			if len(nonZero) >= 3 {
				recent := avgInt64(nonZero[len(nonZero)-2:])
				older := avgInt64(nonZero[:len(nonZero)-2])
				if older > 0 {
					trendPct = (float64(recent) - float64(older)) / float64(older) * 100
					switch {
					case trendPct > 10:
						trend = "increasing"
					case trendPct < -10:
						trend = "decreasing"
					default:
						trend = "stable"
					}
				} else {
					trend = "stable"
				}
			} else {
				trend = "stable"
			}

			// Predictability via coefficient of variation.
			if len(nonZero) > 1 {
				mean := float64(activeAvg)
				var variance float64
				for _, a := range nonZero {
					d := float64(a) - mean
					variance += d * d
				}
				variance /= float64(len(nonZero))
				stdDev := math.Sqrt(variance)
				cv := stdDev / mean
				predictability = math.Max(0, 1-cv)
			} else {
				predictability = 0.5
			}

			totalMonthlySpend += avg
		} else {
			trend = "stable"
		}

		summaries = append(summaries, BucketSummary{
			Bucket:           meta.bucket,
			Label:            meta.label,
			Description:      meta.description,
			MonthlyAvgCents:  avg,
			MonthlyMinCents:  min,
			MonthlyMaxCents:  max,
			Trend:            trend,
			TrendPercent:     trendPct,
			Predictability:   predictability,
			MerchantCount:    len(merchantSet[meta.bucket]),
			TransactionCount: txnCount[meta.bucket],
		})
	}

	// Income: average over the WINDOW LENGTH in months, matching the spend
	// denominator convention above (total income in the window / opts.Months),
	// rather than dividing by the count of months that happened to have income.
	var totalIncome int64
	for _, v := range incomeByMonth {
		totalIncome += v
	}
	var monthlyIncome int64
	if opts.Months > 0 {
		monthlyIncome = totalIncome / int64(opts.Months)
	}

	netMonthly := monthlyIncome - totalMonthlySpend

	savingsRate := 0.0
	if monthlyIncome > 0 {
		savingsRate = float64(netMonthly) / float64(monthlyIncome) * 100
	}

	// Health score.
	var healthFactors []float64
	switch {
	case savingsRate >= 20:
		healthFactors = append(healthFactors, 1.0)
	case savingsRate >= 10:
		healthFactors = append(healthFactors, 0.7)
	case savingsRate >= 0:
		healthFactors = append(healthFactors, 0.4)
	default:
		healthFactors = append(healthFactors, 0.1)
	}
	for _, s := range summaries {
		if s.Bucket == classify.BucketFixedObligations {
			healthFactors = append(healthFactors, s.Predictability)
		}
	}
	healthScore := 0.5
	if len(healthFactors) > 0 {
		var sum float64
		for _, f := range healthFactors {
			sum += f
		}
		healthScore = sum / float64(len(healthFactors))
	}

	// Suggestions.
	var suggestions []string
	if savingsRate < 10 {
		suggestions = append(suggestions, fmt.Sprintf(
			"Savings rate is %.1f%%. Consider reducing discretionary spending.", savingsRate,
		))
	}
	for _, s := range summaries {
		if s.Bucket == classify.BucketDiscretionary && s.Trend == "increasing" {
			suggestions = append(suggestions, fmt.Sprintf(
				"Discretionary spending is up %.1f%% recently.", s.TrendPercent,
			))
		}
		if s.Bucket == classify.BucketOneOffs && monthlyIncome > 0 &&
			s.MonthlyAvgCents > int64(float64(monthlyIncome)*0.2) {
			suggestions = append(suggestions, "One-off spending is high. Review for unexpected large purchases.")
		}
		if s.Bucket == classify.BucketFixedObligations && s.Predictability < 0.7 {
			suggestions = append(suggestions, "Fixed obligations vary more than expected. Review subscriptions for price changes.")
		}
	}

	return &Plan{
		PeriodMonths:       opts.Months,
		TotalMonthlyIncome: monthlyIncome,
		TotalMonthlySpend:  totalMonthlySpend,
		NetMonthlyCents:    netMonthly,
		Buckets:            summaries,
		SavingsRate:        savingsRate,
		Suggestions:        suggestions,
		HealthScore:        healthScore,
	}, nil
}

// BucketDrillDown returns a detailed merchant and monthly breakdown for one
// spending bucket.
func BucketDrillDown(db *sql.DB, bucket classify.SpendingBucket, opts PlanOptions) (*BucketDetail, error) {
	if opts.Months <= 0 {
		opts.Months = 6
	}

	today := time.Now().UTC()
	endDate := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	startDate := endDate.AddDate(0, -opts.Months, 0)

	overrides := classify.NewOverrideRegistry()
	if err := overrides.LoadFromDB(db); err != nil {
		return nil, fmt.Errorf("planner: load overrides: %w", err)
	}

	rows, err := queryTransactions(db, startDate, endDate, opts.AccountFilter)
	if err != nil {
		return nil, err
	}

	merchantData := make(map[string]*struct {
		total  int64
		count  int
		months map[string]struct{}
	})
	monthlyTotals := make(map[string]int64)

	for _, row := range rows {
		if row.amountCents >= 0 {
			continue
		}
		month := row.postedAt[:7]

		result := classify.ClassifyTransaction(
			row.amountCents, row.merchantNorm, false, nil, overrides,
			row.fingerprint, false, "",
		)
		if result.SpendingBucket == nil || *result.SpendingBucket != bucket {
			continue
		}

		abs := -row.amountCents
		if merchantData[row.merchantNorm] == nil {
			merchantData[row.merchantNorm] = &struct {
				total  int64
				count  int
				months map[string]struct{}
			}{months: make(map[string]struct{})}
		}
		merchantData[row.merchantNorm].total += abs
		merchantData[row.merchantNorm].count++
		merchantData[row.merchantNorm].months[month] = struct{}{}
		monthlyTotals[month] += abs
	}

	numMonths := len(monthlyTotals)
	if numMonths == 0 {
		numMonths = 1
	}

	var merchantRows []MerchantRow
	for m, d := range merchantData {
		merchantRows = append(merchantRows, MerchantRow{
			Merchant:     m,
			MonthlyCents: d.total / int64(numMonths),
			TotalCents:   d.total,
			Count:        d.count,
			ActiveMonths: len(d.months),
		})
	}
	sort.Slice(merchantRows, func(i, j int) bool {
		return merchantRows[i].MonthlyCents > merchantRows[j].MonthlyCents
	})
	if len(merchantRows) > 50 {
		merchantRows = merchantRows[:50]
	}

	var monthList []MonthTotal
	for m, a := range monthlyTotals {
		monthList = append(monthList, MonthTotal{Month: m, AmountCents: a})
	}
	sort.Slice(monthList, func(i, j int) bool {
		return monthList[i].Month < monthList[j].Month
	})

	return &BucketDetail{
		Bucket:        bucket,
		Merchants:     merchantRows,
		MonthlyTotals: monthList,
	}, nil
}

// ProjectMonthlyBudget returns a simple forward projection of income and
// spending based on historical averages from SpendingPlan.
func ProjectMonthlyBudget(db *sql.DB, historyMonths, forwardMonths int, accountFilter []string) (map[string]any, error) {
	plan, err := SpendingPlan(db, PlanOptions{Months: historyMonths, AccountFilter: accountFilter})
	if err != nil {
		return nil, err
	}

	today := time.Now().UTC()
	var projections []map[string]any
	for i := 0; i < forwardMonths; i++ {
		futureMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).
			AddDate(0, i, 0)
		monthStr := futureMonth.Format("2006-01")

		var fixed, variable, discretionary int64
		for _, b := range plan.Buckets {
			switch b.Bucket {
			case classify.BucketFixedObligations:
				fixed = b.MonthlyAvgCents
			case classify.BucketVariableEssentials:
				variable = b.MonthlyAvgCents
			case classify.BucketDiscretionary:
				discretionary = b.MonthlyAvgCents
			}
		}

		projections = append(projections, map[string]any{
			"month":                        monthStr,
			"projected_income_cents":       plan.TotalMonthlyIncome,
			"projected_fixed_cents":        fixed,
			"projected_variable_cents":     variable,
			"projected_discretionary_cents": discretionary,
			"projected_net_cents":          plan.NetMonthlyCents,
		})
	}

	return map[string]any{
		"based_on_months": historyMonths,
		"projections":     projections,
		"assumptions": []string{
			"Income remains stable",
			"Fixed obligations stay constant",
			"Variable essentials follow historical average",
			"Discretionary spending follows historical average",
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

type txnRow struct {
	fingerprint  string
	postedAt     string
	amountCents  int64
	merchantNorm string
}

func queryTransactions(db *sql.DB, start, end time.Time, accountFilter []string) ([]txnRow, error) {
	query := `
		SELECT fingerprint, posted_at, amount_cents,
		       TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), ''))) AS merchant_norm
		FROM transactions
		WHERE posted_at >= ? AND posted_at < ?
		  AND COALESCE(pending, 0) = 0`
	args := []any{start.Format("2006-01-02"), end.Format("2006-01-02")}

	if len(accountFilter) > 0 {
		placeholders := make([]byte, 0, len(accountFilter)*2)
		for i, id := range accountFilter {
			if i > 0 {
				placeholders = append(placeholders, ',')
			}
			placeholders = append(placeholders, '?')
			args = append(args, id)
		}
		query += fmt.Sprintf(" AND account_id IN (%s)", string(placeholders))
	}
	query += " ORDER BY posted_at"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("planner: query transactions: %w", err)
	}
	defer rows.Close()

	var result []txnRow
	for rows.Next() {
		var r txnRow
		if err := rows.Scan(&r.fingerprint, &r.postedAt, &r.amountCents, &r.merchantNorm); err != nil {
			return nil, fmt.Errorf("planner: scan transaction: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func avgInt64(vals []int64) int64 {
	if len(vals) == 0 {
		return 0
	}
	var sum int64
	for _, v := range vals {
		sum += v
	}
	return sum / int64(len(vals))
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
