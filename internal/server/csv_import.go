package server

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/arclighteng/fin-go/internal/csvimport"
	"github.com/arclighteng/fin-go/internal/models"
)

// maxCSVUploadSize limits CSV file uploads to 10 MB.
const maxCSVUploadSize = 10 << 20

// handleAPICSVPreview parses an uploaded CSV file and returns a preview of the
// detected bank format and first few rows without writing anything to the DB.
//
// Request:  POST /api/import/csv/preview  (multipart/form-data, field "file")
// Response: JSON with row_count, detected_bank, bank_display_name, headers,
//           column_mapping, and preview (up to 5 rows).
func (s *Server) handleAPICSVPreview(w http.ResponseWriter, r *http.Request) {
	csvBytes, headers, err := readCSVUpload(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Parse with DryRun to get transactions without DB writes.
	result, err := csvimport.Import(strings.NewReader(string(csvBytes)), csvimport.ImportOptions{DryRun: true})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("CSV parse error: %v", err)})
		return
	}

	bank := csvimport.DetectBank(headers)

	// Build column mapping from detected headers.
	colMapping := buildColumnMapping(headers)

	// Build preview rows (up to 5).
	maxPreview := 5
	if len(result.Transactions) < maxPreview {
		maxPreview = len(result.Transactions)
	}
	preview := make([]map[string]any, 0, maxPreview)
	for i := 0; i < maxPreview; i++ {
		txn := result.Transactions[i]
		preview = append(preview, map[string]any{
			"date":        txn.PostedAt.Format("2006-01-02"),
			"amount":      float64(txn.AmountCents) / 100.0,
			"description": txn.Description,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"row_count":         len(result.Transactions),
		"detected_bank":     bank,
		"bank_display_name": bank,
		"headers":           headers,
		"column_mapping":    colMapping,
		"preview":           preview,
	})
}

// handleAPICSVConfirm parses an uploaded CSV file and writes the transactions
// to the database.
//
// Request:  POST /api/import/csv/confirm  (multipart/form-data, field "file")
//           Optional query params: date_col, amount_col, description_col
// Response: JSON with imported count and skipped_duplicates count.
func (s *Server) handleAPICSVConfirm(w http.ResponseWriter, r *http.Request) {
	csvBytes, _, err := readCSVUpload(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	result, err := csvimport.Import(strings.NewReader(string(csvBytes)), csvimport.ImportOptions{})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("CSV parse error: %v", err)})
		return
	}

	if len(result.Transactions) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"imported":           0,
			"skipped_duplicates": 0,
		})
		return
	}

	// Ensure the csv-import account exists.
	accountID := "csv-import"
	if err := s.db.UpsertAccounts([]models.Account{
		{
			AccountID:   accountID,
			Institution: "Manual Import",
			Name:        accountID,
			Type:        "checking",
			Currency:    "USD",
		},
	}); err != nil {
		log.Printf("handleAPICSVConfirm: ensure account: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	inserted, _, err := s.db.UpsertTransactions(result.Transactions)
	if err != nil {
		log.Printf("handleAPICSVConfirm: upsert: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	skipped := len(result.Transactions) - inserted

	writeJSON(w, http.StatusOK, map[string]any{
		"imported":           inserted,
		"skipped_duplicates": skipped,
	})
}

// readCSVUpload extracts the "file" field from a multipart form upload,
// reads its contents, and parses the CSV header row. Returns the raw bytes,
// the header strings, and any error.
func readCSVUpload(w http.ResponseWriter, r *http.Request) ([]byte, []string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCSVUploadSize)

	if err := r.ParseMultipartForm(maxCSVUploadSize); err != nil {
		return nil, nil, fmt.Errorf("invalid multipart form: %w", err)
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, nil, fmt.Errorf("missing 'file' field in upload")
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, fmt.Errorf("read uploaded file: %w", err)
	}

	if len(data) == 0 {
		return nil, nil, fmt.Errorf("uploaded file is empty")
	}

	// Parse just the header row.
	cr := csv.NewReader(strings.NewReader(string(data)))
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	headers, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read CSV headers: %w", err)
	}

	return data, headers, nil
}

// buildColumnMapping returns a map of role -> header name based on
// case-insensitive keyword matching, mirroring what csvimport does internally.
func buildColumnMapping(headers []string) map[string]string {
	mapping := map[string]string{}
	for _, h := range headers {
		lower := strings.ToLower(strings.TrimSpace(h))
		switch {
		case containsKeyword(lower, "date", "posted", "transaction date"):
			if _, ok := mapping["date"]; !ok {
				mapping["date"] = h
			}
		case containsKeyword(lower, "amount", "debit", "credit"):
			if _, ok := mapping["amount"]; !ok {
				mapping["amount"] = h
			}
		case containsKeyword(lower, "description", "memo", "narrative", "detail"):
			if _, ok := mapping["description"]; !ok {
				mapping["description"] = h
			}
		}
	}
	return mapping
}

// containsKeyword reports whether s equals any of the given candidates.
func containsKeyword(s string, candidates ...string) bool {
	for _, c := range candidates {
		if s == c {
			return true
		}
	}
	return false
}
