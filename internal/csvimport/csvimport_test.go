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
