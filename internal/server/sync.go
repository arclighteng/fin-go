package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/arclighteng/fin-go/internal/credentials"
	"github.com/arclighteng/fin-go/internal/simplefin"
)

// syncMu guards the rate-limit check and run recording so that concurrent
// POST /api/sync requests cannot both pass the check before either records a run.
var syncMu sync.Mutex

func (s *Server) handleAPISync(w http.ResponseWriter, r *http.Request) {
	syncMu.Lock()
	defer syncMu.Unlock()

	// Check rate limit
	count, err := s.db.RunsInLast24Hours()
	if err != nil {
		log.Printf("handleAPISync: RunsInLast24Hours: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if count >= MaxSyncsPerDay {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":       fmt.Sprintf("Rate limit reached: max %d syncs per 24 hours", MaxSyncsPerDay),
			"syncs_today": count,
			"limit":       MaxSyncsPerDay,
		})
		return
	}

	// Get access URL
	accessURL, err := credentials.GetSimpleFinURL()
	if err != nil || accessURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "No SimpleFIN access URL configured. Run: fin setup <access-url>",
		})
		return
	}

	// Parse lookback from request body (optional)
	lookbackDays := 30
	if r.Body != nil {
		var body struct {
			LookbackDays int `json:"lookback_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.LookbackDays > 0 {
			lookbackDays = body.LookbackDays
		}
	}
	// Cap lookback to two years to prevent unreasonably large API requests.
	const maxLookbackDays = 730
	if lookbackDays > maxLookbackDays {
		lookbackDays = maxLookbackDays
	}

	client, err := simplefin.NewClient(accessURL)
	if err != nil {
		log.Printf("handleAPISync: NewClient: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	result, err := client.Fetch(context.Background(), lookbackDays)
	if err != nil {
		log.Printf("handleAPISync: Fetch: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "SimpleFIN fetch failed",
		})
		return
	}

	// Upsert accounts
	if err := s.db.UpsertAccounts(result.Accounts); err != nil {
		log.Printf("handleAPISync: UpsertAccounts: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Upsert transactions
	inserted, updated, err := s.db.UpsertTransactions(result.Transactions)
	if err != nil {
		log.Printf("handleAPISync: UpsertTransactions: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Record run
	if err := s.db.RecordRun(lookbackDays, len(result.Transactions), inserted, updated); err != nil {
		log.Printf("warning: failed to record run: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"accounts_synced":       len(result.Accounts),
		"transactions_fetched":  len(result.Transactions),
		"transactions_inserted": inserted,
		"transactions_updated":  updated,
		"syncs_today":           count + 1,
	})
}
