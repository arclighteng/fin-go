package server

import (
	"github.com/arclighteng/fin-go/internal/classify"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/arclighteng/fin-go/internal/models"
)

// DetectResult summarises one auto-detection persist pass.
type DetectResult struct {
	Detected int // subscriptions found in transaction history
	Inserted int // new commitments created
	Updated  int // existing detected commitments refreshed
	Skipped  int // detections that hit a user-owned commitment and were left alone
}

// DetectAndPersistCommitments runs recurring/subscription detection over the
// account's full transaction history and persists detected subscriptions into
// the commitments table with provenance source="detected".
//
// This is the single shared entry point used by BOTH the web (server) and CLI
// sync paths, so auto-detection behaves identically regardless of how a sync
// was triggered. It is safe to call after every sync:
//
//   - Idempotent: detections are keyed on (merchant_norm, direction), so a
//     re-sync refreshes existing detected rows in place instead of duplicating.
//   - Non-destructive: a commitment the user created, confirmed, or dismissed is
//     never overwritten (see db.UpsertDetectedCommitment).
func DetectAndPersistCommitments(database *db.DB) (DetectResult, error) {
	var res DetectResult

	detected, err := classify.DetectRecurring(database.Underlying(), classify.RecurringOptions{})
	if err != nil {
		return res, err
	}
	res.Detected = len(detected)

	for _, d := range detected {
		amount := d.AmountCents
		c := models.Commitment{
			Name:          d.Name,
			MerchantNorm:  d.MerchantNorm,
			ExpectedCents: &amount,
			Cadence:       d.Cadence,
			Direction:     d.Direction,
			Source:        "detected",
			ReferenceDate: d.LastSeen.Format("2006-01-02"),
		}
		if d.DayOfMonth > 0 {
			dom := d.DayOfMonth
			c.DayOfMonth = &dom
		}

		action, err := database.UpsertDetectedCommitment(c)
		if err != nil {
			return res, err
		}
		switch action {
		case "inserted":
			res.Inserted++
		case "updated":
			res.Updated++
		default:
			res.Skipped++
		}
	}

	return res, nil
}
