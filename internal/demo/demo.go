// Package demo generates realistic sample transaction data so users can
// explore the app before connecting a live SimpleFIN account.
//
// All generated accounts and transactions carry the "demo-" prefix so they
// can be identified and removed without touching real data.
package demo

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

// demoAccount describes a synthetic bank account.
type demoAccount struct {
	AccountID   string
	Institution string
	Name        string
	Type        string
	Currency    string
}

var demoAccounts = []demoAccount{
	{
		AccountID:   "demo-checking-001",
		Institution: "Demo Bank",
		Name:        "Primary Checking",
		Type:        "checking",
		Currency:    "USD",
	},
	{
		AccountID:   "demo-credit-001",
		Institution: "Demo Bank",
		Name:        "Rewards Credit Card",
		Type:        "credit",
		Currency:    "USD",
	},
}

// subscription is a fixed-amount recurring charge.
// acctIdx indexes into demoAccounts (0 = checking, 1 = credit).
type subscription struct {
	merchant   string
	amtCents   int64
	dayOfMonth int
	acctIdx    int
}

var subscriptions = []subscription{
	{"NETFLIX.COM", -1599, 15, 1},
	{"SPOTIFY USA", -1099, 3, 1},
	{"DISNEY PLUS", -1399, 22, 1},
	{"AMAZON PRIME", -1499, 7, 1},
	{"YOUTUBE PREMIUM", -1399, 12, 1},
	{"OPENAI *CHATGPT", -2000, 18, 1},
	{"GITHUB INC", -400, 10, 1},
	{"ICLOUD STORAGE", -299, 1, 1},
	{"NYT DIGITAL", -1700, 25, 1},
}

// bill is a variable-amount recurring charge (utilities, etc.).
type bill struct {
	merchant   string
	baseCents  int64
	variance   int64 // half-range: actual = base ± rand(0, variance)
	dayOfMonth int
	acctIdx    int
}

var bills = []bill{
	{"CITY WATER UTILITY", -6500, 2000, 5, 0},
	{"POWER ELECTRIC CO", -12000, 5000, 12, 0},
	{"GAS COMPANY", -8000, 4000, 8, 0},
	{"INTERNET PROVIDER", -7999, 0, 20, 0},
	{"MOBILE CARRIER", -8500, 0, 15, 0},
}

// oneOff is a sporadic merchant with a random-frequency-per-month pattern.
// minCents and maxCents are both negative for expenses.
type oneOff struct {
	merchant      string
	minCents      int64
	maxCents      int64
	freqPerMonth  float64
	acctIdx       int
}

var oneOffs = []oneOff{
	{"WHOLE FOODS MKT", -15000, -4000, 4, 1},
	{"TRADER JOES", -8000, -3000, 2, 1},
	{"COSTCO WHSE", -25000, -8000, 1, 1},
	{"CHIPOTLE", -1800, -1200, 3, 1},
	{"STARBUCKS", -900, -500, 6, 1},
	{"DOORDASH", -5000, -2500, 2, 1},
	{"LOCAL RESTAURANT", -8000, -3000, 2, 1},
	{"SHELL OIL", -6500, -3500, 2, 1},
	{"CHEVRON", -7000, -4000, 2, 1},
	{"AMAZON.COM", -12000, -1500, 3, 1},
	{"TARGET", -8000, -2000, 1, 1},
	{"WALMART", -10000, -3000, 1, 0},
	{"AMC THEATRES", -3500, -1500, 0.5, 1},
	{"STEAM GAMES", -6000, -1000, 0.3, 1},
	{"CVS PHARMACY", -5000, -1000, 0.5, 1},
	{"URGENT CARE COPAY", -5000, -5000, 0.2, 1},
	{"UBER", -4000, -1500, 1, 1},
	{"PARKING METER", -1000, -200, 2, 1},
}

// income is a bi-weekly paycheck: deposited on day1 and day2 of each month.
type income struct {
	merchant  string
	amtCents  int64
	day1      int
	day2      int
	acctIdx   int
}

var incomes = []income{
	{"ACME CORP PAYROLL", 385000, 1, 15, 0},
}

// Generate inserts demo accounts and 12 months of synthetic transactions into db.
// It is idempotent: duplicate fingerprints are silently ignored by the
// INSERT OR IGNORE strategy.
func Generate(db *sql.DB) error {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// --- Accounts ---
	for _, a := range demoAccounts {
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO accounts (account_id, institution, name, type, currency, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			a.AccountID, a.Institution, a.Name, a.Type, a.Currency, now,
		)
		if err != nil {
			return fmt.Errorf("insert account %s: %w", a.AccountID, err)
		}
	}

	// --- Transactions ---
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO transactions
		  (account_id, posted_at, amount_cents, currency, description, merchant,
		   fingerprint, pending, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	inserted := 0
	today := time.Now()

	for _, t := range generateTransactions(today) {
		_, err := stmt.Exec(
			t.accountID,
			t.postedAt.Format("2006-01-02"),
			t.amtCents,
			"USD",
			t.merchant,
			t.merchant,
			t.fingerprint,
			now, now,
		)
		if err != nil {
			return fmt.Errorf("insert transaction: %w", err)
		}
		inserted++
	}

	// Record a synthetic run so the status page looks reasonable.
	_, err = tx.Exec(`
		INSERT INTO runs (ran_at, lookback_days, txns_fetched, txns_inserted, txns_updated)
		VALUES (?, ?, ?, ?, 0)`,
		now, 365, inserted, inserted,
	)
	if err != nil {
		return fmt.Errorf("record run: %w", err)
	}

	return tx.Commit()
}

// Clear removes all demo accounts, their transactions, and any alert_actions
// whose alert_key references a demo account.
func Clear(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, a := range demoAccounts {
		if _, err := tx.Exec("DELETE FROM transactions WHERE account_id = ?", a.AccountID); err != nil {
			return fmt.Errorf("delete transactions for %s: %w", a.AccountID, err)
		}
		if _, err := tx.Exec("DELETE FROM accounts WHERE account_id = ?", a.AccountID); err != nil {
			return fmt.Errorf("delete account %s: %w", a.AccountID, err)
		}
	}

	// Clean up alert_actions whose key begins with "demo-" (best-effort).
	if _, err := tx.Exec("DELETE FROM alert_actions WHERE alert_key LIKE 'demo-%'"); err != nil {
		return fmt.Errorf("delete demo alert_actions: %w", err)
	}

	return tx.Commit()
}

// --- internal generation helpers ---

type txnRow struct {
	accountID   string
	postedAt    time.Time
	amtCents    int64
	merchant    string
	fingerprint string
}

// generateTransactions produces all synthetic transactions for the past 12 months.
func generateTransactions(today time.Time) []txnRow {
	var rows []txnRow

	// Start of month 12 months ago.
	start := today.AddDate(0, -12, 0)
	current := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	todayUTC := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	for !current.After(todayUTC) {
		year, month := current.Year(), current.Month()
		isFirst := current.Year() == start.Year() && current.Month() == start.Month()

		// Income: two paydays per month.
		for _, inc := range incomes {
			acctID := demoAccounts[inc.acctIdx].AccountID
			for _, day := range []int{inc.day1, inc.day2} {
				d := clampDate(year, int(month), day, todayUTC)
				if !d.IsZero() {
					rows = append(rows, makeRow(d, inc.amtCents, inc.merchant, acctID))
				}
			}
		}

		// Subscriptions: one per month.
		for _, s := range subscriptions {
			acctID := demoAccounts[s.acctIdx].AccountID
			d := clampDate(year, int(month), s.dayOfMonth, todayUTC)
			if !d.IsZero() {
				rows = append(rows, makeRow(d, s.amtCents, s.merchant, acctID))
			}
		}

		// Bills: one per month with variance.
		for _, b := range bills {
			acctID := demoAccounts[b.acctIdx].AccountID
			d := clampDate(year, int(month), b.dayOfMonth, todayUTC)
			if !d.IsZero() {
				amt := b.baseCents
				if b.variance > 0 {
					v := randInt64(b.variance*2+1) - b.variance
					amt += v
				}
				rows = append(rows, makeRow(d, amt, b.merchant, acctID))
			}
		}

		// One-offs: random frequency.
		for _, o := range oneOffs {
			acctID := demoAccounts[o.acctIdx].AccountID
			count := frequencyCount(o.freqPerMonth)
			for i := 0; i < count; i++ {
				day := int(randInt64(28)) + 1
				d := clampDate(year, int(month), day, todayUTC)
				if d.IsZero() {
					continue
				}
				rng := o.maxCents - o.minCents
				amt := o.minCents
				if rng > 0 {
					amt += randInt64(rng + 1)
				} else if rng < 0 {
					// minCents > maxCents means both are negative and min is larger absolute value.
					// Swap so we always add a positive offset.
					lo, hi := o.maxCents, o.minCents
					amt = lo + randInt64(hi-lo+1)
				}
				rows = append(rows, makeRow(d, amt, o.merchant, acctID))
			}
		}

		// Credit card payment — skip the very first month.
		if !isFirst {
			d := clampDate(year, int(month), 25, todayUTC)
			if !d.IsZero() {
				payment := 80000 + randInt64(170001) // $800 – $2,500
				rows = append(rows, makeRow(d, -payment, "CREDIT CARD PAYMENT", demoAccounts[0].AccountID))
				rows = append(rows, makeRow(d, payment, "PAYMENT RECEIVED", demoAccounts[1].AccountID))
			}
		}

		// Advance to next month.
		if month == 12 {
			current = time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
		} else {
			current = time.Date(year, month+1, 1, 0, 0, 0, 0, time.UTC)
		}
	}

	// Anomaly rows: duplicate charge and unusually large transaction.
	rows = append(rows,
		makeRow(todayUTC.AddDate(0, 0, -5), -4599, "STREAMING SERVICE", demoAccounts[1].AccountID),
		makeRow(todayUTC.AddDate(0, 0, -3), -4599, "STREAMING SERVICE", demoAccounts[1].AccountID),
		makeRow(todayUTC.AddDate(0, 0, -7), -8500, "STARBUCKS", demoAccounts[1].AccountID),
	)

	return rows
}

// makeRow builds a txnRow with a random unique fingerprint.
func makeRow(d time.Time, amtCents int64, merchant, accountID string) txnRow {
	return txnRow{
		accountID:   accountID,
		postedAt:    d,
		amtCents:    amtCents,
		merchant:    merchant,
		fingerprint: "demo_" + randHex(12),
	}
}

// clampDate returns a date in (year, month) close to targetDay, clamped to the
// last day of the month and constrained to not exceed ceiling. Returns zero
// value when the result would exceed ceiling.
func clampDate(year, month, targetDay int, ceiling time.Time) time.Time {
	lastDay := daysInMonth(year, time.Month(month))
	day := targetDay
	if day > lastDay {
		day = lastDay
	}
	// Small variance: -2..+2 days.
	v := int(randInt64(5)) - 2
	day = day + v
	if day < 1 {
		day = 1
	}
	if day > lastDay {
		day = lastDay
	}
	d := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if d.After(ceiling) {
		return time.Time{}
	}
	return d
}

// daysInMonth returns the number of days in a given year/month.
func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// frequencyCount converts a fractional monthly frequency into an integer
// occurrence count using a probabilistic draw for the fractional part.
func frequencyCount(freq float64) int {
	base := int(freq)
	frac := freq - float64(base)
	if frac > 0 && randFloat() < frac {
		base++
	}
	return base
}

// randInt64 returns a cryptographically random int64 in [0, n).
// Falls back to 0 on error (generation is best-effort for demo data).
func randInt64(n int64) int64 {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		return 0
	}
	return v.Int64()
}

// randFloat returns a float64 in [0, 1).
func randFloat() float64 {
	v, err := rand.Int(rand.Reader, big.NewInt(1<<53))
	if err != nil {
		return 0
	}
	return float64(v.Int64()) / float64(1<<53)
}

// randHex returns n random hex bytes (2n hex characters).
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "000000000000"
	}
	return hex.EncodeToString(b)
}
