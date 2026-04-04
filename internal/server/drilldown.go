package server

import (
	"log"
	"net/http"
	"strings"

	"github.com/arclighteng/fin-go/internal/categorize"
	"github.com/arclighteng/fin-go/internal/db"
)

// handleAPIDrilldown returns filtered transactions for a given scope and date range.
//
// Request:  GET /api/drilldown?scope=income&start_date=2026-01-01&end_date=2026-02-01&limit=100
// Scopes:   income, spend, category:{id}, merchant:{name}
// Response: JSON with title, total_cents, and transactions array.
func (s *Server) handleAPIDrilldown(w http.ResponseWriter, r *http.Request) {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing scope parameter"})
		return
	}
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	if startDate == "" || endDate == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_date and end_date required"})
		return
	}

	// Fetch all transactions in the date range.
	txns, err := s.db.GetTransactionsWithAccounts(startDate, endDate)
	if err != nil {
		log.Printf("handleAPIDrilldown: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Load category overrides for accurate classification.
	overrides, _ := s.db.GetCategoryOverrides()

	// Filter by scope.
	var title string
	var filtered []db.SearchResult
	switch {
	case scope == "income":
		title = "Income"
		for _, t := range txns {
			if t.AmountCents > 0 {
				filtered = append(filtered, t)
			}
		}
	case scope == "spend":
		title = "Expenses"
		for _, t := range txns {
			if t.AmountCents < 0 {
				filtered = append(filtered, t)
			}
		}
	case strings.HasPrefix(scope, "category:"):
		catID := strings.TrimPrefix(scope, "category:")
		if cat, ok := categorize.Categories[catID]; ok {
			title = cat.Name
		} else {
			title = catID
		}
		for _, t := range txns {
			if t.AmountCents >= 0 {
				continue // skip income for category drilldown
			}
			txnCat := classifyTransaction(t, overrides)
			if txnCat == catID {
				filtered = append(filtered, t)
			}
		}
	case strings.HasPrefix(scope, "merchant:"):
		merchantQuery := strings.ToLower(strings.TrimPrefix(scope, "merchant:"))
		title = merchantQuery
		for _, t := range txns {
			if strings.ToLower(t.Merchant) == merchantQuery {
				filtered = append(filtered, t)
			}
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown scope: " + scope})
		return
	}

	// Build response.
	var totalCents int64
	rows := make([]map[string]any, 0, len(filtered))
	for _, t := range filtered {
		totalCents += t.AmountCents
		rows = append(rows, map[string]any{
			"date":         t.PostedAt.Format("2006-01-02"),
			"merchant":     t.Merchant,
			"description":  t.Description,
			"amount_cents": t.AmountCents,
			"account_name": t.AccountName,
			"fingerprint":  t.Fingerprint,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"title":        title,
		"total_cents":  totalCents,
		"count":        len(rows),
		"transactions": rows,
	})
}

// classifyTransaction returns the category ID for a transaction,
// checking overrides first, then falling back to the auto-classifier.
func classifyTransaction(t db.SearchResult, overrides map[string]string) string {
	merchantNorm := strings.ToLower(strings.TrimSpace(t.Merchant))
	if catID, ok := overrides[merchantNorm]; ok {
		return catID
	}
	catID, _ := categorize.CategorizeMerchant(merchantNorm, t.Description)
	return catID
}
