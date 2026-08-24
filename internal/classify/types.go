// Package classify defines the canonical reporting types for the Global Truth Contract.
//
// TRUTH CONTRACT (NON-NEGOTIABLE):
//  1. Transaction types are mutually exclusive: INCOME, EXPENSE, TRANSFER, REFUND, CREDIT_OTHER
//  2. Positive amount != income. Positive is a CREDIT until proven otherwise.
//  3. Transfers do not affect net spend/income; matched transfers net to 0.
//  4. Pending is excluded from posted totals by default.
//  5. All internal date ranges are end-exclusive: start <= posted_at < end_exclusive.
//  6. All money is stored as integer cents; never floats.
//  7. Web/CLI/Exports must use ONE canonical report engine; never recompute totals separately.
//  8. Advice/recommendations are gated by integrity score; if integrity is low, show resolution tasks.
package classify

import "time"

// TransactionType is a mutually exclusive classification for every transaction.
// Every transaction is exactly ONE of these types.
type TransactionType int

const (
	// TxnIncome represents proven income: payroll, user-marked income.
	TxnIncome TransactionType = iota
	// TxnExpense represents an outflow (negative amount).
	TxnExpense
	// TxnTransfer represents a matched internal transfer (both legs identified).
	TxnTransfer
	// TxnRefund represents a matched refund to a prior expense.
	TxnRefund
	// TxnCreditOther represents an unclassified positive — NOT assumed income.
	TxnCreditOther
)

// TransferStatus is the pairing state of a transfer transaction.
type TransferStatus int

const (
	// TransferMatched means both legs are identified and paired.
	TransferMatched TransferStatus = iota
	// TransferUnmatched means only one leg was found; needs resolution.
	TransferUnmatched
	// TransferPendingMatch means a potential match is awaiting confirmation.
	TransferPendingMatch
)

// PendingStatus distinguishes posted from pending transactions.
type PendingStatus int

const (
	// StatusPosted means the transaction is confirmed and settled.
	StatusPosted PendingStatus = iota
	// StatusPending means the transaction is authorized but not yet posted.
	StatusPending
)

// SpendingBucket is the spending categorization used for financial planning.
// These are NOT equivalent to "recurring" in the legacy sense.
type SpendingBucket int

const (
	// BucketFixedObligations covers subscription/utility cadence — predictable outflows.
	BucketFixedObligations SpendingBucket = iota
	// BucketVariableEssentials covers habitual spending (groceries, gas) — irregular but necessary.
	BucketVariableEssentials
	// BucketDiscretionary covers one-off or optional spending.
	BucketDiscretionary
	// BucketOneOffs covers truly one-time items (annual fees, large purchases).
	BucketOneOffs
)

// IntegrityFlag signals a data quality issue that needs resolution.
type IntegrityFlag int

const (
	// FlagUnmatchedTransfer means a transfer has only one leg identified.
	FlagUnmatchedTransfer IntegrityFlag = iota
	// FlagUnclassifiedCredit means a CREDIT_OTHER transaction needs resolution.
	FlagUnclassifiedCredit
	// FlagDuplicateSuspected means a potential duplicate transaction was detected.
	FlagDuplicateSuspected
	// FlagReconciliationFailed means the statement balance does not match.
	FlagReconciliationFailed
	// FlagFutureDataLeak means pattern detection used future data.
	FlagFutureDataLeak
	// FlagPendingInTotals means pending transactions were mixed with posted totals.
	FlagPendingInTotals
	// FlagEmptyAccountFilter means account_filter explicitly returned empty results.
	// This is informational — not an error.
	FlagEmptyAccountFilter
)

// integrityPenalties maps each flag to the score penalty it carries.
// Score runs from 0.0 (broken) to 1.0 (perfect). Below 0.8 gates recommendations.
var integrityPenalties = map[IntegrityFlag]float64{
	FlagUnmatchedTransfer:    0.05,
	FlagUnclassifiedCredit:   0.10,
	FlagDuplicateSuspected:   0.05,
	FlagReconciliationFailed: 0.20,
	FlagFutureDataLeak:       0.15,
	FlagPendingInTotals:      0.10,
	FlagEmptyAccountFilter:   0.00, // Informational only.
}

// ClassificationReason is the audit trail for why a transaction was classified.
type ClassificationReason struct {
	// PrimaryCode is the classification signal, e.g. "USER_OVERRIDE", "PAYROLL_PATTERN", "TRANSFER_PAIR".
	PrimaryCode string
	// Confidence is a value from 0.0 to 1.0.
	Confidence float64
	// Evidence holds supporting detail strings.
	Evidence []string
	// MatchedTxnID is the fingerprint of the paired transaction for refunds/transfers.
	// Empty string means no pairing.
	MatchedTxnID string
}

// ClassifiedTransaction is a transaction with full classification metadata.
type ClassifiedTransaction struct {
	Fingerprint    string
	AccountID      string
	PostedAt       time.Time
	AmountCents    int64
	MerchantNorm   string
	RawDescription string

	// Classification
	TxnType        TransactionType
	SpendingBucket *SpendingBucket // nil for non-expense types
	CategoryID     string          // Expense category; empty if not applicable

	// Status
	PendingStatus PendingStatus
	Reason        ClassificationReason

	// Transfer and refund linking
	TransferGroupID string
	TransferStatus  *TransferStatus // nil when not a transfer
	MatchedRefundOf string          // Fingerprint of the expense being refunded; empty if none
}

// PeriodTotals holds aggregated amounts for a reporting period.
// All amounts are in integer cents.
type PeriodTotals struct {
	// Income
	IncomeCents int64

	// Expenses by spending bucket
	FixedObligationsCents   int64
	VariableEssentialsCents int64
	DiscretionaryCents      int64
	OneOffsCents            int64

	// Credits (NOT income)
	RefundsCents     int64 // Matched to a prior expense
	CreditsOtherCents int64 // Unclassified positive

	// Transfers (should net to 0 for a matched pair)
	TransfersInCents  int64
	TransfersOutCents int64
}

// TotalExpensesCents returns the total outflow across all spending buckets,
// excluding transfers.
func (t *PeriodTotals) TotalExpensesCents() int64 {
	return t.FixedObligationsCents +
		t.VariableEssentialsCents +
		t.DiscretionaryCents +
		t.OneOffsCents
}

// NetSpendCents returns total expenses minus refunds.
func (t *PeriodTotals) NetSpendCents() int64 {
	return t.TotalExpensesCents() - t.RefundsCents
}

// NetCents returns income plus refunds minus total expenses.
func (t *PeriodTotals) NetCents() int64 {
	return t.IncomeCents + t.RefundsCents - t.TotalExpensesCents()
}

// TransferBalanceCents returns the difference between transfers in and out.
// A value of 0 indicates all transfers are matched.
func (t *PeriodTotals) TransferBalanceCents() int64 {
	return t.TransfersInCents - t.TransfersOutCents
}

// IntegrityReport is the data quality assessment for a report period.
type IntegrityReport struct {
	Flags                    []IntegrityFlag
	UnmatchedTransferCount   int
	UnclassifiedCreditCount  int
	UnclassifiedCreditCents  int64
	DuplicateSuspectCount    int
	ReconciliationDeltaCents int64
}

// Score returns the integrity score from 0.0 (broken) to 1.0 (perfect).
// Scores below 0.8 should gate recommendations.
func (ir *IntegrityReport) Score() float64 {
	if len(ir.Flags) == 0 {
		return 1.0
	}
	var totalPenalty float64
	for _, f := range ir.Flags {
		if p, ok := integrityPenalties[f]; ok {
			totalPenalty += p
		} else {
			totalPenalty += 0.05 // Default penalty for unknown flags.
		}
	}
	score := 1.0 - totalPenalty
	if score < 0.0 {
		return 0.0
	}
	return score
}

// IsActionable reports whether recommendations should be shown to the user.
//
// Two gates apply (ADA-111):
//  1. A critical flag (FlagReconciliationFailed) hard-suppresses advice
//     regardless of score: if a statement does not reconcile, the underlying
//     numbers are in doubt and no recommendation can be trusted.
//  2. The numeric threshold is strictly greater than 0.8. A single
//     reconciliation-sized penalty (0.20) lands a report at exactly 0.80; the
//     old `>= 0.8` boundary left that case actionable. Strict `>` closes it.
func (ir *IntegrityReport) IsActionable() bool {
	for _, f := range ir.Flags {
		if f == FlagReconciliationFailed {
			return false
		}
	}
	return ir.Score() > 0.8
}

// Report is the canonical output for a reporting period.
// This is the ONLY structure used by web, CLI, and exports — totals must
// never be recomputed outside this struct.
type Report struct {
	// Period metadata
	PeriodLabel string
	StartDate   time.Time
	EndDate     time.Time // End-exclusive: start <= posted_at < EndDate

	// Aggregated totals
	Totals PeriodTotals

	// Full transaction detail
	Transactions []ClassifiedTransaction

	// Data quality
	Integrity IntegrityReport

	// Versioning for reproducibility
	ClassifierVersion string
	ReportVersion     string
	ReportHash        string // SHA-256 of canonical JSON; empty if not yet computed
	SnapshotID        string // Data snapshot identifier; empty if not applicable

	// Counts
	TransactionCount int
	PendingCount     int // Excluded from totals
}

// HasUnresolvedIssues reports whether the report contains flags needing user attention.
func (r *Report) HasUnresolvedIssues() bool {
	return len(r.Integrity.Flags) > 0
}

// ResolutionTask describes a specific action the user should take to resolve a data issue.
type ResolutionTask struct {
	TaskType       string   // "CLASSIFY_CREDIT", "MATCH_TRANSFER", "RECONCILE"
	Description    string
	Priority       int      // 1 = highest priority
	AffectedCents  int64
	AffectedTxnIDs []string
}

// Recommendation is a gated financial recommendation.
// Only shown when IntegrityReport.IsActionable() returns true.
type Recommendation struct {
	Title                  string
	EvidenceSummary        string
	ImpactCentsPerMonth    int64
	Confidence             float64
	NextSteps              []string
	SupportingTxnIDs       []string
}
