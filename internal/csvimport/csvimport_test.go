package csvimport

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Import — valid CSV
// ---------------------------------------------------------------------------

func TestImport_ValidCSV_StandardColumns(t *testing.T) {
	t.Parallel()

	csv := `Date,Amount,Description
2025-01-15,-50.00,Coffee Shop
2025-01-16,-120.50,Grocery Store
2025-01-17,2500.00,Paycheck`

	result, err := Import(strings.NewReader(csv), ImportOptions{AccountID: "acct-1"})
	if err != nil {
		t.Fatalf("Import: unexpected error: %v", err)
	}
	if len(result.Transactions) != 3 {
		t.Errorf("want 3 transactions, got %d", len(result.Transactions))
	}
	if len(result.Errors) != 0 {
		t.Errorf("want no errors, got %v", result.Errors)
	}

	// Verify first transaction fields.
	txn := result.Transactions[0]
	if txn.AccountID != "acct-1" {
		t.Errorf("AccountID: want acct-1, got %q", txn.AccountID)
	}
	if txn.AmountCents != -5000 {
		t.Errorf("AmountCents: want -5000, got %d", txn.AmountCents)
	}
	if txn.Fingerprint == "" {
		t.Error("Fingerprint must not be empty")
	}
}

// ---------------------------------------------------------------------------
// Import — missing required columns
// ---------------------------------------------------------------------------

func TestImport_MissingRequiredColumns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		csv  string
	}{
		{
			name: "missing amount column",
			csv:  "Date,Description\n2025-01-15,Coffee Shop",
		},
		{
			name: "missing date column",
			csv:  "Amount,Description\n-50.00,Coffee Shop",
		},
		{
			name: "missing description and merchant columns",
			csv:  "Date,Amount\n2025-01-15,-50.00",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Import(strings.NewReader(tc.csv), ImportOptions{})
			if err == nil {
				t.Errorf("Import(%s): want error for missing column, got nil", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Import — malformed amount strings
// ---------------------------------------------------------------------------

func TestImport_MalformedAmounts(t *testing.T) {
	t.Parallel()

	csv := `Date,Amount,Description
2025-01-15,not-a-number,Coffee Shop
2025-01-16,--50.00,Grocery Store`

	result, err := Import(strings.NewReader(csv), ImportOptions{})
	if err != nil {
		t.Fatalf("Import: unexpected top-level error: %v", err)
	}
	// Rows with bad amounts are collected as per-row errors, not fatal.
	if len(result.Errors) == 0 {
		t.Error("want at least one per-row error for malformed amount, got none")
	}
	if len(result.Transactions) != 0 {
		t.Errorf("want 0 successful transactions (all bad amounts), got %d", len(result.Transactions))
	}
}

// ---------------------------------------------------------------------------
// Import — empty CSV
// ---------------------------------------------------------------------------

func TestImport_EmptyCSV(t *testing.T) {
	t.Parallel()

	_, err := Import(strings.NewReader(""), ImportOptions{})
	if err == nil {
		t.Error("Import(empty): want error for empty CSV, got nil")
	}
}

func TestImport_HeaderOnlyNoData(t *testing.T) {
	t.Parallel()

	// Header row present but no data rows.
	_, err := Import(strings.NewReader("Date,Amount,Description\n"), ImportOptions{})
	if err == nil {
		t.Error("Import(header-only): want error for missing data rows, got nil")
	}
}

// ---------------------------------------------------------------------------
// Import — various date formats
// ---------------------------------------------------------------------------

func TestImport_DateFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dateStr string
		wantDay int // expected day-of-month
	}{
		{"ISO 8601", "2025-03-15", 15},
		{"US slash MM/DD/YYYY", "03/15/2025", 15},
		{"US slash M/D/YYYY", "3/15/2025", 15},
		{"slash YYYY/MM/DD", "2025/03/15", 15},
		{"dash MM-DD-YYYY", "03-15-2025", 15},
		{"abbreviation Jan D YYYY", `"Jan 15, 2025"`, 15},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			csv := "Date,Amount,Description\n" + tc.dateStr + ",-10.00,Coffee"
			result, err := Import(strings.NewReader(csv), ImportOptions{})
			if err != nil {
				t.Fatalf("Import(%s): top-level error: %v", tc.name, err)
			}
			if len(result.Errors) > 0 {
				t.Fatalf("Import(%s): per-row errors: %v", tc.name, result.Errors)
			}
			if len(result.Transactions) != 1 {
				t.Fatalf("Import(%s): want 1 transaction, got %d", tc.name, len(result.Transactions))
			}
			if got := result.Transactions[0].PostedAt.Day(); got != tc.wantDay {
				t.Errorf("Import(%s): day-of-month: want %d, got %d", tc.name, tc.wantDay, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Import — default account ID
// ---------------------------------------------------------------------------

func TestImport_DefaultAccountID(t *testing.T) {
	t.Parallel()

	csv := "Date,Amount,Description\n2025-01-15,-10.00,Coffee"
	result, err := Import(strings.NewReader(csv), ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Transactions) != 1 {
		t.Fatalf("want 1 transaction, got %d", len(result.Transactions))
	}
	if result.Transactions[0].AccountID != "csv-import" {
		t.Errorf("default AccountID: want csv-import, got %q", result.Transactions[0].AccountID)
	}
}

// ---------------------------------------------------------------------------
// Import — accounting-style negatives and dollar signs
// ---------------------------------------------------------------------------

func TestImport_AmountFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		amountStr string
		wantCents int64
	}{
		{"plain negative", "-50.00", -5000},
		{"positive", "100.00", 10000},
		{"dollar sign", "$25.99", 2599},
		{"dollar negative", "-$25.99", -2599},
		{"accounting parens", "(75.50)", -7550},
		{"commas", `"1,234.56"`, 123456},
		{"zero", "0.00", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			csv := "Date,Amount,Description\n2025-01-15," + tc.amountStr + ",Coffee"
			result, err := Import(strings.NewReader(csv), ImportOptions{})
			if err != nil {
				t.Fatalf("Import(%s): top-level error: %v", tc.name, err)
			}
			if len(result.Errors) > 0 {
				t.Fatalf("Import(%s): per-row errors: %v", tc.name, result.Errors)
			}
			if len(result.Transactions) != 1 {
				t.Fatalf("Import(%s): want 1 transaction, got %d", tc.name, len(result.Transactions))
			}
			if got := result.Transactions[0].AmountCents; got != tc.wantCents {
				t.Errorf("Import(%s): AmountCents: want %d, got %d", tc.name, tc.wantCents, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Import — fingerprint uniqueness
// ---------------------------------------------------------------------------

func TestImport_FingerprintUnique(t *testing.T) {
	t.Parallel()

	// Two rows with the same date/amount/description → different fingerprints
	// only if they are actually different. Two identical rows → same fingerprint.
	csv := `Date,Amount,Description
2025-01-15,-50.00,Coffee Shop
2025-01-15,-50.00,Coffee Shop`

	result, err := Import(strings.NewReader(csv), ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Transactions) != 2 {
		t.Fatalf("want 2 transactions, got %d", len(result.Transactions))
	}
	// Both rows produce the same fingerprint (identical content).
	fp0 := result.Transactions[0].Fingerprint
	fp1 := result.Transactions[1].Fingerprint
	if fp0 != fp1 {
		t.Errorf("identical rows should produce same fingerprint; got %q and %q", fp0, fp1)
	}
}

// ---------------------------------------------------------------------------
// Import — Capital One split Debit/Credit columns (ADA-109)
// ---------------------------------------------------------------------------

// App-wide sign convention (see internal/server/views.go): negative amount_cents
// = expense, positive = income. Capital One exports split money-out (Debit) and
// money-in (Credit) across two columns; a purchase must import negative and a
// payment/refund must import positive and non-zero.
func TestImport_CapitalOne_DebitCreditSigns(t *testing.T) {
	t.Parallel()

	if bank := DetectBank([]string{
		"Transaction Date", "Posted Date", "Card No.", "Description", "Category", "Debit", "Credit",
	}); bank != "Capital One" {
		t.Fatalf("DetectBank: want Capital One, got %q", bank)
	}

	csv := `Transaction Date,Posted Date,Card No.,Description,Category,Debit,Credit
2025-02-01,2025-02-02,1234,Coffee Shop,Dining,12.34,
2025-02-05,2025-02-06,1234,Online Payment,Payment,,200.00
2025-02-08,2025-02-09,1234,Store Refund,Merchandise,,15.00`

	result, err := Import(strings.NewReader(csv), ImportOptions{AccountID: "capone"})
	if err != nil {
		t.Fatalf("Import: unexpected error: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("want no per-row errors, got %v", result.Errors)
	}
	if len(result.Transactions) != 3 {
		t.Fatalf("want 3 transactions, got %d", len(result.Transactions))
	}

	tests := []struct {
		name      string
		idx       int
		wantCents int64
	}{
		{"purchase (Debit) -> negative expense", 0, -1234},
		{"payment (Credit) -> positive income", 1, 20000},
		{"refund (Credit) -> positive income", 2, 1500},
	}
	for _, tc := range tests {
		if got := result.Transactions[tc.idx].AmountCents; got != tc.wantCents {
			t.Errorf("%s: AmountCents: want %d, got %d", tc.name, tc.wantCents, got)
		}
	}
	for i, txn := range result.Transactions {
		if txn.AmountCents == 0 {
			t.Errorf("transaction %d imported as $0 (silent-drop regression)", i)
		}
	}
}

// A non-empty-but-unparseable amount must surface as a per-row error, never a
// silent $0. Both columns blank is an error for a split-column format (a Capital
// One row is never legitimately blank in both).
func TestImport_CapitalOne_AmountErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		csv  string
	}{
		{
			name: "unparseable debit",
			csv:  "Transaction Date,Description,Debit,Credit\n2025-02-01,Coffee,not-a-number,",
		},
		{
			name: "unparseable credit",
			csv:  "Transaction Date,Description,Debit,Credit\n2025-02-01,Payment,,xyz",
		},
		{
			name: "both columns blank",
			csv:  "Transaction Date,Description,Debit,Credit\n2025-02-01,Mystery,,",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := Import(strings.NewReader(tc.csv), ImportOptions{})
			if err != nil {
				t.Fatalf("Import: unexpected top-level error: %v", err)
			}
			if len(result.Errors) == 0 {
				t.Errorf("%s: want a per-row error, got none (silent-0 regression)", tc.name)
			}
			if len(result.Transactions) != 0 {
				t.Errorf("%s: want 0 transactions, got %d", tc.name, len(result.Transactions))
			}
		})
	}
}

// An unrecognised split-column export (Debit + Credit present, not a known bank)
// must still combine as a signed pair via the generic fallback rather than
// collapsing onto one column.
func TestImport_GenericDebitCreditPair(t *testing.T) {
	t.Parallel()

	// "Date"/"Memo" headers do not match any known bank format, forcing the
	// generic keyword fallback.
	if bank := DetectBank([]string{"Date", "Memo", "Debit", "Credit"}); bank != "" {
		t.Fatalf("DetectBank: want unrecognised (empty), got %q", bank)
	}

	csv := `Date,Memo,Debit,Credit
2025-03-01,Groceries,54.20,
2025-03-02,Deposit,,300.00`

	result, err := Import(strings.NewReader(csv), ImportOptions{})
	if err != nil {
		t.Fatalf("Import: unexpected error: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("want no per-row errors, got %v", result.Errors)
	}
	if len(result.Transactions) != 2 {
		t.Fatalf("want 2 transactions, got %d", len(result.Transactions))
	}
	if got := result.Transactions[0].AmountCents; got != -5420 {
		t.Errorf("debit row AmountCents: want -5420, got %d", got)
	}
	if got := result.Transactions[1].AmountCents; got != 30000 {
		t.Errorf("credit row AmountCents: want 30000, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// DetectBank
// ---------------------------------------------------------------------------

func TestDetectBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers []string
		want    string
	}{
		{
			name:    "Chase headers",
			headers: []string{"Transaction Date", "Amount", "Description"},
			want:    "Chase",
		},
		{
			name:    "unknown headers",
			headers: []string{"Foo", "Bar", "Baz"},
			want:    "",
		},
		{
			name:    "empty headers",
			headers: []string{},
			want:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := DetectBank(tc.headers)
			if got != tc.want {
				t.Errorf("DetectBank(%v): want %q, got %q", tc.headers, tc.want, got)
			}
		})
	}
}
