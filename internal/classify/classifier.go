package classify

import (
	"database/sql"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Classification Evidence Codes
// ---------------------------------------------------------------------------

// Evidence codes indicate why a transaction was classified a certain way.
const (
	// User-provided overrides (highest precedence).
	EvidenceUserOverride      = "USER_OVERRIDE"
	EvidenceUserIncomeRule    = "USER_INCOME_RULE"
	EvidenceUserNotIncomeRule = "USER_NOT_INCOME_RULE"

	// Transfer evidence.
	EvidenceTransferPairMatched  = "TRANSFER_PAIR_MATCHED"
	EvidenceTransferBankPattern  = "TRANSFER_BANK_PATTERN"
	EvidenceTransferCCPayment    = "TRANSFER_CC_PAYMENT"

	// Refund evidence.
	EvidenceRefundMatched = "REFUND_MATCHED"
	EvidenceRefundKeyword = "REFUND_KEYWORD"

	// Income evidence.
	EvidenceIncomePayroll = "INCOME_PAYROLL"

	// Defaults.
	EvidenceCreditUnclassified = "CREDIT_UNCLASSIFIED"
	EvidenceExpenseDefault     = "EXPENSE_DEFAULT"
)

// ---------------------------------------------------------------------------
// Keyword Sets
// ---------------------------------------------------------------------------
//
// Keyword lists are defined in keywords.go. This section intentionally left
// empty to avoid duplication.

// ---------------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------------

// containsKeyword reports whether text contains any of the given keywords.
// All comparisons are substring matches; callers must pass a lowercased text.
func containsKeyword(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// isStrongIncome reports whether merchantNorm contains a payroll keyword.
func isStrongIncome(merchantNorm string) bool {
	return containsKeyword(merchantNorm, payrollKeywords)
}

// isTransferPattern reports whether merchantNorm contains a transfer keyword
// (P2P apps, ACH, wire, etc.).
func isTransferPattern(merchantNorm string) bool {
	return containsKeyword(merchantNorm, transferKeywords)
}

// isBankToBank reports whether merchantNorm looks like a bank-to-bank transfer:
// at least one recognised bank name AND a directional word ("transfer"/"to"/"from").
func isBankToBank(merchantNorm string) bool {
	if !containsKeyword(merchantNorm, bankKeywords) {
		return false
	}
	return containsKeyword(merchantNorm, bankDirectionWords)
}

// isCCPayment reports whether merchantNorm looks like a credit card payment
// being made from a non-CC account.
//
// When isCCAccount is true the transaction is on the CC side (money received),
// which is classified as a transfer-in elsewhere — not a CC payment.
func isCCPayment(merchantNorm string, isCCAccount bool) bool {
	if isCCAccount {
		return false
	}
	return containsKeyword(merchantNorm, ccPaymentKeywords)
}

// hasRefundKeywords reports whether merchantNorm contains a refund keyword.
func hasRefundKeywords(merchantNorm string) bool {
	return containsKeyword(merchantNorm, refundKeywords)
}

// ---------------------------------------------------------------------------
// Classification Result
// ---------------------------------------------------------------------------

// ClassificationResult is the outcome of classifying a single transaction.
type ClassificationResult struct {
	// TxnType is the mutually exclusive classification.
	TxnType TransactionType
	// Reason is the audit trail explaining the classification.
	Reason ClassificationReason
	// SpendingBucket is only set when TxnType == TxnExpense.
	SpendingBucket *SpendingBucket
}

// IsIncome returns true when the transaction was classified as income.
func (r ClassificationResult) IsIncome() bool { return r.TxnType == TxnIncome }

// IsExpense returns true when the transaction was classified as an expense.
func (r ClassificationResult) IsExpense() bool { return r.TxnType == TxnExpense }

// IsTransfer returns true when the transaction was classified as a transfer.
func (r ClassificationResult) IsTransfer() bool { return r.TxnType == TxnTransfer }

// IsRefund returns true when the transaction was classified as a refund.
func (r ClassificationResult) IsRefund() bool { return r.TxnType == TxnRefund }

// IsCreditOther returns true when the transaction was classified as unclassified credit.
func (r ClassificationResult) IsCreditOther() bool { return r.TxnType == TxnCreditOther }

// ---------------------------------------------------------------------------
// User Overrides
// ---------------------------------------------------------------------------

// UserOverride is a user-provided classification override for a transaction
// or group of transactions matching a merchant pattern.
type UserOverride struct {
	// Fingerprint identifies a specific transaction override.
	// Empty when the override is merchant-pattern based.
	Fingerprint string
	// MerchantPattern is a substring matched against merchant_norm.
	// Empty when the override is fingerprint based.
	MerchantPattern string
	// TargetType is the forced classification type.
	// nil means the override only carries an income hint (see IsIncome).
	TargetType *TransactionType
	// IsIncome is a tri-state income hint: nil = no hint, true = income, false = not-income.
	// Only used when TargetType is nil.
	IsIncome *bool
}

// OverrideRegistry holds user-provided classification overrides loaded from the DB.
//
// Overrides are checked in order:
//  1. Fingerprint-specific override (exact transaction match)
//  2. Merchant pattern override (all transactions whose merchant_norm contains the pattern)
//  3. Income / not-income merchant hints
type OverrideRegistry struct {
	fingerprintOverrides map[string]UserOverride
	merchantOverrides    []UserOverride
	incomeMerchants      map[string]bool
	excludedMerchants    map[string]bool
}

// NewOverrideRegistry returns an initialised, empty OverrideRegistry.
func NewOverrideRegistry() *OverrideRegistry {
	return &OverrideRegistry{
		fingerprintOverrides: make(map[string]UserOverride),
		merchantOverrides:    nil,
		incomeMerchants:      make(map[string]bool),
		excludedMerchants:    make(map[string]bool),
	}
}

// AddFingerprintOverride records an explicit type override for a single transaction.
func (or *OverrideRegistry) AddFingerprintOverride(fingerprint string, targetType TransactionType) {
	tt := targetType
	or.fingerprintOverrides[fingerprint] = UserOverride{
		Fingerprint: fingerprint,
		TargetType:  &tt,
	}
}

// AddMerchantTypeOverride records a type override for every transaction whose
// merchant_norm contains merchantPattern (case-insensitive substring).
func (or *OverrideRegistry) AddMerchantTypeOverride(merchantPattern string, targetType TransactionType) {
	tt := targetType
	or.merchantOverrides = append(or.merchantOverrides, UserOverride{
		MerchantPattern: strings.ToLower(merchantPattern),
		TargetType:      &tt,
	})
}

// AddIncomeMerchant marks a merchant pattern as an income source.
func (or *OverrideRegistry) AddIncomeMerchant(merchantPattern string) {
	or.incomeMerchants[strings.ToLower(merchantPattern)] = true
}

// AddExcludedMerchant marks a merchant pattern as NOT an income source.
func (or *OverrideRegistry) AddExcludedMerchant(merchantPattern string) {
	or.excludedMerchants[strings.ToLower(merchantPattern)] = true
}

// LoadFromDB populates the registry from the merchant_rules and
// txn_type_overrides tables in the given database.
//
// merchant_rules rows with rule_type "income" are added as income merchants;
// rows with rule_type "not_income" are added as excluded merchants.
//
// txn_type_overrides rows are mapped to fingerprint or merchant pattern overrides
// depending on which column is non-NULL. The target_type column must match one of
// the TransactionType string values (INCOME, EXPENSE, TRANSFER, REFUND, CREDIT_OTHER).
func (or *OverrideRegistry) LoadFromDB(db *sql.DB) error {
	// --- merchant_rules ---
	mrRows, err := db.Query(
		`SELECT merchant_pattern, rule_type FROM merchant_rules
		 WHERE rule_type IN ('income', 'not_income')`,
	)
	if err != nil {
		return fmt.Errorf("classify: query merchant_rules: %w", err)
	}
	defer mrRows.Close()

	for mrRows.Next() {
		var pattern, ruleType string
		if err := mrRows.Scan(&pattern, &ruleType); err != nil {
			return fmt.Errorf("classify: scan merchant_rules row: %w", err)
		}
		pattern = strings.ToLower(pattern)
		switch ruleType {
		case "income":
			or.incomeMerchants[pattern] = true
		case "not_income":
			or.excludedMerchants[pattern] = true
		}
	}
	if err := mrRows.Err(); err != nil {
		return fmt.Errorf("classify: iterate merchant_rules: %w", err)
	}

	// --- txn_type_overrides ---
	ovRows, err := db.Query(
		`SELECT fingerprint, merchant_pattern, target_type FROM txn_type_overrides`,
	)
	if err != nil {
		return fmt.Errorf("classify: query txn_type_overrides: %w", err)
	}
	defer ovRows.Close()

	for ovRows.Next() {
		var fp, mp, tt sql.NullString
		if err := ovRows.Scan(&fp, &mp, &tt); err != nil {
			return fmt.Errorf("classify: scan txn_type_overrides row: %w", err)
		}
		if !tt.Valid {
			continue
		}
		targetType, err := parseTransactionType(tt.String)
		if err != nil {
			// Unknown type value in DB — skip rather than hard-fail, for forward-compat.
			continue
		}
		tt2 := targetType
		switch {
		case fp.Valid && fp.String != "":
			or.fingerprintOverrides[fp.String] = UserOverride{
				Fingerprint: fp.String,
				TargetType:  &tt2,
			}
		case mp.Valid && mp.String != "":
			or.merchantOverrides = append(or.merchantOverrides, UserOverride{
				MerchantPattern: strings.ToLower(mp.String),
				TargetType:      &tt2,
			})
		}
	}
	if err := ovRows.Err(); err != nil {
		return fmt.Errorf("classify: iterate txn_type_overrides: %w", err)
	}

	return nil
}

// GetOverride returns the first applicable override for a transaction, or nil
// if no override applies. Precedence: fingerprint > merchant pattern > income hint.
func (or *OverrideRegistry) GetOverride(fingerprint, merchantNorm string) *UserOverride {
	// 1. Exact fingerprint match.
	if ov, ok := or.fingerprintOverrides[fingerprint]; ok {
		return &ov
	}

	// 2. Merchant pattern match (first match wins).
	for i := range or.merchantOverrides {
		ov := &or.merchantOverrides[i]
		if ov.MerchantPattern != "" && strings.Contains(merchantNorm, ov.MerchantPattern) {
			return ov
		}
	}

	// 3. Income / not-income hint. Return a synthetic override carrying only the hint.
	for pattern := range or.incomeMerchants {
		if strings.Contains(merchantNorm, pattern) {
			t := true
			return &UserOverride{
				MerchantPattern: pattern,
				IsIncome:        &t,
			}
		}
	}
	for pattern := range or.excludedMerchants {
		if strings.Contains(merchantNorm, pattern) {
			f := false
			return &UserOverride{
				MerchantPattern: pattern,
				IsIncome:        &f,
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Merchant Pattern
// ---------------------------------------------------------------------------

// MerchantPattern summarises the detected behavioural pattern for a merchant.
// It is populated by the pattern-detection layer and consumed here during
// classification to refine the spending bucket.
type MerchantPattern struct {
	// MerchantNorm is the normalised merchant name (lowercase, trimmed).
	MerchantNorm string
	// OccurrenceCount is the number of times this merchant has appeared.
	OccurrenceCount int
	// IsSubscription is true when a regular cadence has been detected
	// (e.g. Netflix, rent).
	IsSubscription bool
	// IsHabitual is true when the merchant is frequent but without a fixed
	// cadence (e.g. groceries, coffee shops).
	IsHabitual bool
	// IsTransfer is true when the pattern layer flagged this merchant as a
	// transfer (e.g. Zelle, Venmo).
	IsTransfer bool
	// AvgAmountCents is the median transaction amount for this merchant, in cents.
	AvgAmountCents int64
	// CadenceDays is the median number of days between transactions,
	// or nil when the cadence is not yet established.
	CadenceDays *float64
}

// ---------------------------------------------------------------------------
// Main Classification Function
// ---------------------------------------------------------------------------

// ClassifyTransaction classifies a single transaction following the strict
// precedence defined in the Truth Contract:
//
//	USER_OVERRIDE > TRANSFER_PAIR > REFUND_MATCH > TRANSFER_PATTERN >
//	INCOME_PATTERN > DEFAULT
//
// Parameters:
//   - amountCents: signed transaction amount; positive = credit, negative = debit.
//   - merchantNorm: normalised merchant name (lowercase, trimmed).
//   - isCreditCardAccount: true when the account type is credit card.
//   - pattern: optional merchant pattern from pattern-detection; may be nil.
//   - registry: optional user overrides; may be nil.
//   - fingerprint: transaction fingerprint used for override lookup.
//   - isTransferPaired: true when both legs of a transfer have been matched.
//   - matchedRefundOf: fingerprint of the expense this transaction refunds; empty if none.
func ClassifyTransaction(
	amountCents int64,
	merchantNorm string,
	isCreditCardAccount bool,
	pattern *MerchantPattern,
	registry *OverrideRegistry,
	fingerprint string,
	isTransferPaired bool,
	matchedRefundOf string,
) ClassificationResult {
	// evidence accumulates hint strings from early steps that influence later ones.
	// Only income/not-income hints are carried forward; hard overrides return early.
	var evidence []string
	userMarkedIncome := false
	userMarkedNotIncome := false

	// =========================================================================
	// STEP 1: USER OVERRIDE (highest precedence)
	// =========================================================================
	if registry != nil && fingerprint != "" {
		override := registry.GetOverride(fingerprint, merchantNorm)
		if override != nil {
			if override.TargetType != nil {
				// Hard type override — return immediately.
				return ClassificationResult{
					TxnType: *override.TargetType,
					Reason: ClassificationReason{
						PrimaryCode: EvidenceUserOverride,
						Confidence:  1.0,
						Evidence:    []string{fmt.Sprintf("User override: %s", txnTypeName(*override.TargetType))},
					},
				}
			}
			// Income / not-income hint — store for use in positive-amount logic.
			if override.IsIncome != nil {
				if *override.IsIncome {
					userMarkedIncome = true
					evidence = append(evidence, fmt.Sprintf("User marked as income: %s", override.MerchantPattern))
				} else {
					userMarkedNotIncome = true
					evidence = append(evidence, fmt.Sprintf("User marked as not-income: %s", override.MerchantPattern))
				}
			}
		}
	}

	// =========================================================================
	// STEP 2: TRANSFER PAIR (second highest)
	// =========================================================================
	if isTransferPaired {
		return ClassificationResult{
			TxnType: TxnTransfer,
			Reason: ClassificationReason{
				PrimaryCode: EvidenceTransferPairMatched,
				Confidence:  1.0,
				Evidence:    []string{"Matched transfer pair across accounts"},
			},
		}
	}

	// =========================================================================
	// STEP 3: REFUND MATCH
	// =========================================================================
	if matchedRefundOf != "" && amountCents > 0 {
		suffix := matchedRefundOf
		if len(suffix) > 8 {
			suffix = suffix[:8]
		}
		return ClassificationResult{
			TxnType: TxnRefund,
			Reason: ClassificationReason{
				PrimaryCode:  EvidenceRefundMatched,
				Confidence:   1.0,
				Evidence:     []string{fmt.Sprintf("Matched refund of expense %s...", suffix)},
				MatchedTxnID: matchedRefundOf,
			},
		}
	}

	// =========================================================================
	// POSITIVE AMOUNTS (credits)
	// =========================================================================
	if amountCents > 0 {
		// 4a. User explicitly excluded from income → CREDIT_OTHER
		if userMarkedNotIncome {
			return ClassificationResult{
				TxnType: TxnCreditOther,
				Reason: ClassificationReason{
					PrimaryCode: EvidenceUserNotIncomeRule,
					Confidence:  1.0,
					Evidence:    evidence,
				},
			}
		}

		// 4b. User explicitly marked as income → INCOME
		if userMarkedIncome {
			return ClassificationResult{
				TxnType: TxnIncome,
				Reason: ClassificationReason{
					PrimaryCode: EvidenceUserIncomeRule,
					Confidence:  1.0,
					Evidence:    evidence,
				},
			}
		}

		// 5a. Pattern layer flagged this merchant as a transfer.
		if pattern != nil && pattern.IsTransfer {
			return ClassificationResult{
				TxnType: TxnTransfer,
				Reason: ClassificationReason{
					PrimaryCode: EvidenceTransferBankPattern,
					Confidence:  0.9,
					Evidence:    []string{"Pattern marked as transfer"},
				},
			}
		}

		// 5b. Transfer keyword in merchant name (P2P, ACH, wire, etc.).
		if isTransferPattern(merchantNorm) {
			return ClassificationResult{
				TxnType: TxnTransfer,
				Reason: ClassificationReason{
					PrimaryCode: EvidenceTransferBankPattern,
					Confidence:  0.8,
					Evidence:    []string{fmt.Sprintf("Transfer keyword in: %s", merchantNorm)},
				},
			}
		}

		// 5c. Bank-to-bank transfer pattern.
		if isBankToBank(merchantNorm) {
			return ClassificationResult{
				TxnType: TxnTransfer,
				Reason: ClassificationReason{
					PrimaryCode: EvidenceTransferBankPattern,
					Confidence:  0.85,
					Evidence:    []string{"Bank-to-bank pattern detected"},
				},
			}
		}

		// 5d. Positive amount on a credit card account = payment received (transfer in).
		if isCreditCardAccount {
			return ClassificationResult{
				TxnType: TxnTransfer,
				Reason: ClassificationReason{
					PrimaryCode: EvidenceTransferCCPayment,
					Confidence:  0.95,
					Evidence:    []string{"Positive on credit card = payment received"},
				},
			}
		}

		// 6. Refund keywords (unmatched — lower confidence than a matched refund).
		if hasRefundKeywords(merchantNorm) {
			return ClassificationResult{
				TxnType: TxnRefund,
				Reason: ClassificationReason{
					PrimaryCode: EvidenceRefundKeyword,
					Confidence:  0.7,
					Evidence:    []string{fmt.Sprintf("Refund keyword in: %s", merchantNorm)},
				},
			}
		}

		// 7. Strong income signal (payroll, direct deposit keywords).
		if isStrongIncome(merchantNorm) {
			return ClassificationResult{
				TxnType: TxnIncome,
				Reason: ClassificationReason{
					PrimaryCode: EvidenceIncomePayroll,
					Confidence:  0.9,
					Evidence:    []string{fmt.Sprintf("Payroll keyword in: %s", merchantNorm)},
				},
			}
		}

		// 8. DEFAULT: unclassified positive.
		//    We do NOT assume positive = income.
		return ClassificationResult{
			TxnType: TxnCreditOther,
			Reason: ClassificationReason{
				PrimaryCode: EvidenceCreditUnclassified,
				Confidence:  0.5,
				Evidence:    []string{"Unclassified positive - needs user review"},
			},
		}
	}

	// =========================================================================
	// NEGATIVE AMOUNTS (debits / expenses)
	// =========================================================================

	// 1. CC payment from a non-CC account = transfer out to pay the card.
	if isCCPayment(merchantNorm, isCreditCardAccount) {
		return ClassificationResult{
			TxnType: TxnTransfer,
			Reason: ClassificationReason{
				PrimaryCode: EvidenceTransferCCPayment,
				Confidence:  0.95,
				Evidence:    []string{"Credit card payment from checking"},
			},
		}
	}

	// 2. Pattern layer flagged this merchant as a transfer.
	if pattern != nil && pattern.IsTransfer {
		return ClassificationResult{
			TxnType: TxnTransfer,
			Reason: ClassificationReason{
				PrimaryCode: EvidenceTransferBankPattern,
				Confidence:  0.9,
				Evidence:    []string{"Pattern marked as transfer"},
			},
		}
	}

	// 3. Transfer keyword in merchant name.
	if isTransferPattern(merchantNorm) {
		return ClassificationResult{
			TxnType: TxnTransfer,
			Reason: ClassificationReason{
				PrimaryCode: EvidenceTransferBankPattern,
				Confidence:  0.8,
				Evidence:    []string{fmt.Sprintf("Transfer keyword in: %s", merchantNorm)},
			},
		}
	}

	// 4. Determine the spending bucket from the pattern (defaults to Discretionary).
	bucket := BucketDiscretionary
	if pattern != nil {
		switch {
		case pattern.IsSubscription:
			// Predictable cadence = fixed obligation (rent, Netflix, utilities).
			bucket = BucketFixedObligations
		case pattern.IsHabitual:
			// Frequent but irregular = variable essential (groceries, gas).
			bucket = BucketVariableEssentials
		}
	}

	// 5. DEFAULT: expense.
	return ClassificationResult{
		TxnType: TxnExpense,
		Reason: ClassificationReason{
			PrimaryCode: EvidenceExpenseDefault,
			Confidence:  0.8,
			Evidence:    []string{"Default expense classification"},
		},
		SpendingBucket: &bucket,
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// parseTransactionType converts a DB string value to a TransactionType.
// Returns an error for unrecognised values so the caller can decide whether
// to skip or fail.
func parseTransactionType(s string) (TransactionType, error) {
	switch strings.ToUpper(s) {
	case "INCOME":
		return TxnIncome, nil
	case "EXPENSE":
		return TxnExpense, nil
	case "TRANSFER":
		return TxnTransfer, nil
	case "REFUND":
		return TxnRefund, nil
	case "CREDIT_OTHER":
		return TxnCreditOther, nil
	default:
		return TxnCreditOther, fmt.Errorf("classify: unknown transaction type %q", s)
	}
}

// txnTypeName returns the canonical string name for a TransactionType,
// matching the DB and JSON representation.
func txnTypeName(t TransactionType) string {
	switch t {
	case TxnIncome:
		return "INCOME"
	case TxnExpense:
		return "EXPENSE"
	case TxnTransfer:
		return "TRANSFER"
	case TxnRefund:
		return "REFUND"
	case TxnCreditOther:
		return "CREDIT_OTHER"
	default:
		return "UNKNOWN"
	}
}
