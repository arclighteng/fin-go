package db

import (
	"database/sql"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/models"
)

// SearchTransactions searches by merchant or description using LIKE.
func (d *DB) SearchTransactions(query string, limit int) ([]models.Transaction, error) {
	pattern := "%" + query + "%"
	rows, err := d.db.Query(`
		SELECT account_id, posted_at, amount_cents, currency, description, merchant,
		       source_txn_id, fingerprint, COALESCE(pending, 0)
		FROM transactions
		WHERE merchant LIKE ? OR description LIKE ?
		ORDER BY posted_at DESC
		LIMIT ?`,
		pattern, pattern, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTransactions(rows)
}

// SaveIncomeSource marks a merchant as income or not-income.
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
