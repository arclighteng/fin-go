package models

import "time"

type Account struct {
	AccountID   string
	Institution string
	Name        string
	Type        string // checking, savings, credit, etc.
	Currency    string // default "USD"
}

type Transaction struct {
	AccountID   string
	PostedAt    time.Time
	AmountCents int64
	Currency    string
	Description string
	Merchant    string
	SourceTxnID string
	Fingerprint string
	Pending     bool
}

type SyncRun struct {
	ID           int64
	RanAt        time.Time
	LookbackDays int
	TxnsFetched  int
	TxnsInserted int
	TxnsUpdated  int
}

type AlertAction struct {
	AlertKey    string
	Action      string
	MerchantNorm string
	PatternType string
	Notes       string
}

type Commitment struct {
	ID            int64
	Name          string
	MerchantNorm  string
	ExpectedCents *int64 // nullable
	Cadence       string // monthly, weekly, annual, quarterly, biweekly, one_time
	DayOfMonth    *int   // nullable
	ReferenceDate string // YYYY-MM-DD
	Confirmed     bool
	Source        string // detected, manual, dismissed
	Direction     string // expense, income
}

type BudgetTarget struct {
	CategoryID        string
	MonthlyTargetCents int64
}

type TimePeriod string

const (
	PeriodMonth   TimePeriod = "month"
	PeriodQuarter TimePeriod = "quarter"
	PeriodYear    TimePeriod = "year"
)
