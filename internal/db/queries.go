package db

import (
	"database/sql"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/models"
)

// SearchResult holds a transaction plus its resolved account name.
type SearchResult struct {
	models.Transaction
	AccountName string
}

// SearchOptions configures optional filters for SearchTransactions.
type SearchOptions struct {
	// MinDate filters results to posted_at >= this date (ISO 8601). Empty means no filter.
	MinDate string
	// Accounts filters results to these account IDs. Empty means all accounts.
	Accounts []string
}

// SearchTransactions searches by merchant or description using LIKE.
// It joins the accounts table to resolve the human-readable account name.
func (d *DB) SearchTransactions(query string, limit int, opts SearchOptions) ([]SearchResult, error) {
	pattern := "%" + query + "%"

	var where strings.Builder
	args := []any{pattern, pattern}

	where.WriteString("(t.merchant LIKE ? OR t.description LIKE ?)")

	if opts.MinDate != "" {
		where.WriteString(" AND t.posted_at >= ?")
		args = append(args, opts.MinDate)
	}

	if len(opts.Accounts) > 0 {
		placeholders := make([]string, len(opts.Accounts))
		for i, acct := range opts.Accounts {
			placeholders[i] = "?"
			args = append(args, acct)
		}
		where.WriteString(" AND t.account_id IN (" + strings.Join(placeholders, ",") + ")")
	}

	args = append(args, limit)

	rows, err := d.db.Query(`
		SELECT t.account_id, t.posted_at, t.amount_cents, t.currency, t.description, t.merchant,
		       COALESCE(t.source_txn_id, ''), t.fingerprint, COALESCE(t.pending, 0),
		       COALESCE(a.name, t.account_id)
		FROM transactions t
		LEFT JOIN accounts a ON a.account_id = t.account_id
		WHERE `+where.String()+`
		ORDER BY t.posted_at DESC
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		var dateStr string
		if err := rows.Scan(
			&sr.AccountID, &dateStr, &sr.AmountCents, &sr.Currency,
			&sr.Description, &sr.Merchant, &sr.SourceTxnID, &sr.Fingerprint,
			&sr.Pending, &sr.AccountName,
		); err != nil {
			return nil, err
		}
		sr.PostedAt, _ = time.Parse("2006-01-02", dateStr)
		results = append(results, sr)
	}
	return results, rows.Err()
}

// SaveIncomeSource marks a merchant as income or not-income.
// GetTransactionsWithAccounts returns transactions in a date range with resolved
// account names, ordered by posted_at DESC.
func (d *DB) GetTransactionsWithAccounts(startISO, endISO string) ([]SearchResult, error) {
	rows, err := d.db.Query(`
		SELECT t.account_id, t.posted_at, t.amount_cents, t.currency, t.description, t.merchant,
		       COALESCE(t.source_txn_id, ''), t.fingerprint, COALESCE(t.pending, 0),
		       COALESCE(a.name, t.account_id)
		FROM transactions t
		LEFT JOIN accounts a ON a.account_id = t.account_id
		WHERE t.posted_at >= ? AND t.posted_at < ?
		ORDER BY t.posted_at DESC`,
		startISO, endISO,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		var dateStr string
		if err := rows.Scan(
			&sr.AccountID, &dateStr, &sr.AmountCents, &sr.Currency,
			&sr.Description, &sr.Merchant, &sr.SourceTxnID, &sr.Fingerprint,
			&sr.Pending, &sr.AccountName,
		); err != nil {
			return nil, err
		}
		sr.PostedAt, _ = time.Parse("2006-01-02", dateStr)
		results = append(results, sr)
	}
	return results, rows.Err()
}

func (d *DB) SaveIncomeSource(merchant string, isIncome bool) error {
	ruleType := "income"
	if !isIncome {
		ruleType = "not_income"
	}
	now := utcNowISO()
	// Remove any existing rule for this merchant first (a merchant should only be income OR not_income).
	if _, err := d.db.Exec("DELETE FROM merchant_rules WHERE merchant_pattern = ? AND rule_type IN ('income', 'not_income')", merchant); err != nil {
		return err
	}
	_, err := d.db.Exec(`
		INSERT INTO merchant_rules(merchant_pattern, rule_type, created_at)
		VALUES (?, ?, ?)`,
		merchant, ruleType, now,
	)
	return err
}

// GetIncomeSources returns all income-marked merchants.
func (d *DB) GetIncomeSources() ([]string, error) {
	rows, err := d.db.Query("SELECT merchant_pattern FROM merchant_rules WHERE rule_type = 'income'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

// GetCategoryOverrides returns merchant_norm -> category_id map.
func (d *DB) GetCategoryOverrides() (map[string]string, error) {
	rows, err := d.db.Query("SELECT merchant_norm, category_id FROM category_overrides")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var merchant, catID string
		if err := rows.Scan(&merchant, &catID); err != nil {
			return nil, err
		}
		result[merchant] = catID
	}
	return result, rows.Err()
}

// SaveCategoryOverride upserts a category override.
func (d *DB) SaveCategoryOverride(merchant, categoryID string) error {
	_, err := d.db.Exec(`
		INSERT INTO category_overrides(merchant_norm, category_id, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(merchant_norm) DO UPDATE SET category_id=excluded.category_id, updated_at=excluded.updated_at`,
		merchant, categoryID, utcNowISO(), utcNowISO(),
	)
	return err
}

// DeleteCategoryOverride removes a category override.
func (d *DB) DeleteCategoryOverride(merchant string) error {
	_, err := d.db.Exec("DELETE FROM category_overrides WHERE merchant_norm = ?", merchant)
	return err
}

// SaveTypeOverride saves a recurring type override.
func (d *DB) SaveTypeOverride(merchant, overrideType string) error {
	_, err := d.db.Exec(`
		INSERT INTO recurring_type_overrides(merchant_norm, override_type, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(merchant_norm) DO UPDATE SET override_type=excluded.override_type, updated_at=excluded.updated_at`,
		merchant, overrideType, utcNowISO(), utcNowISO(),
	)
	return err
}

// SaveTransactionNote saves a note on a transaction.
func (d *DB) SaveTransactionNote(fingerprint, note string) error {
	note = strings.TrimSpace(note)
	_, err := d.db.Exec(`
		INSERT INTO transaction_notes(fingerprint, note, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET note=excluded.note, updated_at=excluded.updated_at`,
		fingerprint, note, utcNowISO(), utcNowISO(),
	)
	return err
}

// DeleteTransactionNote deletes a note.
func (d *DB) DeleteTransactionNote(fingerprint string) error {
	_, err := d.db.Exec("DELETE FROM transaction_notes WHERE fingerprint = ?", fingerprint)
	return err
}

// AddTransactionTag adds a tag to a transaction.
func (d *DB) AddTransactionTag(fingerprint, tag string) error {
	_, err := d.db.Exec(`
		INSERT OR IGNORE INTO transaction_tags(fingerprint, tag, created_at)
		VALUES (?, ?, ?)`,
		fingerprint, tag, utcNowISO(),
	)
	return err
}

// DeleteTransactionTag removes a tag from a transaction.
func (d *DB) DeleteTransactionTag(fingerprint, tag string) error {
	_, err := d.db.Exec("DELETE FROM transaction_tags WHERE fingerprint = ? AND tag = ?", fingerprint, tag)
	return err
}

// GetTransactionAnnotations returns the note and tags for a transaction.
func (d *DB) GetTransactionAnnotations(fingerprint string) (string, []string, error) {
	var note string
	noteRow := d.db.QueryRow("SELECT note FROM transaction_notes WHERE fingerprint = ?", fingerprint)
	if err := noteRow.Scan(&note); err != nil && err != sql.ErrNoRows {
		return "", nil, err
	}

	tagRows, err := d.db.Query("SELECT tag FROM transaction_tags WHERE fingerprint = ? ORDER BY tag", fingerprint)
	if err != nil {
		return note, nil, err
	}
	defer tagRows.Close()

	var tags []string
	for tagRows.Next() {
		var t string
		if err := tagRows.Scan(&t); err != nil {
			return note, nil, err
		}
		tags = append(tags, t)
	}
	return note, tags, tagRows.Err()
}

// GetAllTags returns all distinct tags.
func (d *DB) GetAllTags() ([]string, error) {
	rows, err := d.db.Query("SELECT DISTINCT tag FROM transaction_tags ORDER BY tag")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// SaveBudgetTarget upserts a budget target.
func (d *DB) SaveBudgetTarget(categoryID string, monthlyCents int64) error {
	_, err := d.db.Exec(`
		INSERT INTO budget_targets(category_id, monthly_target_cents, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(category_id) DO UPDATE SET
		  monthly_target_cents=excluded.monthly_target_cents,
		  updated_at=excluded.updated_at`,
		categoryID, monthlyCents, utcNowISO(), utcNowISO(),
	)
	return err
}

// DeleteBudgetTarget removes a budget target.
func (d *DB) DeleteBudgetTarget(categoryID string) error {
	_, err := d.db.Exec("DELETE FROM budget_targets WHERE category_id = ?", categoryID)
	return err
}

// GetBudgetTargets returns all budget targets.
func (d *DB) GetBudgetTargets() ([]models.BudgetTarget, error) {
	rows, err := d.db.Query("SELECT category_id, monthly_target_cents FROM budget_targets ORDER BY category_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []models.BudgetTarget
	for rows.Next() {
		var t models.BudgetTarget
		if err := rows.Scan(&t.CategoryID, &t.MonthlyTargetCents); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// GetMerchantRules returns income and not_income merchant patterns.
func (d *DB) GetMerchantRules() (incomePatterns, excludedPatterns []string, err error) {
	rows, err := d.db.Query("SELECT merchant_pattern, rule_type FROM merchant_rules WHERE rule_type IN ('income', 'not_income')")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var pattern, ruleType string
		if err := rows.Scan(&pattern, &ruleType); err != nil {
			return nil, nil, err
		}
		if ruleType == "income" {
			incomePatterns = append(incomePatterns, pattern)
		} else {
			excludedPatterns = append(excludedPatterns, pattern)
		}
	}
	return incomePatterns, excludedPatterns, rows.Err()
}

// TxnTypeOverride represents a stored type override.
type TxnTypeOverride struct {
	Fingerprint     string
	MerchantPattern string
	TargetType      string
}

// GetTxnTypeOverrides returns all type overrides.
func (d *DB) GetTxnTypeOverrides() ([]TxnTypeOverride, error) {
	rows, err := d.db.Query("SELECT COALESCE(fingerprint,''), COALESCE(merchant_pattern,''), target_type FROM txn_type_overrides")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var overrides []TxnTypeOverride
	for rows.Next() {
		var o TxnTypeOverride
		if err := rows.Scan(&o.Fingerprint, &o.MerchantPattern, &o.TargetType); err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}

// TransactionCount returns the total number of transactions.
func (d *DB) TransactionCount() (int, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&count)
	return count, err
}

// OldestTransaction returns the oldest transaction date.
func (d *DB) OldestTransaction() (time.Time, error) {
	var dateStr sql.NullString
	err := d.db.QueryRow("SELECT MIN(posted_at) FROM transactions").Scan(&dateStr)
	if err != nil || !dateStr.Valid {
		return time.Time{}, err
	}
	return time.Parse("2006-01-02", dateStr.String)
}

// NewestTransaction returns the most recent transaction date.
func (d *DB) NewestTransaction() (time.Time, error) {
	var dateStr sql.NullString
	err := d.db.QueryRow("SELECT MAX(posted_at) FROM transactions").Scan(&dateStr)
	if err != nil || !dateStr.Valid {
		return time.Time{}, err
	}
	return time.Parse("2006-01-02", dateStr.String)
}

// -----------------------------------------------------------------------------
// Commitments
// -----------------------------------------------------------------------------

// GetCommitments returns all commitments.
func (d *DB) GetCommitments() ([]models.Commitment, error) {
	rows, err := d.db.Query(`
		SELECT id, name, COALESCE(merchant_norm,''), expected_cents, cadence,
		       day_of_month, COALESCE(reference_date,''), confirmed, source, direction
		FROM commitments
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Commitment
	for rows.Next() {
		var c models.Commitment
		if err := rows.Scan(&c.ID, &c.Name, &c.MerchantNorm, &c.ExpectedCents,
			&c.Cadence, &c.DayOfMonth, &c.ReferenceDate, &c.Confirmed, &c.Source, &c.Direction); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// SaveCommitment inserts a new commitment and returns its ID.
func (d *DB) SaveCommitment(c models.Commitment) (int64, error) {
	now := utcNowISO()
	res, err := d.db.Exec(`
		INSERT INTO commitments(name, merchant_norm, expected_cents, cadence, day_of_month,
		                        reference_date, confirmed, source, direction, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.MerchantNorm, c.ExpectedCents, c.Cadence, c.DayOfMonth,
		c.ReferenceDate, c.Confirmed, c.Source, c.Direction, now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCommitment applies partial updates to a commitment by ID.
// Only non-nil fields in the map are updated.
func (d *DB) UpdateCommitment(id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	// Whitelist of allowed columns to prevent injection.
	allowed := map[string]bool{
		"confirmed": true, "source": true, "cadence": true,
		"name": true, "expected_cents": true, "direction": true,
		"day_of_month": true, "reference_date": true, "merchant_norm": true,
	}
	var setClauses []string
	var args []any
	for col, val := range fields {
		if !allowed[col] {
			continue
		}
		setClauses = append(setClauses, col+" = ?")
		args = append(args, val)
	}
	if len(setClauses) == 0 {
		return nil
	}
	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, utcNowISO())
	args = append(args, id)

	query := "UPDATE commitments SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	_, err := d.db.Exec(query, args...)
	return err
}

// DeleteCommitment removes a commitment by ID.
func (d *DB) DeleteCommitment(id int64) error {
	_, err := d.db.Exec("DELETE FROM commitments WHERE id = ?", id)
	return err
}

// RecentMerchantDates returns raw (merchant, description, max_posted_at) tuples
// for transactions posted within the last N days. The caller is responsible for
// normalizing merchant names and matching against commitment merchant_norms.
func (d *DB) RecentMerchantDates(lookbackDays int) ([]MerchantDate, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -lookbackDays).Format("2006-01-02")
	rows, err := d.db.Query(`
		SELECT merchant, description, MAX(posted_at) as last_posted
		FROM transactions
		WHERE posted_at >= ?
		GROUP BY LOWER(TRIM(COALESCE(merchant,'')))`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MerchantDate
	for rows.Next() {
		var md MerchantDate
		if err := rows.Scan(&md.Merchant, &md.Description, &md.LastPosted); err != nil {
			continue
		}
		results = append(results, md)
	}
	return results, rows.Err()
}

// MerchantDate holds a merchant's most recent posting date.
type MerchantDate struct {
	Merchant    string
	Description string
	LastPosted  string // ISO date or datetime
}

// TransactionsInWindow returns raw (merchant, description, posted_at) for all
// transactions posted within [startISO, endISO] inclusive.
func (d *DB) TransactionsInWindow(startISO, endISO string) ([]MerchantDate, error) {
	rows, err := d.db.Query(`
		SELECT merchant, description, posted_at
		FROM transactions
		WHERE posted_at >= ? AND posted_at <= ?`, startISO, endISO)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MerchantDate
	for rows.Next() {
		var md MerchantDate
		if err := rows.Scan(&md.Merchant, &md.Description, &md.LastPosted); err != nil {
			continue
		}
		results = append(results, md)
	}
	return results, rows.Err()
}

// DismissDuplicateGroup marks all commitments matching a merchant_norm as dismissed.
func (d *DB) DismissDuplicateGroup(merchantNorm string) error {
	_, err := d.db.Exec(`
		UPDATE commitments SET source = 'dismissed', updated_at = ?
		WHERE merchant_norm = ? AND source != 'dismissed'`,
		utcNowISO(), merchantNorm,
	)
	return err
}
