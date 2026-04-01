package classify

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
)

// SnapshotInfo describes the state of the transaction dataset at a point in
// time. Including it in a Report makes the report reproducible: the same
// SnapshotID means the underlying data is identical.
type SnapshotInfo struct {
	// SnapshotID is a 12-character hex prefix of a SHA-256 over key data
	// descriptors. It changes whenever transactions are added, modified, or
	// deleted, or a new ingest run occurs.
	SnapshotID string

	// TransactionCount is the total number of transactions in the database.
	TransactionCount int

	// LatestPostedAt is the MAX(posted_at) across all transactions, or empty
	// when the table is empty.
	LatestPostedAt string

	// LatestIngestRun is the MAX(id) from the ingest_runs table.
	// It is nil when the table does not exist or is empty.
	LatestIngestRun *int64

	// ComputedAt is the UTC timestamp at which this snapshot was computed.
	ComputedAt string
}

// ComputeSnapshotID computes a SnapshotInfo that uniquely identifies the
// current data state.
//
// The SnapshotID is a 12-character hex prefix derived from:
//
//	"{count}|{latest_posted_at}|{latest_updated_at}|{latest_ingest_run}"
//
// Any change to transactions (insert, update, delete) or a new ingest run
// will produce a different SnapshotID.
func ComputeSnapshotID(db *sql.DB) (*SnapshotInfo, error) {
	// Aggregate transaction metrics in a single query.
	var txnCount int
	var latestPosted sql.NullString
	var latestUpdated sql.NullString

	err := db.QueryRow(`
		SELECT
			COUNT(*),
			MAX(posted_at),
			MAX(COALESCE(updated_at, posted_at))
		FROM transactions`,
	).Scan(&txnCount, &latestPosted, &latestUpdated)
	if err != nil {
		return nil, fmt.Errorf("compute snapshot: query transactions: %w", err)
	}

	// MAX(id) from ingest_runs — this table may not exist in all deployments.
	var latestIngestRun *int64
	var runID sql.NullInt64
	err = db.QueryRow("SELECT MAX(id) FROM ingest_runs").Scan(&runID)
	if err == nil && runID.Valid {
		v := runID.Int64
		latestIngestRun = &v
	}
	// Silently ignore the error: the table simply may not exist.

	// Build the hash input string.
	latestPostedStr := latestPosted.String
	latestUpdatedStr := latestUpdated.String
	var latestIngestStr string
	if latestIngestRun != nil {
		latestIngestStr = fmt.Sprintf("%d", *latestIngestRun)
	} else {
		latestIngestStr = "<nil>"
	}

	hashInput := fmt.Sprintf("%d|%s|%s|%s", txnCount, latestPostedStr, latestUpdatedStr, latestIngestStr)
	sum := sha256.Sum256([]byte(hashInput))
	snapshotID := fmt.Sprintf("%x", sum)[:12]

	return &SnapshotInfo{
		SnapshotID:       snapshotID,
		TransactionCount: txnCount,
		LatestPostedAt:   latestPostedStr,
		LatestIngestRun:  latestIngestRun,
		ComputedAt:       time.Now().UTC().Format(time.RFC3339),
	}, nil
}
