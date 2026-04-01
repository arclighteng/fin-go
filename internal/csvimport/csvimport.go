// Package csvimport parses bank CSV exports and converts them to Transaction
// records ready for insertion via db.UpsertTransactions.
//
// Auto-detection covers the most common US bank CSV formats. Unknown formats
// fall back to a generic case-insensitive column search.
package csvimport

import (
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/models"
)

// bankFormat describes column names for a known bank CSV export.
type bankFormat struct {
	displayName string
	dateCol     string
	amountCol   string
	descCol     string
	merchantCol string // optional — empty means no separate merchant column
}

// knownFormats maps a bank slug to its column layout.
var knownFormats = map[string]bankFormat{
	"chase": {
		displayName: "Chase",
		dateCol:     "Transaction Date",
		amountCol:   "Amount",
		descCol:     "Description",
	},
	"bofa": {
		displayName: "Bank of America",
		dateCol:     "Date",
		amountCol:   "Amount",
		descCol:     "Description",
	},
	"amex": {
		displayName: "American Express",
		dateCol:     "Date",
		amountCol:   "Amount",
		descCol:     "Description",
	},
	"wellsfargo": {
		displayName: "Wells Fargo",
		dateCol:     "Date",
		amountCol:   "Amount",
		descCol:     "Description",
	},
	"capitalone": {
		displayName: "Capital One",
		dateCol:     "Transaction Date",
		amountCol:   "Debit",
		descCol:     "Description",
	},
}

// ImportOptions controls CSV parsing behaviour.
type ImportOptions struct {
	// AccountID is assigned to every imported transaction. Defaults to "csv-import".
	AccountID string

	// DateFormat is a Go time layout string tried first when parsing dates.
	// Common fallback formats are tried automatically afterwards.
	DateFormat string

	// Delimiter is the field separator. Zero value defaults to comma.
	Delimiter rune

	// DryRun parses the file and returns results without writing to the DB.
	DryRun bool
}

// Result summarises a completed import operation.
type Result struct {
	Imported     int
	Skipped      int
	Errors       []string
	Transactions []models.Transaction // all parsed rows (populated even on DryRun)
}

// Import reads CSV data from r, parses transactions, and returns them together
// with summary counts. When opts.DryRun is true the returned transactions are
// populated but Imported and Skipped will both be zero (the caller performs the
// actual database writes).
//
// The function auto-detects the bank format from CSV headers; opts fields
// override detected values when explicitly set.
func Import(r io.Reader, opts ImportOptions) (*Result, error) {
	if opts.AccountID == "" {
		opts.AccountID = "csv-import"
	}
	if opts.Delimiter == 0 {
		opts.Delimiter = ','
	}

	cr := csv.NewReader(r)
	cr.Comma = opts.Delimiter
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true

	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv has no data rows (only %d row(s))", len(records))
	}

	headers := records[0]
	dataRows := records[1:]

	// Resolve column indices from detected or default mapping.
	dateIdx, amountIdx, descIdx, merchantIdx, err := resolveColumns(headers, opts)
	if err != nil {
		return nil, err
	}

	fallbackDateFormats := []string{
		"2006-01-02",
		"01/02/2006",
		"1/2/2006",
		"02/01/2006",
		"01-02-2006",
		"2006/01/02",
		"Jan 2, 2006",
		"January 2, 2006",
	}
	if opts.DateFormat != "" {
		// Try user-supplied format first.
		fallbackDateFormats = append([]string{opts.DateFormat}, fallbackDateFormats...)
	}

	result := &Result{}

	for rowNum, row := range dataRows {
		lineNum := rowNum + 2 // +1 for header, +1 for 1-based display

		txn, errMsg := parseRow(row, lineNum, headers, dateIdx, amountIdx, descIdx, merchantIdx, opts.AccountID, fallbackDateFormats)
		if errMsg != "" {
			result.Errors = append(result.Errors, errMsg)
			continue
		}
		result.Transactions = append(result.Transactions, txn)
	}

	return result, nil
}

// parseRow converts one CSV record into a Transaction. Returns an error message
// (non-empty) if the row cannot be parsed.
func parseRow(
	row []string,
	lineNum int,
	headers []string,
	dateIdx, amountIdx, descIdx, merchantIdx int,
	accountID string,
	dateFormats []string,
) (models.Transaction, string) {
	get := func(idx int) string {
		if idx < 0 || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	// --- Date ---
	dateStr := get(dateIdx)
	var postedAt time.Time
	for _, fmt := range dateFormats {
		if t, err := time.Parse(fmt, dateStr); err == nil {
			postedAt = t
			break
		}
	}
	if postedAt.IsZero() {
		return models.Transaction{}, fmt.Sprintf("row %d: cannot parse date %q", lineNum, dateStr)
	}

	// --- Amount ---
	amountStr := get(amountIdx)
	amountCents, err := parseAmountCents(amountStr)
	if err != nil {
		return models.Transaction{}, fmt.Sprintf("row %d: %v", lineNum, err)
	}

	// --- Description / Merchant ---
	description := get(descIdx)
	merchant := ""
	if merchantIdx >= 0 {
		merchant = get(merchantIdx)
	}

	if description == "" && merchant == "" {
		return models.Transaction{}, fmt.Sprintf("row %d: no description or merchant value", lineNum)
	}

	// When there is no separate merchant column, mirror Python: set merchant = description.
	if merchant == "" {
		merchant = description
	}

	// --- Fingerprint ---
	display := merchant
	if display == "" {
		display = description
	}
	fpRaw := fmt.Sprintf("%s|%d|%s|%s",
		postedAt.Format("2006-01-02"), amountCents, display, accountID)
	sum := sha256.Sum256([]byte(fpRaw))
	fingerprint := "csv_" + fmt.Sprintf("%x", sum)[:32]

	return models.Transaction{
		AccountID:   accountID,
		PostedAt:    postedAt,
		AmountCents: amountCents,
		Currency:    "USD",
		Description: description,
		Merchant:    merchant,
		Fingerprint: fingerprint,
		Pending:     false,
	}, ""
}

// parseAmountCents converts a raw amount string (from a CSV cell) to integer
// cents. Handles dollar signs, commas, spaces, and accounting parentheses.
func parseAmountCents(raw string) (int64, error) {
	s := strings.ReplaceAll(raw, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")

	// Accounting-style negatives: (1234.56) -> -1234.56
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = "-" + s[1:len(s)-1]
	}
	if s == "" {
		return 0, nil
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse amount %q: %w", raw, err)
	}

	// Round-half-up (standard financial rounding).
	cents := f * 100
	if cents >= 0 {
		return int64(math.Floor(cents + 0.5)), nil
	}
	return int64(math.Ceil(cents - 0.5)), nil
}

// resolveColumns maps logical roles (date, amount, description, merchant) to
// zero-based column indices within headers. Detection order:
//  1. Known bank format matched by required column presence.
//  2. Generic case-insensitive keyword search as fallback.
func resolveColumns(headers []string, opts ImportOptions) (dateIdx, amountIdx, descIdx, merchantIdx int, err error) {
	dateIdx, amountIdx, descIdx, merchantIdx = -1, -1, -1, -1

	lower := make([]string, len(headers))
	for i, h := range headers {
		lower[i] = strings.ToLower(strings.TrimSpace(h))
	}

	findCI := func(target string) int {
		t := strings.ToLower(strings.TrimSpace(target))
		for i, h := range lower {
			if h == t {
				return i
			}
		}
		return -1
	}

	// Attempt bank auto-detection.
	detectedFmt, detected := detectFormat(lower)

	if detected {
		dateIdx = findCI(detectedFmt.dateCol)
		amountIdx = findCI(detectedFmt.amountCol)
		descIdx = findCI(detectedFmt.descCol)
		if detectedFmt.merchantCol != "" {
			merchantIdx = findCI(detectedFmt.merchantCol)
		}
	} else {
		// Generic fallback: look for keywords in header names.
		for i, h := range lower {
			switch {
			case containsAny(h, "date", "posted", "transaction date"):
				if dateIdx < 0 {
					dateIdx = i
				}
			case containsAny(h, "amount", "debit", "credit"):
				if amountIdx < 0 {
					amountIdx = i
				}
			case containsAny(h, "description", "memo", "narrative", "detail"):
				if descIdx < 0 {
					descIdx = i
				}
			case containsAny(h, "merchant", "payee", "vendor"):
				if merchantIdx < 0 {
					merchantIdx = i
				}
			}
		}
	}

	if dateIdx < 0 {
		return 0, 0, 0, 0, fmt.Errorf("cannot find date column in headers: %v", headers)
	}
	if amountIdx < 0 {
		return 0, 0, 0, 0, fmt.Errorf("cannot find amount column in headers: %v", headers)
	}
	if descIdx < 0 && merchantIdx < 0 {
		return 0, 0, 0, 0, fmt.Errorf("cannot find description or merchant column in headers: %v", headers)
	}
	if descIdx < 0 {
		descIdx = merchantIdx // use merchant as description when description is absent
	}

	return dateIdx, amountIdx, descIdx, merchantIdx, nil
}

// detectFormat returns the bankFormat whose required columns are all present in
// the lowercased headers slice. Returns (zero, false) when no format matches.
func detectFormat(lowerHeaders []string) (bankFormat, bool) {
	headerSet := make(map[string]bool, len(lowerHeaders))
	for _, h := range lowerHeaders {
		headerSet[h] = true
	}

	for _, fmt := range knownFormats {
		required := []string{
			strings.ToLower(fmt.dateCol),
			strings.ToLower(fmt.amountCol),
			strings.ToLower(fmt.descCol),
		}
		matched := true
		for _, col := range required {
			if col != "" && !headerSet[col] {
				matched = false
				break
			}
		}
		if matched {
			return fmt, true
		}
	}
	return bankFormat{}, false
}

// containsAny reports whether s equals any of the given candidates.
func containsAny(s string, candidates ...string) bool {
	for _, c := range candidates {
		if s == c {
			return true
		}
	}
	return false
}

// DetectBank returns the display name of the bank whose CSV format matches the
// given headers, or an empty string when no bank is recognised.
func DetectBank(headers []string) string {
	lower := make([]string, len(headers))
	for i, h := range headers {
		lower[i] = strings.ToLower(strings.TrimSpace(h))
	}
	if fmt, ok := detectFormat(lower); ok {
		return fmt.displayName
	}
	return ""
}
