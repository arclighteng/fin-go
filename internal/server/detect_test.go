package server

import (
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
)

// seedRecurring inserts a transaction history containing two genuine
// subscriptions (Netflix monthly, Spotify monthly) plus irregular/variable
// grocery spend that must NOT be detected as recurring.
func seedRecurring(t *testing.T, database *db.DB) {
	t.Helper()
	if err := database.UpsertAccounts([]models.Account{
		{AccountID: "acct-check", Institution: "Bank", Name: "Checking", Type: "checking", Currency: "USD"},
	}); err != nil {
		t.Fatalf("seed accounts: %v", err)
	}

	var txns []models.Transaction
	add := func(fp, merchant string, d time.Time, cents int64) {
		txns = append(txns, models.Transaction{
			AccountID: "acct-check", PostedAt: d, AmountCents: cents,
			Currency: "USD", Description: merchant, Merchant: merchant, Fingerprint: fp,
		})
	}

	for i := 0; i < 6; i++ {
		add("nf-"+itoa(i), "Netflix", time.Date(2026, time.Month(1+i), 15, 0, 0, 0, 0, time.UTC), -1599)
	}
	for i := 0; i < 5; i++ {
		add("sp-"+itoa(i), "Spotify", time.Date(2026, time.Month(1+i), 3, 0, 0, 0, 0, time.UTC), -999)
	}
	groceryDates := []time.Time{
		time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}
	groceryAmounts := []int64{-4500, -8231, -2200, -12000, -6700}
	for i := range groceryDates {
		add("wf-"+itoa(i), "Whole Foods", groceryDates[i], groceryAmounts[i])
	}

	if _, _, err := database.UpsertTransactions(txns); err != nil {
		t.Fatalf("seed transactions: %v", err)
	}
}

func itoa(i int) string { return string(rune('0' + i)) }

func findCommitment(cs []models.Commitment, merchantNorm string) (models.Commitment, bool) {
	for _, c := range cs {
		if c.MerchantNorm == merchantNorm {
			return c, true
		}
	}
	return models.Commitment{}, false
}

// (a) After a sync over recurring transactions, commitments contains the
// detected subscriptions with no manual entry.
func TestDetectAndPersist_CreatesDetectedCommitments(t *testing.T) {
	database := newRawDB(t)
	seedRecurring(t, database)

	res, err := DetectAndPersistCommitments(database)
	if err != nil {
		t.Fatalf("DetectAndPersistCommitments: %v", err)
	}
	if res.Inserted < 2 {
		t.Fatalf("want >= 2 inserted, got %d (detected=%d)", res.Inserted, res.Detected)
	}

	cs, err := database.GetCommitments()
	if err != nil {
		t.Fatalf("GetCommitments: %v", err)
	}

	nf, ok := findCommitment(cs, "netflix")
	if !ok {
		t.Fatalf("netflix not persisted; commitments=%+v", cs)
	}
	if nf.Source != "detected" {
		t.Errorf("netflix source: want detected, got %q", nf.Source)
	}
	if nf.Cadence != "monthly" {
		t.Errorf("netflix cadence: want monthly, got %q", nf.Cadence)
	}
	if nf.ExpectedCents == nil || *nf.ExpectedCents != 1599 {
		t.Errorf("netflix amount: want 1599, got %v", nf.ExpectedCents)
	}
	if nf.Confirmed {
		t.Errorf("auto-detected commitment must not be pre-confirmed")
	}
	if _, ok := findCommitment(cs, "spotify"); !ok {
		t.Errorf("spotify not persisted; commitments=%+v", cs)
	}
	// Variable/irregular grocery spend must not be misreported.
	if _, ok := findCommitment(cs, "whole foods"); ok {
		t.Errorf("whole foods (variable spend) was wrongly detected")
	}
}

// (b) A SECOND sync does not duplicate detected commitments.
func TestDetectAndPersist_Idempotent(t *testing.T) {
	database := newRawDB(t)
	seedRecurring(t, database)

	if _, err := DetectAndPersistCommitments(database); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	first, err := database.GetCommitments()
	if err != nil {
		t.Fatalf("GetCommitments: %v", err)
	}

	res2, err := DetectAndPersistCommitments(database)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	second, err := database.GetCommitments()
	if err != nil {
		t.Fatalf("GetCommitments: %v", err)
	}

	if len(second) != len(first) {
		t.Fatalf("re-sync duplicated commitments: first=%d second=%d", len(first), len(second))
	}
	if res2.Inserted != 0 {
		t.Errorf("second pass should insert nothing, inserted=%d", res2.Inserted)
	}
	if res2.Updated == 0 {
		t.Errorf("second pass should refresh existing detected rows, updated=%d", res2.Updated)
	}
}

// (c) A user-entered/confirmed commitment survives a re-sync unchanged, and no
// duplicate detected row is created for the same merchant.
func TestDetectAndPersist_PreservesUserCommitments(t *testing.T) {
	database := newRawDB(t)
	seedRecurring(t, database)

	// User manually enters their own Netflix commitment with a custom amount,
	// and a rent commitment that has no matching transactions at all.
	manualNetflix := models.Commitment{
		Name: "Netflix Premium", MerchantNorm: "netflix", Cadence: "monthly",
		Direction: "expense", Source: "manual", Confirmed: true,
	}
	amt := int64(1999)
	manualNetflix.ExpectedCents = &amt
	if _, err := database.SaveCommitment(manualNetflix); err != nil {
		t.Fatalf("SaveCommitment netflix: %v", err)
	}
	rentAmt := int64(200000)
	if _, err := database.SaveCommitment(models.Commitment{
		Name: "Rent", MerchantNorm: "landlord", Cadence: "monthly",
		Direction: "expense", Source: "manual", ExpectedCents: &rentAmt,
	}); err != nil {
		t.Fatalf("SaveCommitment rent: %v", err)
	}

	if _, err := DetectAndPersistCommitments(database); err != nil {
		t.Fatalf("DetectAndPersistCommitments: %v", err)
	}

	cs, err := database.GetCommitments()
	if err != nil {
		t.Fatalf("GetCommitments: %v", err)
	}

	// Exactly one netflix row, still the user's manual one, unchanged.
	nfCount := 0
	for _, c := range cs {
		if c.MerchantNorm == "netflix" {
			nfCount++
		}
	}
	if nfCount != 1 {
		t.Fatalf("want exactly 1 netflix commitment, got %d (detection must not clobber/duplicate user rows)", nfCount)
	}
	nf, _ := findCommitment(cs, "netflix")
	if nf.Source != "manual" || nf.Name != "Netflix Premium" || nf.ExpectedCents == nil || *nf.ExpectedCents != 1999 {
		t.Errorf("user netflix commitment was mutated: %+v", nf)
	}

	// Rent (manual, no transactions) survives untouched.
	rent, ok := findCommitment(cs, "landlord")
	if !ok || rent.Source != "manual" || rent.ExpectedCents == nil || *rent.ExpectedCents != 200000 {
		t.Errorf("manual rent commitment was altered or dropped: %+v", rent)
	}
}
