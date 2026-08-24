// Canonical report engine for the classify package.
//
// TRUTH CONTRACT (NON-NEGOTIABLE):
//  1. Transaction types are mutually exclusive: INCOME, EXPENSE, TRANSFER, REFUND, CREDIT_OTHER.
//  2. Positive amount != income. Positive is a CREDIT until proven otherwise.
//  3. Transfers do not affect net spend/income; matched transfers net to 0.
//  4. Pending is excluded from posted totals by default.
//  5. All internal date ranges are end-exclusive: start <= posted_at < endDate.
//  6. All money is stored as integer cents; never floats.
//  7. Web/CLI/Exports must use ONE canonical report engine; never recompute totals separately.
//  8. Advice/recommendations are gated by integrity score; if integrity is low, show resolution tasks.
package classify

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/categorize"
	"github.com/arclighteng/fin-go/internal/reconciliation"
)

// ClassifierVersion and ReportVersion are bumped whenever classification logic
// or the Report shape changes, making it possible to detect stale cached reports.
const ClassifierVersion = "1.0.0"
const ReportVersion = "1.0.0"

// ---------------------------------------------------------------------------
// Report engine
// ---------------------------------------------------------------------------

// txnRow holds raw data for a single transaction as returned by the main query.
type txnRow struct {
	Fingerprint    string
	AccountID      string
	PostedAt       string
	AmountCents    int64
	Pending        int
	MerchantNorm   string
	RawDescription string
}

// ReportPeriod generates the canonical Report for [startDate, endDate).
//
// This is THE function all callers must use. Web, CLI, and exports must never
// recompute totals independently.
//
// accountFilter semantics:
//   - nil    → all accounts
//   - []     → empty filter; returns an empty report with FlagEmptyAccountFilter
//   - [ids…] → restrict to named accounts
func ReportPeriod(db *sql.DB, startDate, endDate time.Time, includePending bool, accountFilter []string) *Report {
	if accountFilter != nil && len(accountFilter) == 0 {
		return emptyReport(startDate, endDate)
	}

	// 1. Load override registry.
	registry := NewOverrideRegistry()
	if err := registry.LoadFromDB(db); err != nil {
		// Non-fatal: proceed without overrides rather than producing no report.
		registry = NewOverrideRegistry()
	}

	// 2. Category overrides (merchant_norm → category_id).
	catOverrides := loadCategoryOverrides(db)

	// 3. Transfer pairs.
	xferResult := DetectTransferPairs(db, startDate, endDate, 3, 300)
	pairedFPs := xferResult.PairedFingerprints()

	// If accountFilter is set, only hide a transfer when both legs are visible.
	if accountFilter != nil {
		acctSet := make(map[string]bool, len(accountFilter))
		for _, id := range accountFilter {
			acctSet[id] = true
		}
		filtered := make(map[string]bool)
		for i := range xferResult.MatchedPairs {
			p := &xferResult.MatchedPairs[i]
			if acctSet[p.Outflow.AccountID] && acctSet[p.Inflow.AccountID] {
				filtered[p.Outflow.Fingerprint] = true
				filtered[p.Inflow.Fingerprint] = true
			}
		}
		pairedFPs = filtered
	}

	// 4. Refund matches.
	refundResult := DetectRefundMatches(db, startDate, endDate, 90, 5.0)

	// 5. CC accounts.
	ccAccounts := getCCAccountIDs(db)

	// 6. Pending count (always counts regardless of includePending).
	pendingCount := countPending(db, startDate, endDate, accountFilter)

	// 7. Query main transaction rows.
	rows := queryTransactions(db, startDate, endDate, includePending, accountFilter)

	// 8. Classify and tally.
	var totals PeriodTotals
	var transactions []ClassifiedTransaction
	var integrityFlags []IntegrityFlag
	var unclassifiedCreditCount int
	var unclassifiedCreditCents int64

	for _, row := range rows {
		isPending := row.Pending == 1
		if isPending && !includePending {
			continue
		}

		isCC := ccAccounts[row.AccountID]
		isTransferPaired := pairedFPs[row.Fingerprint]
		matchedRefundOf := refundResult.ExpenseForRefund(row.Fingerprint)

		result := ClassifyTransaction(
			row.AmountCents,
			row.MerchantNorm,
			isCC,
			nil, // pattern detection not wired at report time
			registry,
			row.Fingerprint,
			isTransferPaired,
			matchedRefundOf,
		)

		// Category (for expenses and refunds).
		categoryID := ""
		if result.TxnType == TxnExpense || result.TxnType == TxnRefund {
			if ov, ok := catOverrides[row.MerchantNorm]; ok {
				categoryID = ov
			} else {
				categoryID, _ = categorize.CategorizeMerchant(row.MerchantNorm, row.RawDescription)
			}
		}

		postedAt, _ := parseDate(row.PostedAt)

		txn := ClassifiedTransaction{
			Fingerprint:    row.Fingerprint,
			AccountID:      row.AccountID,
			PostedAt:       postedAt,
			AmountCents:    row.AmountCents,
			MerchantNorm:   row.MerchantNorm,
			RawDescription: row.RawDescription,
			TxnType:        result.TxnType,
			SpendingBucket: result.SpendingBucket,
			CategoryID:     categoryID,
			Reason:         result.Reason,
		}

		if isPending {
			txn.PendingStatus = StatusPending
		}

		if isTransferPaired {
			txn.TransferGroupID = xferResult.PairID(row.Fingerprint)
			matched := TransferMatched
			txn.TransferStatus = &matched
		} else if result.TxnType == TxnTransfer {
			unmatched := TransferUnmatched
			txn.TransferStatus = &unmatched
		}

		if matchedRefundOf != "" {
			txn.MatchedRefundOf = matchedRefundOf
		}

		transactions = append(transactions, txn)

		// Tally by type.
		switch result.TxnType {
		case TxnIncome:
			totals.IncomeCents += row.AmountCents

		case TxnExpense:
			absAmt := row.AmountCents
			if absAmt < 0 {
				absAmt = -absAmt
			}
			if result.SpendingBucket != nil {
				switch *result.SpendingBucket {
				case BucketFixedObligations:
					totals.FixedObligationsCents += absAmt
				case BucketVariableEssentials:
					totals.VariableEssentialsCents += absAmt
				case BucketOneOffs:
					totals.OneOffsCents += absAmt
				default: // BucketDiscretionary
					totals.DiscretionaryCents += absAmt
				}
			} else {
				totals.DiscretionaryCents += absAmt
			}

		case TxnTransfer:
			if row.AmountCents > 0 {
				totals.TransfersInCents += row.AmountCents
			} else {
				totals.TransfersOutCents += -row.AmountCents
			}

		case TxnRefund:
			totals.RefundsCents += row.AmountCents

		case TxnCreditOther:
			totals.CreditsOtherCents += row.AmountCents
			unclassifiedCreditCount++
			unclassifiedCreditCents += row.AmountCents
		}
	}

	// Build integrity flags.
	if unclassifiedCreditCount > 0 {
		integrityFlags = append(integrityFlags, FlagUnclassifiedCredit)
	}
	if xferResult.HasUnmatched() {
		integrityFlags = append(integrityFlags, FlagUnmatchedTransfer)
	}
	if includePending && pendingCount > 0 {
		integrityFlags = append(integrityFlags, FlagPendingInTotals)
	}

	// Duplicate suspicion (ADA-111): distinct fingerprints that share the same
	// (account, posted date, amount, merchant) are almost always double-imports.
	// Same-fingerprint rows are already deduped upstream, so any collision here
	// is a genuine review candidate.
	dupCount := detectDuplicateSuspects(transactions)
	if dupCount > 0 {
		integrityFlags = append(integrityFlags, FlagDuplicateSuspected)
	}

	// Reconciliation (ADA-111): unresolved statement discrepancies for the
	// in-scope accounts mean the period's balances cannot be trusted, so the
	// report must say so rather than presenting clean-looking totals.
	reconDelta := reconciliationDelta(db, accountFilter)
	if reconDelta != 0 {
		integrityFlags = append(integrityFlags, FlagReconciliationFailed)
	}

	unmatchedCount := len(xferResult.UnmatchedOutflows) + len(xferResult.UnmatchedInflows)
	integrity := IntegrityReport{
		Flags:                    integrityFlags,
		UnmatchedTransferCount:   unmatchedCount,
		UnclassifiedCreditCount:  unclassifiedCreditCount,
		UnclassifiedCreditCents:  unclassifiedCreditCents,
		DuplicateSuspectCount:    dupCount,
		ReconciliationDeltaCents: reconDelta,
	}

	label := fmt.Sprintf("%s to %s", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))

	report := &Report{
		PeriodLabel:       label,
		StartDate:         startDate,
		EndDate:           endDate,
		Totals:            totals,
		Transactions:      transactions,
		Integrity:         integrity,
		ClassifierVersion: ClassifierVersion,
		ReportVersion:     ReportVersion,
		TransactionCount:  len(transactions),
		PendingCount:      pendingCount,
	}

	report.ReportHash = computeReportHash(report)
	return report
}

// ReportMonth generates a report for a specific calendar month.
func ReportMonth(db *sql.DB, year, month int, includePending bool, accountFilter []string) *Report {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	report := ReportPeriod(db, start, end, includePending, accountFilter)
	report.PeriodLabel = start.Format("Jan 2006")
	return report
}

// ReportPeriods generates reports for numPeriods consecutive periods going
// backwards from endDate (or today when endDate is nil). periodType must be
// one of "month", "quarter", or "year".
func ReportPeriods(db *sql.DB, periodType string, numPeriods int, includePending bool, accountFilter []string, endDate *time.Time) []*Report {
	var anchor time.Time
	if endDate != nil {
		anchor = *endDate
	} else {
		anchor = time.Now().UTC()
	}

	// Compute the bounds for the period containing anchor.
	var start, end time.Time
	switch strings.ToLower(periodType) {
	case "quarter":
		y, m, _ := anchor.Date()
		q := (int(m) - 1) / 3
		startMonth := time.Month(q*3 + 1)
		start = time.Date(y, startMonth, 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 3, 0)
	case "year":
		start = time.Date(anchor.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(1, 0, 0)
	default: // "month"
		y, m, _ := anchor.Date()
		start = time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, 0)
	}

	reports := make([]*Report, 0, numPeriods)
	for i := 0; i < numPeriods; i++ {
		r := ReportPeriod(db, start, end, includePending, accountFilter)
		r.PeriodLabel = formatPeriodLabel(periodType, start)
		reports = append(reports, r)

		// Step back one period.
		end = start
		switch strings.ToLower(periodType) {
		case "quarter":
			start = start.AddDate(0, -3, 0)
		case "year":
			start = start.AddDate(-1, 0, 0)
		default:
			start = start.AddDate(0, -1, 0)
		}
	}
	return reports
}

func formatPeriodLabel(periodType string, start time.Time) string {
	switch strings.ToLower(periodType) {
	case "quarter":
		q := (int(start.Month())-1)/3 + 1
		return fmt.Sprintf("Q%d %d", q, start.Year())
	case "year":
		return fmt.Sprintf("%d", start.Year())
	default:
		return start.Format("Jan 2006")
	}
}

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

// detectDuplicateSuspects counts how many transactions are exact economic
// duplicates of an earlier one in the same period: identical account, posted
// date, signed amount, and normalised merchant. The first occurrence of each
// identity is not counted; every additional copy is. Rows with an empty
// merchant are skipped to avoid flagging sparse data as duplicates.
func detectDuplicateSuspects(txns []ClassifiedTransaction) int {
	type dupKey struct {
		account  string
		date     string
		merchant string
		amount   int64
	}
	seen := make(map[dupKey]int, len(txns))
	extras := 0
	for i := range txns {
		t := &txns[i]
		if t.MerchantNorm == "" {
			continue
		}
		k := dupKey{
			account:  t.AccountID,
			date:     t.PostedAt.Format("2006-01-02"),
			merchant: t.MerchantNorm,
			amount:   t.AmountCents,
		}
		seen[k]++
		if seen[k] > 1 {
			extras++
		}
	}
	return extras
}

// reconciliationDelta returns the total absolute unresolved statement
// discrepancy (in cents) for the accounts in scope. accountFilter follows the
// same allow-list semantics as ReportPeriod: nil means all accounts. A return
// value of 0 means every in-scope account reconciles (or none has ever been
// reconciled), and no FlagReconciliationFailed is raised.
func reconciliationDelta(db *sql.DB, accountFilter []string) int64 {
	events, err := reconciliation.ListPendingDiscrepancies(db)
	if err != nil || len(events) == 0 {
		return 0
	}

	var acctSet map[string]bool
	if accountFilter != nil {
		acctSet = make(map[string]bool, len(accountFilter))
		for _, id := range accountFilter {
			acctSet[id] = true
		}
	}

	var total int64
	for i := range events {
		e := &events[i]
		if acctSet != nil && !acctSet[e.AccountID] {
			continue
		}
		d := e.DeltaCents
		if d < 0 {
			d = -d
		}
		total += d
	}
	return total
}

func loadCategoryOverrides(db *sql.DB) map[string]string {
	rows, err := db.Query("SELECT merchant_norm, category_id FROM category_overrides")
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var merchant, catID string
		if err := rows.Scan(&merchant, &catID); err != nil {
			log.Printf("loadCategoryOverrides: scan: %v", err)
			continue
		}
		out[merchant] = catID
	}
	return out
}

func getCCAccountIDs(db *sql.DB) map[string]bool {
	rows, err := db.Query("SELECT account_id FROM accounts WHERE LOWER(type) LIKE '%credit%'")
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			log.Printf("getCCAccountIDs: scan: %v", err)
			continue
		}
		out[id] = true
	}
	return out
}

func countPending(db *sql.DB, start, end time.Time, accountFilter []string) int {
	q := `
		SELECT COUNT(*) FROM transactions t
		WHERE t.posted_at >= ? AND t.posted_at < ?
		  AND COALESCE(t.pending, 0) = 1`
	args := []any{start.Format("2006-01-02"), end.Format("2006-01-02")}

	if len(accountFilter) > 0 {
		q += " AND t.account_id IN (?" + strings.Repeat(",?", len(accountFilter)-1) + ")"
		for _, id := range accountFilter {
			args = append(args, id)
		}
	}

	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		log.Printf("countPending: scan: %v", err)
	}
	return n
}

func queryTransactions(db *sql.DB, start, end time.Time, includePending bool, accountFilter []string) []txnRow {
	const base = `
		SELECT
			t.fingerprint,
			t.account_id,
			t.posted_at,
			t.amount_cents,
			COALESCE(t.pending, 0)                                                     AS pending,
			TRIM(LOWER(COALESCE(NULLIF(t.merchant,''), NULLIF(t.description,''), ''))) AS merchant_norm,
			COALESCE(t.description, t.merchant, '')                                    AS raw_description
		FROM transactions t
		WHERE t.posted_at >= ? AND t.posted_at < ?`

	args := []any{start.Format("2006-01-02"), end.Format("2006-01-02")}

	var clauses strings.Builder
	clauses.WriteString(base)

	if !includePending {
		clauses.WriteString("\n  AND COALESCE(t.pending, 0) = 0")
	}

	if len(accountFilter) > 0 {
		clauses.WriteString("\n  AND t.account_id IN (?")
		clauses.WriteString(strings.Repeat(",?", len(accountFilter)-1))
		clauses.WriteByte(')')
		for _, id := range accountFilter {
			args = append(args, id)
		}
	}

	clauses.WriteString("\n  ORDER BY t.posted_at DESC")

	rows, err := db.Query(clauses.String(), args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []txnRow
	for rows.Next() {
		var r txnRow
		if err := rows.Scan(&r.Fingerprint, &r.AccountID, &r.PostedAt, &r.AmountCents, &r.Pending, &r.MerchantNorm, &r.RawDescription); err != nil {
			log.Printf("queryTransactions: scan: %v", err)
			continue
		}
		out = append(out, r)
	}
	return out
}

// reportHashInput mirrors the Python canonical JSON shape exactly.
// Fields are declared in sorted-key order (matching json.dumps(sort_keys=True))
// so the serialised JSON is deterministic and cross-language compatible.
type reportHashInput struct {
	ClassifierVersion       string `json:"classifier_version"`
	CreditsOtherCents       int64  `json:"credits_other_cents"`
	DiscretionaryCents      int64  `json:"discretionary_cents"`
	EndDate                 string `json:"end_date"`
	FixedObligationsCents   int64  `json:"fixed_obligations_cents"`
	IncomeCents             int64  `json:"income_cents"`
	OneOffsCents            int64  `json:"one_offs_cents"`
	RefundsCents            int64  `json:"refunds_cents"`
	ReportVersion           string `json:"report_version"`
	StartDate               string `json:"start_date"`
	TransactionCount        int    `json:"transaction_count"`
	TransfersInCents        int64  `json:"transfers_in_cents"`
	TransfersOutCents       int64  `json:"transfers_out_cents"`
	VariableEssentialsCents int64  `json:"variable_essentials_cents"`
}

func computeReportHash(report *Report) string {
	// Struct field order matches sorted JSON key order, making the hash
	// deterministic across runs and compatible with the Python implementation.
	data := reportHashInput{
		ClassifierVersion:       report.ClassifierVersion,
		CreditsOtherCents:       report.Totals.CreditsOtherCents,
		DiscretionaryCents:      report.Totals.DiscretionaryCents,
		EndDate:                 report.EndDate.Format("2006-01-02"),
		FixedObligationsCents:   report.Totals.FixedObligationsCents,
		IncomeCents:             report.Totals.IncomeCents,
		OneOffsCents:            report.Totals.OneOffsCents,
		RefundsCents:            report.Totals.RefundsCents,
		ReportVersion:           report.ReportVersion,
		StartDate:               report.StartDate.Format("2006-01-02"),
		TransactionCount:        report.TransactionCount,
		TransfersInCents:        report.Totals.TransfersInCents,
		TransfersOutCents:       report.Totals.TransfersOutCents,
		VariableEssentialsCents: report.Totals.VariableEssentialsCents,
	}

	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)[:16]
}

func emptyReport(start, end time.Time) *Report {
	label := fmt.Sprintf("%s to %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	return &Report{
		PeriodLabel:       label,
		StartDate:         start,
		EndDate:           end,
		Totals:            PeriodTotals{},
		Transactions:      nil,
		Integrity:         IntegrityReport{Flags: []IntegrityFlag{FlagEmptyAccountFilter}},
		ClassifierVersion: ClassifierVersion,
		ReportVersion:     ReportVersion,
		TransactionCount:  0,
		PendingCount:      0,
		ReportHash:        "",
	}
}
