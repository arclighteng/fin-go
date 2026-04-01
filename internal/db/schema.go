package db

const schemaDDL = `
PRAGMA journal_mode=WAL;

CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id TEXT NOT NULL UNIQUE,
  institution TEXT NOT NULL,
  name TEXT NOT NULL,
  type TEXT,
  currency TEXT NOT NULL,
  last_seen_at TEXT
);

CREATE TABLE IF NOT EXISTS transactions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id TEXT NOT NULL,
  posted_at TEXT NOT NULL,
  amount_cents INTEGER NOT NULL,
  currency TEXT NOT NULL,
  description TEXT,
  merchant TEXT,
  source_txn_id TEXT,
  fingerprint TEXT NOT NULL,
  pending INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(account_id, source_txn_id) ON CONFLICT IGNORE
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_txn_fallback
ON transactions(account_id, posted_at, amount_cents, fingerprint)
WHERE source_txn_id IS NULL;

CREATE TABLE IF NOT EXISTS runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ran_at TEXT NOT NULL,
  lookback_days INTEGER NOT NULL,
  txns_fetched INTEGER NOT NULL,
  txns_inserted INTEGER NOT NULL,
  txns_updated INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS subscription_candidates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id TEXT NOT NULL,
  merchant_norm TEXT NOT NULL,
  amount_median_cents INTEGER NOT NULL,
  interval_days INTEGER NOT NULL,
  confidence REAL NOT NULL,
  last_seen_at TEXT NOT NULL,
  next_expected_at TEXT,
  monthly_cost_estimate_cents INTEGER NOT NULL,
  details_json TEXT
);

CREATE TABLE IF NOT EXISTS anomalies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  txn_id INTEGER NOT NULL,
  type TEXT NOT NULL,
  severity TEXT NOT NULL,
  details_json TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY(txn_id) REFERENCES transactions(id)
);

CREATE TABLE IF NOT EXISTS alert_actions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  alert_key TEXT NOT NULL UNIQUE,
  action TEXT NOT NULL,
  merchant_norm TEXT,
  pattern_type TEXT,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_alert_actions_merchant ON alert_actions(merchant_norm);
CREATE INDEX IF NOT EXISTS idx_alert_actions_key ON alert_actions(alert_key);

CREATE TABLE IF NOT EXISTS merchant_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  merchant_pattern TEXT NOT NULL,
  rule_type TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(merchant_pattern, rule_type)
);

CREATE TABLE IF NOT EXISTS recurring_type_overrides (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  merchant_norm TEXT NOT NULL UNIQUE,
  override_type TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS category_overrides (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  merchant_norm TEXT NOT NULL UNIQUE,
  category_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS txn_type_overrides (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  fingerprint TEXT,
  merchant_pattern TEXT,
  target_type TEXT NOT NULL,
  reason TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(fingerprint),
  UNIQUE(merchant_pattern)
);

CREATE INDEX IF NOT EXISTS idx_txn_type_overrides_fp ON txn_type_overrides(fingerprint);

CREATE TABLE IF NOT EXISTS budget_targets (
    category_id TEXT NOT NULL UNIQUE,
    monthly_target_cents INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS transaction_notes (
    fingerprint TEXT NOT NULL UNIQUE,
    note TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS transaction_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    fingerprint TEXT NOT NULL,
    tag TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(fingerprint, tag)
);

CREATE INDEX IF NOT EXISTS idx_txn_tags_fp ON transaction_tags(fingerprint);
CREATE INDEX IF NOT EXISTS idx_txn_tags_tag ON transaction_tags(tag);

CREATE INDEX IF NOT EXISTS idx_txn_posted_at ON transactions(posted_at);
CREATE INDEX IF NOT EXISTS idx_txn_account_posted ON transactions(account_id, posted_at);
CREATE INDEX IF NOT EXISTS idx_txn_pending ON transactions(pending) WHERE pending = 1;
CREATE INDEX IF NOT EXISTS idx_txn_amount ON transactions(amount_cents);

CREATE TABLE IF NOT EXISTS commitments (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  name           TEXT NOT NULL,
  merchant_norm  TEXT,
  expected_cents INTEGER,
  cadence        TEXT NOT NULL DEFAULT 'monthly',
  day_of_month   INTEGER,
  reference_date TEXT,
  confirmed      INTEGER NOT NULL DEFAULT 0,
  source         TEXT NOT NULL DEFAULT 'detected',
  direction      TEXT NOT NULL DEFAULT 'expense',
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);
`
