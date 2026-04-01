// Package closebooks implements period closing and post-close adjustment
// tracking ("close the books").
//
// TRUTH CONTRACT:
//   - A closed period holds a snapshot of the canonical totals that becomes
//     the "official" numbers for that date range.
//   - Any transactions that arrive in the database after a period is closed
//     are surfaced as post-close adjustments — never silently mixed into the
//     frozen snapshot.
//   - Closing a period a second time supersedes the previous close record
//     rather than creating a duplicate; the old record is marked "superseded".
package closebooks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// ClosedPeriod is the canonical snapshot of a finalized reporting period.
type ClosedPeriod struct {
	ID       int64
	Start    time.Time // inclusive
	End      time.Time // exclusive (same semantics as the rest of the codebase)
	ClosedAt time.Time
	ClosedBy string // empty when no user identifier is available

	// Snapshot totals in cents.
	IncomeCents              int64
	FixedObligationsCents    int64
	VariableEssentialsCents  int64
	DiscretionaryCents       int64
	OneOffsCents             int64
	RefundsCents             int64
	CreditsOtherCents        int64
	TransfersInCents         int64
	TransfersOutCents        int64

	// Integrity provenance.
	ReportHash       string
	SnapshotID       string
	TransactionCount int

	// Optional JSON-serialised list of account IDs. Nil means all accounts.
	AccountFilter []string

	// "closed" or "superseded".
	Status string
	Notes  string
}

// TotalExpensesCents returns the sum of all expense buckets.
func (cp *ClosedPeriod) TotalExpensesCents() int64 {
	return cp.FixedObligationsCents +
		cp.VariableEssentialsCents +
		cp.DiscretionaryCents +
		cp.OneOffsCents
}

// NetCents returns income + refunds − total expenses.
func (cp *ClosedPeriod) NetCents() int64 {
	return cp.IncomeCents + cp.RefundsCents - cp.TotalExpensesCents()
}

// PostCloseAdjustment represents a transaction that arrived in the database
// after its enclosing period was closed.
type PostCloseAdjustment struct {
	ID             int64
	ClosedPeriodID int64
	Fingerprint    string
	DetectedAt     time.Time
	AdjustmentType string // "new_txn", "modified_txn", "deleted_txn"

	// Transaction snapshot at detection time; pointers because the
	// underlying columns are nullable.
	PostedAt     *time.Time
	AmountCents  *int64
	MerchantNorm *string
	Description  *string

	// Resolution state: "pending", "acknowledged", "incorporated".
	Status          string
	ResolvedAt      *time.Time
	ResolvedBy      *string
	ResolutionNotes *string
}

// AdjustmentSummary aggregates the adjustments for a single closed period.
type AdjustmentSummary struct {
	Period                ClosedPeriod
	PendingAdjustments    []PostCloseAdjustment
	TotalAdjustmentCents  int64
	AdjustedNetCents      int64
}

// HasPending reports whether there are unresolved adjustments.
func (s *AdjustmentSummary) HasPending() bool {
	return len(s.PendingAdjustments) > 0
}

// NetChangeCents returns the difference between the adjusted net and the
// original closed-period net.
func (s *AdjustmentSummary) NetChangeCents() int64 {
	return s.AdjustedNetCents - s.Period.NetCents()
}

// PeriodTotals carries the snapshot values that the caller provides when
// closing a period. In the full app this comes from the canonical report
// engine; here it is passed in to keep the package decoupled from the
// report layer.
type PeriodTotals struct {
	IncomeCents             int64
	FixedObligationsCents   int64
	VariableEssentialsCents int64
	DiscretionaryCents      int64
	OneOffsCents            int64
	RefundsCents            int64
	CreditsOtherCents       int64
	TransfersInCents        int64
	TransfersOutCents       int64
	ReportHash              string
	SnapshotID              string
	TransactionCount        int
}

// CloseOptions holds optional parameters for ClosePeriod.
type CloseOptions struct {
	AccountFilter []string
	ClosedBy      string
	Notes         string
}

// ClosePeriod creates an official snapshot for [start, end).
// If the period has already been closed, the existing record is superseded
// and a fresh record is inserted.
func ClosePeriod(db *sql.DB, start, end time.Time, totals PeriodTotals, opts CloseOptions) (*ClosedPeriod, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	filterJSON, err := encodeFilter(opts.AccountFilter)
	if err != nil {
		return nil, err
	}

	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")
	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("closebooks: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Supersede any existing closed record for this exact range + filter.
	var existingID int64
	err = tx.QueryRow(`
		SELECT id FROM closed_periods
		WHERE start_date = ? AND end_date = ?
		  AND (account_filter IS ? OR (account_filter IS NOT NULL AND account_filter = ?))
		  AND status = 'closed'`,
		startStr, endStr, filterJSON, filterJSON,
	).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("closebooks: check existing: %w", err)
	}
	if err == nil {
		if _, err := tx.Exec(
			"UPDATE closed_periods SET status = 'superseded' WHERE id = ?",
			existingID,
		); err != nil {
			return nil, fmt.Errorf("closebooks: supersede existing: %w", err)
		}
	}

	res, err := tx.Exec(`
		INSERT INTO closed_periods (
			start_date, end_date, closed_at, closed_by,
			income_cents, fixed_obligations_cents, variable_essentials_cents,
			discretionary_cents, one_offs_cents, refunds_cents, credits_other_cents,
			transfers_in_cents, transfers_out_cents,
			report_hash, snapshot_id, transaction_count,
			account_filter, status, notes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'closed', ?)`,
		startStr, endStr, nowISO, nullableString(opts.ClosedBy),
		totals.IncomeCents, totals.FixedObligationsCents, totals.VariableEssentialsCents,
		totals.DiscretionaryCents, totals.OneOffsCents, totals.RefundsCents,
		totals.CreditsOtherCents, totals.TransfersInCents, totals.TransfersOutCents,
		totals.ReportHash, totals.SnapshotID, totals.TransactionCount,
		filterJSON, nullableString(opts.Notes),
	)
	if err != nil {
		return nil, fmt.Errorf("closebooks: insert closed period: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("closebooks: get insert id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("closebooks: commit: %w", err)
	}

	return &ClosedPeriod{
		ID:                      id,
		Start:                   start,
		End:                     end,
		ClosedAt:                now,
		ClosedBy:                opts.ClosedBy,
		IncomeCents:             totals.IncomeCents,
		FixedObligationsCents:   totals.FixedObligationsCents,
		VariableEssentialsCents: totals.VariableEssentialsCents,
		DiscretionaryCents:      totals.DiscretionaryCents,
		OneOffsCents:            totals.OneOffsCents,
		RefundsCents:            totals.RefundsCents,
		CreditsOtherCents:       totals.CreditsOtherCents,
		TransfersInCents:        totals.TransfersInCents,
		TransfersOutCents:       totals.TransfersOutCents,
		ReportHash:              totals.ReportHash,
		SnapshotID:              totals.SnapshotID,
		TransactionCount:        totals.TransactionCount,
		AccountFilter:           opts.AccountFilter,
		Status:                  "closed",
		Notes:                   opts.Notes,
	}, nil
}

// GetClosedPeriod returns the active closed-period record for [start, end)
// with the given account filter, or nil when none exists.
func GetClosedPeriod(db *sql.DB, start, end time.Time, accountFilter []string) (*ClosedPeriod, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	filterJSON, err := encodeFilter(accountFilter)
	if err != nil {
		return nil, err
	}

	row := db.QueryRow(`
		SELECT id, start_date, end_date, closed_at, closed_by,
		       income_cents, fixed_obligations_cents, variable_essentials_cents,
		       discretionary_cents, one_offs_cents, refunds_cents, credits_other_cents,
		       transfers_in_cents, transfers_out_cents,
		       report_hash, snapshot_id, transaction_count,
		       account_filter, status, COALESCE(notes, '')
		FROM closed_periods
		WHERE start_date = ? AND end_date = ?
		  AND (account_filter IS ? OR (account_filter IS NOT NULL AND account_filter = ?))
		  AND status = 'closed'`,
		start.Format("2006-01-02"), end.Format("2006-01-02"), filterJSON, filterJSON,
	)

	cp, err := scanClosedPeriod(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("closebooks: get closed period: %w", err)
	}
	return cp, nil
}

// ListClosedPeriods returns all active closed periods, most-recent first.
func ListClosedPeriods(db *sql.DB) ([]ClosedPeriod, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT id, start_date, end_date, closed_at, closed_by,
		       income_cents, fixed_obligations_cents, variable_essentials_cents,
		       discretionary_cents, one_offs_cents, refunds_cents, credits_other_cents,
		       transfers_in_cents, transfers_out_cents,
		       report_hash, snapshot_id, transaction_count,
		       account_filter, status, COALESCE(notes, '')
		FROM closed_periods
		WHERE status = 'closed'
		ORDER BY closed_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("closebooks: list closed periods: %w", err)
	}
	defer rows.Close()

	var periods []ClosedPeriod
	for rows.Next() {
		cp, err := scanClosedPeriodRow(rows)
		if err != nil {
			return nil, err
		}
		periods = append(periods, *cp)
	}
	return periods, rows.Err()
}

// DetectPostCloseAdjustments compares the current transactions in the period
// against the closed snapshot's transaction count and records any new
// fingerprints as pending adjustments.
func DetectPostCloseAdjustments(db *sql.DB, period ClosedPeriod) ([]PostCloseAdjustment, error) {
	// Collect current posted fingerprints in the period.
	rows, err := db.Query(`
		SELECT fingerprint, posted_at, amount_cents,
		       TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), ''))) AS merchant_norm,
		       COALESCE(description, merchant, '') AS description
		FROM transactions
		WHERE posted_at >= ? AND posted_at < ?
		  AND COALESCE(pending, 0) = 0`,
		period.Start.Format("2006-01-02"), period.End.Format("2006-01-02"),
	)
	if err != nil {
		return nil, fmt.Errorf("closebooks: query current txns: %w", err)
	}
	defer rows.Close()

	type txnSnap struct {
		postedAt    string
		amountCents int64
		merchant    string
		description string
	}
	current := make(map[string]txnSnap)
	for rows.Next() {
		var fp, postedAt, merchant, desc string
		var amount int64
		if err := rows.Scan(&fp, &postedAt, &amount, &merchant, &desc); err != nil {
			return nil, fmt.Errorf("closebooks: scan txn row: %w", err)
		}
		current[fp] = txnSnap{postedAt: postedAt, amountCents: amount, merchant: merchant, description: desc}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("closebooks: iterate txns: %w", err)
	}

	// Collect fingerprints already recorded as adjustments.
	adjRows, err := db.Query(
		"SELECT fingerprint FROM post_close_adjustments WHERE closed_period_id = ?",
		period.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("closebooks: query existing adjustments: %w", err)
	}
	defer adjRows.Close()

	existing := make(map[string]struct{})
	for adjRows.Next() {
		var fp string
		if err := adjRows.Scan(&fp); err != nil {
			return nil, fmt.Errorf("closebooks: scan adjustment fp: %w", err)
		}
		existing[fp] = struct{}{}
	}
	if err := adjRows.Err(); err != nil {
		return nil, fmt.Errorf("closebooks: iterate adjustment fps: %w", err)
	}

	// Only proceed if transaction count has grown since close.
	if len(current) <= period.TransactionCount {
		return nil, nil
	}

	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339)

	var newAdj []PostCloseAdjustment
	for fp, snap := range current {
		if _, seen := existing[fp]; seen {
			continue
		}

		res, err := db.Exec(`
			INSERT OR IGNORE INTO post_close_adjustments (
				closed_period_id, fingerprint, detected_at, adjustment_type,
				posted_at, amount_cents, merchant_norm, description, status
			) VALUES (?, ?, ?, 'new_txn', ?, ?, ?, ?, 'pending')`,
			period.ID, fp, nowISO,
			snap.postedAt, snap.amountCents, snap.merchant, snap.description,
		)
		if err != nil {
			return nil, fmt.Errorf("closebooks: insert adjustment: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue
		}
		id, _ := res.LastInsertId()

		postedAt, _ := time.Parse("2006-01-02", snap.postedAt)
		a := snap.amountCents
		m := snap.merchant
		d := snap.description
		newAdj = append(newAdj, PostCloseAdjustment{
			ID:             id,
			ClosedPeriodID: period.ID,
			Fingerprint:    fp,
			DetectedAt:     now,
			AdjustmentType: "new_txn",
			PostedAt:       &postedAt,
			AmountCents:    &a,
			MerchantNorm:   &m,
			Description:    &d,
			Status:         "pending",
		})
	}
	return newAdj, nil
}

// ListPendingAdjustments returns pending post-close adjustments, optionally
// filtered to a single closed period (pass 0 for all periods).
func ListPendingAdjustments(db *sql.DB, closedPeriodID int64) ([]PostCloseAdjustment, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	var rows *sql.Rows
	var err error
	if closedPeriodID > 0 {
		rows, err = db.Query(`
			SELECT id, closed_period_id, fingerprint, detected_at, adjustment_type,
			       posted_at, amount_cents, merchant_norm, description,
			       status, resolved_at, resolved_by, resolution_notes
			FROM post_close_adjustments
			WHERE closed_period_id = ? AND status = 'pending'
			ORDER BY detected_at DESC`,
			closedPeriodID,
		)
	} else {
		rows, err = db.Query(`
			SELECT id, closed_period_id, fingerprint, detected_at, adjustment_type,
			       posted_at, amount_cents, merchant_norm, description,
			       status, resolved_at, resolved_by, resolution_notes
			FROM post_close_adjustments
			WHERE status = 'pending'
			ORDER BY detected_at DESC`)
	}
	if err != nil {
		return nil, fmt.Errorf("closebooks: query pending adjustments: %w", err)
	}
	defer rows.Close()

	var adjs []PostCloseAdjustment
	for rows.Next() {
		a, err := scanAdjustment(rows)
		if err != nil {
			return nil, err
		}
		adjs = append(adjs, a)
	}
	return adjs, rows.Err()
}

// AcknowledgeAdjustment marks an adjustment as acknowledged by the user.
func AcknowledgeAdjustment(db *sql.DB, adjustmentID int64, resolvedBy, notes string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE post_close_adjustments
		SET status = 'acknowledged', resolved_at = ?, resolved_by = ?, resolution_notes = ?
		WHERE id = ?`,
		now, nullableString(resolvedBy), nullableString(notes), adjustmentID,
	)
	if err != nil {
		return fmt.Errorf("closebooks: acknowledge adjustment %d: %w", adjustmentID, err)
	}
	return nil
}

// GetAdjustmentSummary returns a summary of adjustments for a closed period.
func GetAdjustmentSummary(db *sql.DB, period ClosedPeriod) (*AdjustmentSummary, error) {
	pending, err := ListPendingAdjustments(db, period.ID)
	if err != nil {
		return nil, err
	}

	var total int64
	for _, a := range pending {
		if a.AmountCents != nil {
			total += *a.AmountCents
		}
	}

	return &AdjustmentSummary{
		Period:               period,
		PendingAdjustments:   pending,
		TotalAdjustmentCents: total,
		AdjustedNetCents:     period.NetCents() + total,
	}, nil
}

// CheckAdjustmentsOnIngest iterates all active closed periods and detects
// new adjustments. Call this after each sync. Returns a map of
// closedPeriodID → new adjustments detected.
func CheckAdjustmentsOnIngest(db *sql.DB) (map[int64][]PostCloseAdjustment, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	periods, err := ListClosedPeriods(db)
	if err != nil {
		return nil, err
	}

	result := make(map[int64][]PostCloseAdjustment)
	for _, p := range periods {
		newAdj, err := DetectPostCloseAdjustments(db, p)
		if err != nil {
			return nil, err
		}
		if len(newAdj) > 0 {
			result[p.ID] = newAdj
		}
	}
	return result, nil
}

// SaveStatementMatch records a user-confirmed statement-to-transaction match.
func SaveStatementMatch(
	db *sql.DB,
	fingerprint string,
	statementDate *time.Time,
	statementAmountCents *int64,
	statementDescription *string,
	matchedBy string,
	confidence string,
) error {
	if err := EnsureSchema(db); err != nil {
		return err
	}
	if confidence == "" {
		confidence = "user_confirmed"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var dateStr *string
	if statementDate != nil {
		s := statementDate.Format("2006-01-02")
		dateStr = &s
	}

	_, err := db.Exec(`
		INSERT INTO statement_matches (
			fingerprint, matched_at, matched_by, confidence,
			statement_date, statement_amount_cents, statement_description
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			matched_at                = excluded.matched_at,
			matched_by                = excluded.matched_by,
			confidence                = excluded.confidence,
			statement_date            = excluded.statement_date,
			statement_amount_cents    = excluded.statement_amount_cents,
			statement_description     = excluded.statement_description`,
		fingerprint, now, nullableString(matchedBy), confidence,
		dateStr, statementAmountCents, statementDescription,
	)
	if err != nil {
		return fmt.Errorf("closebooks: save statement match: %w", err)
	}
	return nil
}

// GetMatchedFingerprints returns the set of all statement-matched fingerprints.
func GetMatchedFingerprints(db *sql.DB) (map[string]struct{}, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}

	rows, err := db.Query("SELECT fingerprint FROM statement_matches")
	if err != nil {
		return nil, fmt.Errorf("closebooks: get matched fingerprints: %w", err)
	}
	defer rows.Close()

	result := make(map[string]struct{})
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return nil, fmt.Errorf("closebooks: scan fingerprint: %w", err)
		}
		result[fp] = struct{}{}
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanClosedPeriod(row rowScanner) (*ClosedPeriod, error) {
	var cp ClosedPeriod
	var startStr, endStr, closedAtStr, statusStr, notes string
	var closedBy sql.NullString
	var filterStr sql.NullString

	err := row.Scan(
		&cp.ID, &startStr, &endStr, &closedAtStr, &closedBy,
		&cp.IncomeCents, &cp.FixedObligationsCents, &cp.VariableEssentialsCents,
		&cp.DiscretionaryCents, &cp.OneOffsCents, &cp.RefundsCents,
		&cp.CreditsOtherCents, &cp.TransfersInCents, &cp.TransfersOutCents,
		&cp.ReportHash, &cp.SnapshotID, &cp.TransactionCount,
		&filterStr, &statusStr, &notes,
	)
	if err != nil {
		return nil, err
	}

	cp.Start, _ = time.Parse("2006-01-02", startStr)
	cp.End, _ = time.Parse("2006-01-02", endStr)
	cp.ClosedAt, _ = time.Parse(time.RFC3339, closedAtStr)
	cp.ClosedBy = closedBy.String
	cp.Status = statusStr
	cp.Notes = notes

	if filterStr.Valid && filterStr.String != "" {
		if err := json.Unmarshal([]byte(filterStr.String), &cp.AccountFilter); err != nil {
			return nil, fmt.Errorf("closebooks: unmarshal account_filter: %w", err)
		}
	}

	return &cp, nil
}

func scanClosedPeriodRow(rows *sql.Rows) (*ClosedPeriod, error) {
	var cp ClosedPeriod
	var startStr, endStr, closedAtStr, statusStr, notes string
	var closedBy sql.NullString
	var filterStr sql.NullString

	if err := rows.Scan(
		&cp.ID, &startStr, &endStr, &closedAtStr, &closedBy,
		&cp.IncomeCents, &cp.FixedObligationsCents, &cp.VariableEssentialsCents,
		&cp.DiscretionaryCents, &cp.OneOffsCents, &cp.RefundsCents,
		&cp.CreditsOtherCents, &cp.TransfersInCents, &cp.TransfersOutCents,
		&cp.ReportHash, &cp.SnapshotID, &cp.TransactionCount,
		&filterStr, &statusStr, &notes,
	); err != nil {
		return nil, fmt.Errorf("closebooks: scan closed period: %w", err)
	}

	cp.Start, _ = time.Parse("2006-01-02", startStr)
	cp.End, _ = time.Parse("2006-01-02", endStr)
	cp.ClosedAt, _ = time.Parse(time.RFC3339, closedAtStr)
	cp.ClosedBy = closedBy.String
	cp.Status = statusStr
	cp.Notes = notes

	if filterStr.Valid && filterStr.String != "" {
		if err := json.Unmarshal([]byte(filterStr.String), &cp.AccountFilter); err != nil {
			return nil, fmt.Errorf("closebooks: unmarshal account_filter: %w", err)
		}
	}

	return &cp, nil
}

func scanAdjustment(rows *sql.Rows) (PostCloseAdjustment, error) {
	var a PostCloseAdjustment
	var detectedStr string
	var postedStr sql.NullString
	var amountCents sql.NullInt64
	var merchant, desc, resolvedBy, resNotes, resolvedAt sql.NullString

	if err := rows.Scan(
		&a.ID, &a.ClosedPeriodID, &a.Fingerprint, &detectedStr, &a.AdjustmentType,
		&postedStr, &amountCents, &merchant, &desc,
		&a.Status, &resolvedAt, &resolvedBy, &resNotes,
	); err != nil {
		return PostCloseAdjustment{}, fmt.Errorf("closebooks: scan adjustment: %w", err)
	}

	a.DetectedAt, _ = time.Parse(time.RFC3339, detectedStr)

	if postedStr.Valid {
		t, _ := time.Parse("2006-01-02", postedStr.String)
		a.PostedAt = &t
	}
	if amountCents.Valid {
		v := amountCents.Int64
		a.AmountCents = &v
	}
	if merchant.Valid {
		s := merchant.String
		a.MerchantNorm = &s
	}
	if desc.Valid {
		s := desc.String
		a.Description = &s
	}
	if resolvedAt.Valid {
		t, _ := time.Parse(time.RFC3339, resolvedAt.String)
		a.ResolvedAt = &t
	}
	if resolvedBy.Valid {
		s := resolvedBy.String
		a.ResolvedBy = &s
	}
	if resNotes.Valid {
		s := resNotes.String
		a.ResolutionNotes = &s
	}

	return a, nil
}

// encodeFilter serialises an account filter slice to a stable JSON string for
// storage. A nil or empty slice returns nil (stored as SQL NULL).
func encodeFilter(filter []string) (*string, error) {
	if len(filter) == 0 {
		return nil, nil
	}
	sorted := make([]string, len(filter))
	copy(sorted, filter)
	sort.Strings(sorted)
	b, err := json.Marshal(sorted)
	if err != nil {
		return nil, fmt.Errorf("closebooks: encode account filter: %w", err)
	}
	s := string(b)
	return &s, nil
}

// nullableString returns nil for an empty string (maps to SQL NULL) and a
// pointer to the value otherwise.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
