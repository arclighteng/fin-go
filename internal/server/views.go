package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/db"
	"github.com/go-chi/chi/v5"
)

// -----------------------------------------------------------------------------
// View model types
// -----------------------------------------------------------------------------

// BaseData holds fields injected into every rendered page.
type BaseData struct {
	// AppVersion is the binary version shown in the nav bar.
	AppVersion string
	// ActiveNav is the nav link to highlight ("dashboard", "connect", "sync-log").
	ActiveNav string
	// IsDemo is true when demo transactions are loaded.
	IsDemo bool
	// IsEmpty is true when the database contains no transactions at all.
	IsEmpty bool
}

// AccountRow is a single row from the accounts table used in the filter UI.
type AccountRow struct {
	AccountID   string
	Name        string
	Institution string
	Type        string
}

// PeriodSummary mirrors the Python PeriodViewModel fields used by the
// dashboard template.
type PeriodSummary struct {
	// PeriodLabel is the human-readable label, e.g. "March 2025".
	PeriodLabel string
	// StartDate is ISO 8601, e.g. "2025-03-01".
	StartDate string
	// EndDate is ISO 8601, exclusive.
	EndDate string

	IncomeCents        int64
	RecurringCents     int64
	DiscretionaryCents int64
	TransferCents      int64
	NetCents           int64
}

// CategoryItem holds a single row in the spending breakdown card.
type CategoryItem struct {
	CategoryID   string
	CategoryName string
	CategoryIcon string
	NetCents     int64
	AvgCents     int64
	OutlierPct   int64
	Count        int
}

// AttentionItem is a single "Heads Up" card entry.
type AttentionItem struct {
	Type           string
	Title          string
	Detail         string
	Severity       string
	ActionType     string
	Key            string
	Actioned       bool
	DrilldownScope string
	Link           string
	LinkLabel      string
}

// DashboardData is the complete view model for the dashboard template.
type DashboardData struct {
	BaseData

	PeriodType    string
	CurrentPeriod *PeriodSummary
	Periods       []PeriodSummary
	ClosedPeriod  bool

	AllAccounts      []AccountRow
	SelectedAccounts []string

	CategoryBreakdown []CategoryItem
	AttentionItems    []AttentionItem

	SavingsRatePct    float64
	AvgSavingsRatePct float64
	SavingsTier       string
	SpendFooterDiff   int64

	PendingCount int

	ShowNoData bool
}

// IsAccountSelected returns true when accountID is in the selected set, or
// when no explicit selection is active (all accounts shown).
func (d *DashboardData) IsAccountSelected(accountID string) bool {
	if len(d.SelectedAccounts) == 0 {
		return true
	}
	for _, id := range d.SelectedAccounts {
		if id == accountID {
			return true
		}
	}
	return false
}

// ConnectData is the view model for the connect/onboarding page.
// All interaction on that page is client-side JS.
type ConnectData struct {
	BaseData
}

// SyncStats holds aggregate transaction statistics for the sync log page.
type SyncStats struct {
	TotalTxns int64
	Earliest  string
	Latest    string
}

// SyncRun is a single row from the runs table.
type SyncRun struct {
	ID           int64
	RanAt        string
	TxnsFetched  int
	TxnsInserted int
	TxnsUpdated  int
}

// RecentTxn is a transaction row shown in the recently-added/updated tables.
type RecentTxn struct {
	PostedAt    string
	AmountCents int64
	Payee       string
	AccountName string
	CreatedAt   string
	UpdatedAt   string
}

// SyncLogData is the complete view model for the sync-log template.
type SyncLogData struct {
	BaseData
	Stats         SyncStats
	Runs          []SyncRun
	RecentInserts []RecentTxn
	RecentUpdates []RecentTxn
}

// -----------------------------------------------------------------------------
// Route registration
// -----------------------------------------------------------------------------

// RegisterViewRoutes mounts server-rendered page routes on r.
// templateDir is the path to the ui/templates directory;
// staticDir is the path to the ui/static directory (served at /static/).
// version is the build version string shown in the nav bar.
func RegisterViewRoutes(r chi.Router, database *db.DB, templateDir, staticDir, version string) error {
	tmpl, err := NewTemplateEngine(templateDir, version)
	if err != nil {
		return fmt.Errorf("init template engine: %w", err)
	}

	// Serve static assets.
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Page routes.
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
	})
	r.Get("/dashboard", makeViewHandler(database, tmpl, handleDashboardView))
	r.Get("/connect", makeViewHandler(database, tmpl, handleConnectView))
	r.Get("/sync-log", makeViewHandler(database, tmpl, handleSyncLogView))

	// Extra page routes: budget, commitments, insights, review.
	RegisterExtraViewRoutes(r, database, tmpl)

	return nil
}

// pageHandler is the function signature for page-specific handlers.
type pageHandler func(w http.ResponseWriter, r *http.Request, database *db.DB, tmpl *TemplateEngine)

// makeViewHandler wraps a page handler, injecting the db and template engine.
func makeViewHandler(database *db.DB, tmpl *TemplateEngine, fn pageHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fn(w, r, database, tmpl)
	}
}

// newBaseData builds the BaseData struct shared by all pages.
// version is the build version string shown in the nav bar.
func newBaseData(database *db.DB, activeNav, version string) BaseData {
	bd := BaseData{
		AppVersion: version,
		ActiveNav:  activeNav,
	}

	var txnCount, demoCount int64
	if err := database.QueryRow("SELECT COUNT(*) FROM transactions").Scan(&txnCount); err != nil {
		log.Printf("newBaseData: scan txn count: %v", err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM transactions WHERE account_id LIKE 'demo-%'").Scan(&demoCount); err != nil {
		log.Printf("newBaseData: scan demo count: %v", err)
	}
	bd.IsDemo = demoCount > 0
	bd.IsEmpty = txnCount == 0

	return bd
}

// -----------------------------------------------------------------------------
// Dashboard handler
// -----------------------------------------------------------------------------

func handleDashboardView(w http.ResponseWriter, r *http.Request, database *db.DB, tmpl *TemplateEngine) {
	q := r.URL.Query()
	periodType := q.Get("period")
	if periodType == "" {
		periodType = "this_month"
	}

	// Parse account filter.
	accountsParam := q.Get("accounts")
	var selectedAccounts []string
	showNoData := strings.ToLower(accountsParam) == "none"
	if !showNoData && accountsParam != "" {
		for _, a := range strings.Split(accountsParam, ",") {
			if s := strings.TrimSpace(a); s != "" {
				selectedAccounts = append(selectedAccounts, s)
			}
		}
	}

	data := DashboardData{
		BaseData:         newBaseData(database, "dashboard", tmpl.version),
		PeriodType:       periodType,
		AllAccounts:      queryAllAccounts(database),
		SelectedAccounts: selectedAccounts,
		ShowNoData:       showNoData,
	}

	if !showNoData {
		populateDashboard(database, r, &data, periodType)
	}

	if err := tmpl.Render(w, "base", data); err != nil {
		log.Printf("dashboard render: %v", err)
	}
}

// populateDashboard fills period-dependent fields of DashboardData.
func populateDashboard(database *db.DB, r *http.Request, data *DashboardData, periodType string) {
	q := r.URL.Query()
	startStr := q.Get("start_date")
	endStr := q.Get("end_date")

	var startISO, endISO string
	switch {
	case startStr != "" && endStr != "":
		startISO = startStr
		endISO = endStr
	case periodType == "last_month":
		startISO, endISO = lastMonthRange()
	default:
		startISO, endISO = thisMonthRange()
	}

	if startISO == "" {
		return
	}

	// Current period totals.
	cp := queryPeriodTotals(database, startISO, endISO, data.SelectedAccounts)
	if cp != nil {
		data.CurrentPeriod = cp
		if cp.IncomeCents > 0 {
			data.SavingsRatePct = float64(cp.NetCents) / float64(cp.IncomeCents) * 100
		}
		data.SavingsTier = computeSavingsTier(data.SavingsRatePct)
	}

	// Historical periods (last 6 months, most recent first).
	data.Periods = queryHistoricalPeriods(database, data.SelectedAccounts, 6)

	// 3-month average savings rate from periods 1..3 (skip current at index 0).
	if len(data.Periods) >= 4 {
		var rates []float64
		for _, p := range data.Periods[1:4] {
			if p.IncomeCents > 0 {
				rates = append(rates, float64(p.NetCents)/float64(p.IncomeCents)*100)
			}
		}
		if len(rates) > 0 {
			var sum float64
			for _, rt := range rates {
				sum += rt
			}
			data.AvgSavingsRatePct = sum / float64(len(rates))
		}
	}

	// Pending transactions in current period.
	if startISO != "" {
		data.PendingCount = queryPendingCount(database, startISO, endISO, data.SelectedAccounts)
	}

	// Category spending breakdown.
	data.CategoryBreakdown = queryCategoryBreakdown(database, startISO, endISO, data.SelectedAccounts)

	// Spend footer diff vs 3-month average.
	if cp != nil && len(data.Periods) >= 4 {
		total := cp.RecurringCents + cp.DiscretionaryCents
		var avgTotal int64
		for _, p := range data.Periods[1:4] {
			avgTotal += p.RecurringCents + p.DiscretionaryCents
		}
		avgTotal /= 3
		if avgTotal > 0 {
			data.SpendFooterDiff = total - avgTotal
		}
	}
}

// queryAllAccounts returns all accounts, ordered by institution then name.
func queryAllAccounts(database *db.DB) []AccountRow {
	rows, err := database.Query(`
		SELECT account_id, COALESCE(name,''), COALESCE(institution,''), COALESCE(type,'')
		FROM accounts
		ORDER BY institution, name
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var accounts []AccountRow
	for rows.Next() {
		var a AccountRow
		if err := rows.Scan(&a.AccountID, &a.Name, &a.Institution, &a.Type); err != nil {
			continue
		}
		accounts = append(accounts, a)
	}
	return accounts
}

// queryPeriodTotals aggregates transaction totals for a date range.
// Positive amount_cents = income, negative = expense (standard SimpleFIN convention).
func queryPeriodTotals(database *db.DB, startISO, endISO string, accountFilter []string) *PeriodSummary {
	q := `
		SELECT
			COALESCE(SUM(CASE WHEN amount_cents > 0 THEN amount_cents ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN amount_cents < 0 THEN ABS(amount_cents) ELSE 0 END), 0)
		FROM transactions
		WHERE posted_at >= ? AND posted_at < ?
	`
	args := []any{startISO, endISO}
	q, args = appendAccountFilter(q, args, accountFilter)

	var income, expenses int64
	if err := database.QueryRow(q, args...).Scan(&income, &expenses); err != nil {
		return nil
	}

	return &PeriodSummary{
		PeriodLabel:        monthPeriodLabel(startISO),
		StartDate:          startISO,
		EndDate:            endISO,
		IncomeCents:        income,
		DiscretionaryCents: expenses,
		NetCents:           income - expenses,
	}
}

// queryHistoricalPeriods returns the n most recent calendar months as PeriodSummary,
// most recent first.
func queryHistoricalPeriods(database *db.DB, accountFilter []string, n int) []PeriodSummary {
	months := nPriorMonths(n)
	out := make([]PeriodSummary, 0, n)
	for _, m := range months {
		ps := queryPeriodTotals(database, m[0], m[1], accountFilter)
		if ps == nil {
			ps = &PeriodSummary{
				PeriodLabel: monthPeriodLabel(m[0]),
				StartDate:   m[0],
				EndDate:     m[1],
			}
		}
		out = append(out, *ps)
	}
	return out
}

// queryPendingCount returns the count of pending transactions in the given range.
func queryPendingCount(database *db.DB, startISO, endISO string, accountFilter []string) int {
	q := `SELECT COUNT(*) FROM transactions WHERE posted_at >= ? AND posted_at < ? AND pending = 1`
	args := []any{startISO, endISO}
	q, args = appendAccountFilter(q, args, accountFilter)

	var count int
	if err := database.QueryRow(q, args...).Scan(&count); err != nil {
		log.Printf("queryPendingCount: scan: %v", err)
	}
	return count
}

// queryCategoryBreakdown returns spending totals grouped by category_id.
// Rows without a category override are bucketed as "other".
func queryCategoryBreakdown(database *db.DB, startISO, endISO string, accountFilter []string) []CategoryItem {
	q := `
		SELECT
			COALESCE(c.category_id, 'other') AS cat_id,
			COALESCE(SUM(ABS(t.amount_cents)), 0) AS total
		FROM transactions t
		LEFT JOIN category_overrides c
			ON TRIM(LOWER(COALESCE(NULLIF(t.merchant,''), NULLIF(t.description,''), ''))) = c.merchant_norm
		WHERE t.posted_at >= ? AND t.posted_at < ?
		  AND t.amount_cents < 0
	`
	args := []any{startISO, endISO}
	q, args = appendAccountFilter(q, args, accountFilter)
	q += " GROUP BY cat_id ORDER BY total DESC LIMIT 20"

	rows, err := database.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var items []CategoryItem
	for rows.Next() {
		var catID string
		var total int64
		if err := rows.Scan(&catID, &total); err != nil {
			continue
		}
		items = append(items, CategoryItem{
			CategoryID:   catID,
			CategoryName: titleCase(catID),
			NetCents:     total,
		})
	}
	return items
}

// appendAccountFilter adds an account_id IN (?) clause when a filter is provided.
// It returns the updated query string and args slice.
func appendAccountFilter(q string, args []any, accountFilter []string) (string, []any) {
	if len(accountFilter) == 0 {
		return q, args
	}
	placeholders := strings.Repeat("?,", len(accountFilter))
	placeholders = placeholders[:len(placeholders)-1]
	q += " AND account_id IN (" + placeholders + ")"
	for _, a := range accountFilter {
		args = append(args, a)
	}
	return q, args
}

// -----------------------------------------------------------------------------
// Connect handler
// -----------------------------------------------------------------------------

func handleConnectView(w http.ResponseWriter, r *http.Request, database *db.DB, tmpl *TemplateEngine) {
	data := ConnectData{
		BaseData: newBaseData(database, "connect", tmpl.version),
	}
	if err := tmpl.Render(w, "base", data); err != nil {
		log.Printf("connect render: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Sync-log handler
// -----------------------------------------------------------------------------

func handleSyncLogView(w http.ResponseWriter, r *http.Request, database *db.DB, tmpl *TemplateEngine) {
	data := SyncLogData{
		BaseData: newBaseData(database, "sync-log", tmpl.version),
	}

	// Aggregate transaction stats.
	if err := database.QueryRow(`
		SELECT COUNT(*), COALESCE(MIN(posted_at),''), COALESCE(MAX(posted_at),'')
		FROM transactions
	`).Scan(&data.Stats.TotalTxns, &data.Stats.Earliest, &data.Stats.Latest); err != nil {
		log.Printf("handleSyncLogView: scan stats: %v", err)
	}

	// Sync run history.
	runRows, err := database.Query(`
		SELECT id, ran_at, txns_fetched, txns_inserted, txns_updated
		FROM runs
		ORDER BY ran_at DESC
		LIMIT 50
	`)
	if err == nil {
		defer runRows.Close()
		for runRows.Next() {
			var run SyncRun
			if err := runRows.Scan(&run.ID, &run.RanAt, &run.TxnsFetched, &run.TxnsInserted, &run.TxnsUpdated); err != nil {
				continue
			}
			data.Runs = append(data.Runs, run)
		}
	}

	// Recently added and recently updated transactions.
	data.RecentInserts = queryRecentInserts(database)
	data.RecentUpdates = queryRecentUpdates(database)

	if err := tmpl.Render(w, "base", data); err != nil {
		log.Printf("sync-log render: %v", err)
	}
}

// queryRecentInserts returns the 15 most recently created transactions.
func queryRecentInserts(database *db.DB) []RecentTxn {
	rows, err := database.Query(`
		SELECT
			COALESCE(t.posted_at,''),
			t.amount_cents,
			COALESCE(t.merchant, t.description, '') AS payee,
			COALESCE(a.name, t.account_id) AS account_name,
			COALESCE(t.created_at,''),
			COALESCE(t.updated_at,'')
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.account_id
		ORDER BY t.created_at DESC
		LIMIT 15
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanRecentTxns(rows)
}

// queryRecentUpdates returns the 15 most recently updated transactions
// (where updated_at > created_at).
func queryRecentUpdates(database *db.DB) []RecentTxn {
	rows, err := database.Query(`
		SELECT
			COALESCE(t.posted_at,''),
			t.amount_cents,
			COALESCE(t.merchant, t.description, '') AS payee,
			COALESCE(a.name, t.account_id) AS account_name,
			COALESCE(t.created_at,''),
			COALESCE(t.updated_at,'')
		FROM transactions t
		LEFT JOIN accounts a ON t.account_id = a.account_id
		WHERE t.updated_at > t.created_at
		ORDER BY t.updated_at DESC
		LIMIT 15
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanRecentTxns(rows)
}

// scanRecentTxns reads RecentTxn rows from an open sql.Rows cursor.
func scanRecentTxns(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) []RecentTxn {
	var out []RecentTxn
	for rows.Next() {
		var txn RecentTxn
		if err := rows.Scan(
			&txn.PostedAt,
			&txn.AmountCents,
			&txn.Payee,
			&txn.AccountName,
			&txn.CreatedAt,
			&txn.UpdatedAt,
		); err != nil {
			log.Printf("scanRecentTxns: scan: %v", err)
			continue
		}
		out = append(out, txn)
	}
	return out
}

// -----------------------------------------------------------------------------
// Date / period helpers
// -----------------------------------------------------------------------------

// thisMonthRange returns the ISO 8601 [start, endExclusive) strings for the
// current calendar month: start = first of month, end = first of next month.
func thisMonthRange() (start, end string) {
	now := time.Now().UTC()
	y, m, _ := now.Date()
	start = fmt.Sprintf("%04d-%02d-01", y, m)
	next := time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
	end = fmt.Sprintf("%04d-%02d-%02d", next.Year(), next.Month(), next.Day())
	return
}

// lastMonthRange returns the ISO 8601 [start, endExclusive) for the previous
// complete calendar month.
func lastMonthRange() (start, end string) {
	now := time.Now().UTC()
	y, m, _ := now.Date()
	firstOfMonth := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	firstOfLastMonth := firstOfMonth.AddDate(0, -1, 0)
	ly, lm, _ := firstOfLastMonth.Date()
	start = fmt.Sprintf("%04d-%02d-01", ly, lm)
	end = fmt.Sprintf("%04d-%02d-01", y, m)
	return
}

// nPriorMonths returns n [start, end) pairs for the n most recent calendar
// months, starting with the current month, going backwards.
func nPriorMonths(n int) [][2]string {
	now := time.Now().UTC()
	y, m, _ := now.Date()
	anchor := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)

	out := make([][2]string, 0, n)
	for i := 0; i < n; i++ {
		next := anchor.AddDate(0, 1, 0)
		start := fmt.Sprintf("%04d-%02d-%02d", anchor.Year(), anchor.Month(), anchor.Day())
		end := fmt.Sprintf("%04d-%02d-%02d", next.Year(), next.Month(), next.Day())
		out = append(out, [2]string{start, end})
		anchor = anchor.AddDate(0, -1, 0)
	}
	return out
}

// monthPeriodLabel converts an ISO date prefix "2025-03-01" to "March 2025".
func monthPeriodLabel(isoDate string) string {
	if len(isoDate) < 7 {
		return isoDate
	}
	months := [...]string{"", "January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December"}
	var y, mo int
	fmt.Sscanf(isoDate[:7], "%d-%d", &y, &mo)
	if mo < 1 || mo > 12 {
		return isoDate
	}
	return fmt.Sprintf("%s %d", months[mo], y)
}

// computeSavingsTier maps a savings rate percentage to a tier identifier.
// Tiers match the Python implementation.
func computeSavingsTier(pct float64) string {
	switch {
	case pct >= 30:
		return "wealth-building"
	case pct >= 20:
		return "progress"
	case pct >= 0:
		return "survival"
	default:
		return "negative"
	}
}

// titleCase converts "snake_case" or "lower" identifiers to "Title Case".
func titleCase(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	parts := strings.Fields(s)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// currentYMD is a thin shim so templates.go can call a date helper without
// importing time itself. It returns [year, month, day] for today.
func currentYMD() [3]int {
	now := time.Now()
	y, m, d := now.Date()
	return [3]int{y, int(m), d}
}

// -----------------------------------------------------------------------------
// Default path helpers (for use from main/cli)
// -----------------------------------------------------------------------------

// DefaultTemplateDir returns the path to ui/templates resolved relative to the
// executable's directory. Falls back to the working directory if the executable
// path cannot be determined.
func DefaultTemplateDir() string {
	return defaultAssetDir("ui", "templates")
}

// DefaultStaticDir returns the path to ui/static resolved relative to the
// executable's directory.
func DefaultStaticDir() string {
	return defaultAssetDir("ui", "static")
}

// defaultAssetDir resolves an asset path relative to the executable's directory.
// It tries: (1) exe dir/../ui/X, (2) exe dir/ui/X, (3) ./ui/X.
func defaultAssetDir(parts ...string) string {
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		// The exe may be in go/ (same level as ui/) or in go/e2e/ (one level deeper).
		candidates := []string{
			filepath.Clean(filepath.Join(exeDir, "..", filepath.Join(parts...))),
			filepath.Clean(filepath.Join(exeDir, filepath.Join(parts...))),
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				return c
			}
		}
	}
	return filepath.Join(parts...)
}
