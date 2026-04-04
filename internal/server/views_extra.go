package server

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/arclighteng/fin-go/internal/categorize"
	"github.com/arclighteng/fin-go/internal/db"
	"github.com/go-chi/chi/v5"
)

// -----------------------------------------------------------------------------
// Budget view model
// -----------------------------------------------------------------------------

// BudgetRow holds spending vs target for one category.
type BudgetRow struct {
	CategoryID   string
	CategoryName string
	CategoryIcon string
	TargetCents  int64
	SpentCents   int64
	// Percent is SpentCents / TargetCents * 100 (0 when TargetCents == 0).
	Percent float64
}

// BudgetCategory is a category entry for the "Add target" dropdown.
type BudgetCategory struct {
	ID   string
	Name string
	Icon string
}

// BudgetData is the complete view model for budget.html.
type BudgetData struct {
	BaseData

	BudgetRows      []BudgetRow
	TotalBudgetCents int64
	TotalSpentCents  int64
	TotalPercent     float64
	PeriodStart      string
	PeriodEnd        string
	AllCategories    []BudgetCategory
	HasUnbudgeted    bool
}

// -----------------------------------------------------------------------------
// Commitments view model
// -----------------------------------------------------------------------------

// CommitmentRow is a single commitment entry shown on the commitments page.
type CommitmentRow struct {
	ID              int64
	Name            string
	Cadence         string
	ExpectedCents   int64 // 0 means unknown
	MonthlyEquivCents int64
}

// DuplicateItem is one member of a detected duplicate group.
type DuplicateItem struct {
	Name        string
	MonthlyCents int64
	Cadence     string
}

// DuplicateGroup represents a group of suspected duplicate commitments.
type DuplicateGroup struct {
	Detail          string
	Severity        string // "high" or "medium"
	TotalMonthlyCents int64
	Items           []DuplicateItem
}

// NotYetPostedItem is a commitment that hasn't appeared in transactions yet.
type NotYetPostedItem struct {
	Merchant        string
	Cadence         string
	MedianAmountCents int64
	LastSeen        string
	ExpectedDate    string
}

// CommitmentsData is the complete view model for commitments.html.
type CommitmentsData struct {
	BaseData

	IncomeConfirmed    []CommitmentRow
	IncomeSuggestions  []CommitmentRow
	IncomeDismissed    []CommitmentRow
	ExpenseConfirmed   []CommitmentRow
	ExpenseSuggestions []CommitmentRow
	ExpenseDismissed   []CommitmentRow

	IncomeMonthlyTotal  int64
	ExpenseMonthlyTotal int64
	NetMonthly          int64

	IncomeSuggestionCount  int
	ExpenseSuggestionCount int

	Duplicates    []DuplicateGroup
	NotYetPosted  []NotYetPostedItem
}

// -----------------------------------------------------------------------------
// Insights view model
// -----------------------------------------------------------------------------

// SavingsEntry is one month's data point for the insights sparkline and table.
type SavingsEntry struct {
	Label          string
	SavingsRatePct float64
	NetCents       int64
	IncomeCents    int64
	// SparkY is the pre-computed Y coordinate for the SVG sparkline (0..80).
	SparkY float64
	// CX is the pre-computed X coordinate for this entry's SVG circle.
	CX int64
}

// InsightsData is the complete view model for insights.html.
type InsightsData struct {
	BaseData

	SavingsHistory         []SavingsEntry
	SavingsHistoryReversed []SavingsEntry
	AvgSavingsRatePct      float64
	IncomeStability        string // "stable", "moderate", "variable"
	IncomeStabilityLabel   string // Capitalized display label
	SavingsStreak          int
	MonthsWithData         int

	// Pre-computed SVG values so the template stays logic-free.
	SparklinePoints  string
	SparklineMaxRate float64
	SparklineMinRate float64
	SparklineRange   float64
	SparklineStep    int64 // x-step between data points in SVG units
	SparklineTargetY float64

	// X-axis label text and mid-label x coordinate (px).
	SparklineFirst string
	SparklineMid   string
	SparklineMidX  int64
	SparklineLast  string
}

// -----------------------------------------------------------------------------
// Review view model
// -----------------------------------------------------------------------------

// GhostTxn is an uncategorized expense shown in the review queue.
type GhostTxn struct {
	Date           string
	MerchantNorm   string
	AmountCents    int64
	RawDescription string
}

// LowConfTxn is a low-confidence transaction shown in the review queue.
type LowConfTxn struct {
	Date         string
	MerchantNorm string
	AmountCents  int64
	CategoryName string
	Confidence   float64 // 0..100 — already multiplied for template display
}

// MerchantSummary is one row in the top-merchants table.
type MerchantSummary struct {
	Merchant   string
	Count      int
	TotalCents int64
	LastSeen   string
	Status     string // "tracked", "recurring", "untracked"
}

// ReviewCoverage holds coverage statistics displayed at the bottom of the page.
type ReviewCoverage struct {
	TotalTxns       int
	CatPct          int
	TrackedMerchants int
	LowConf         int
}

// ReviewData is the complete view model for review.html.
type ReviewData struct {
	BaseData

	AllAccounts      []AccountRow
	SelectedAccounts []string
	ShowNoData       bool

	Ghosts           []GhostTxn
	LowConfidence    []LowConfTxn
	TopMerchants     []MerchantSummary
	Coverage         ReviewCoverage
	AllCategories    []categorize.Category
	NeedsReviewCount int
}

// IsAccountSelected returns true when accountID is selected or when no filter
// is active.
func (d ReviewData) IsAccountSelected(accountID string) bool {
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

// -----------------------------------------------------------------------------
// Route registration
// -----------------------------------------------------------------------------

// RegisterExtraViewRoutes mounts the budget, commitments, insights, and review
// page handlers onto r. It re-uses the same TemplateEngine that RegisterViewRoutes
// creates; callers should pass the engine from that call.
//
// This function is intentionally standalone so it can be called from server.go
// without modifying views.go.
func RegisterExtraViewRoutes(r chi.Router, database *db.DB, tmpl *TemplateEngine) {
	r.Get("/budget", makeViewHandler(database, tmpl, handleBudgetView))
	r.Get("/commitments", makeViewHandler(database, tmpl, handleCommitmentsView))
	r.Get("/subs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/commitments", http.StatusMovedPermanently)
	})
	r.Get("/insights", makeViewHandler(database, tmpl, handleInsightsView))
	r.Get("/review", makeViewHandler(database, tmpl, handleReviewView))
}

// -----------------------------------------------------------------------------
// Budget handler
// -----------------------------------------------------------------------------

func handleBudgetView(w http.ResponseWriter, r *http.Request, database *db.DB, tmpl *TemplateEngine) {
	now := time.Now().UTC()
	y, m, _ := now.Date()
	startISO := fmt.Sprintf("%04d-%02d-01", y, m)
	next := time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
	endISO := fmt.Sprintf("%04d-%02d-%02d", next.Year(), next.Month(), next.Day())

	// Fetch budget targets from DB.
	targets, _ := database.GetBudgetTargets()
	targetMap := make(map[string]int64, len(targets))
	for _, t := range targets {
		targetMap[t.CategoryID] = t.MonthlyTargetCents
	}

	// Fetch actual spending by category for the current month.
	actualMap := queryCategoryActuals(database, startISO, endISO)

	// Build sorted category list for the dropdown (exclude system categories).
	skipCats := map[string]bool{"income": true, "transfer": true, "one_time_deposit": true}
	allCats := make([]BudgetCategory, 0, len(categorize.Categories))
	for _, cat := range categorize.Categories {
		if !skipCats[cat.ID] {
			allCats = append(allCats, BudgetCategory{ID: cat.ID, Name: cat.Name, Icon: cat.Icon})
		}
	}
	sort.Slice(allCats, func(i, j int) bool { return allCats[i].Name < allCats[j].Name })

	// Build budget rows: include categories that have a target OR actual spend.
	var rows []BudgetRow
	var totalBudget, totalSpent int64
	seen := make(map[string]bool)

	for catID, target := range targetMap {
		spent := actualMap[catID]
		pct := 0.0
		if target > 0 {
			pct = float64(spent) / float64(target) * 100.0
		}
		cat := categorize.Categories[catID]
		rows = append(rows, BudgetRow{
			CategoryID:   catID,
			CategoryName: cat.Name,
			CategoryIcon: cat.Icon,
			TargetCents:  target,
			SpentCents:   spent,
			Percent:      pct,
		})
		totalBudget += target
		totalSpent += spent
		seen[catID] = true
	}
	for catID, spent := range actualMap {
		if seen[catID] {
			continue
		}
		cat := categorize.Categories[catID]
		rows = append(rows, BudgetRow{
			CategoryID:   catID,
			CategoryName: cat.Name,
			CategoryIcon: cat.Icon,
			TargetCents:  0,
			SpentCents:   spent,
			Percent:      0,
		})
		totalSpent += spent
	}

	// Sort: budgeted rows first (by % used desc), then unbudgeted by spend desc.
	sort.Slice(rows, func(i, j int) bool {
		hasTgtI := rows[i].TargetCents > 0
		hasTgtJ := rows[j].TargetCents > 0
		if hasTgtI != hasTgtJ {
			return hasTgtI // budgeted before unbudgeted
		}
		if hasTgtI {
			return rows[i].Percent > rows[j].Percent
		}
		return rows[i].SpentCents > rows[j].SpentCents
	})

	totalPct := 0.0
	if totalBudget > 0 {
		totalPct = float64(totalSpent) / float64(totalBudget) * 100.0
	}

	hasUnbudgeted := false
	for _, row := range rows {
		if row.TargetCents == 0 && row.SpentCents > 0 {
			hasUnbudgeted = true
			break
		}
	}

	data := BudgetData{
		BaseData:         newBaseData(database, "budget", tmpl.version),
		BudgetRows:       rows,
		TotalBudgetCents: totalBudget,
		TotalSpentCents:  totalSpent,
		TotalPercent:     totalPct,
		PeriodStart:      startISO,
		PeriodEnd:        endISO,
		AllCategories:    allCats,
		HasUnbudgeted:    hasUnbudgeted,
	}

	if err := tmpl.Render(w, "base", data); err != nil {
		log.Printf("budget render: %v", err)
	}
}

// queryCategoryActuals returns category_id -> total_spent_cents for the period.
// Only expense transactions (amount_cents < 0) are included.
func queryCategoryActuals(database *db.DB, startISO, endISO string) map[string]int64 {
	q := `
		SELECT
			COALESCE(c.category_id, 'other') AS cat_id,
			COALESCE(SUM(ABS(t.amount_cents)), 0)
		FROM transactions t
		LEFT JOIN category_overrides c
			ON TRIM(LOWER(COALESCE(NULLIF(t.merchant,''), NULLIF(t.description,''), ''))) = c.merchant_norm
		WHERE t.posted_at >= ? AND t.posted_at < ?
		  AND t.amount_cents < 0
		  AND COALESCE(t.pending, 0) = 0
		GROUP BY cat_id
	`
	rows, err := database.Query(q, startISO, endISO)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var catID string
		var total int64
		if err := rows.Scan(&catID, &total); err != nil {
			continue
		}
		result[catID] = total
	}
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	return result
}

// -----------------------------------------------------------------------------
// Commitments handler
// -----------------------------------------------------------------------------

// cadenceMultiplier returns the monthly equivalent multiplier for a cadence string.
func cadenceMultiplier(cadence string) float64 {
	switch cadence {
	case "monthly":
		return 1.0
	case "weekly":
		return 52.0 / 12.0
	case "biweekly":
		return 26.0 / 12.0
	case "annual":
		return 1.0 / 12.0
	case "quarterly":
		return 1.0 / 3.0
	default:
		return 1.0
	}
}

func handleCommitmentsView(w http.ResponseWriter, r *http.Request, database *db.DB, tmpl *TemplateEngine) {
	data := CommitmentsData{
		BaseData: newBaseData(database, "commitments", tmpl.version),
	}

	rows, err := database.Query(`
		SELECT id, name, COALESCE(merchant_norm,''), COALESCE(expected_cents,0), cadence, confirmed, source, direction
		FROM commitments
		ORDER BY name
	`)
	if err != nil {
		log.Printf("commitments query: %v", err)
		if renderErr := tmpl.Render(w, "base", data); renderErr != nil {
			log.Printf("commitments render: %v", renderErr)
		}
		return
	}
	defer rows.Close()

	type rawRow struct {
		id           int64
		name         string
		merchantNorm string
		expected     int64
		cadence      string
		confirmed    bool
		source       string
		direction    string
	}

	var allRows []rawRow
	for rows.Next() {
		var row rawRow
		var confirmed int
		if err := rows.Scan(&row.id, &row.name, &row.merchantNorm, &row.expected, &row.cadence, &confirmed, &row.source, &row.direction); err != nil {
			continue
		}
		row.confirmed = confirmed != 0
		allRows = append(allRows, row)
	}
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	toCommitmentRow := func(rr rawRow) CommitmentRow {
		mult := cadenceMultiplier(rr.cadence)
		monthlyEquiv := int64(math.Round(float64(rr.expected) * mult))
		return CommitmentRow{
			ID:                rr.id,
			Name:              rr.name,
			Cadence:           rr.cadence,
			ExpectedCents:     rr.expected,
			MonthlyEquivCents: monthlyEquiv,
		}
	}

	var incomeTotal, expenseTotal int64

	for _, rr := range allRows {
		cr := toCommitmentRow(rr)
		mult := cadenceMultiplier(rr.cadence)
		monthlyAmt := int64(math.Round(float64(rr.expected) * mult))

		isDismissed := rr.source == "dismissed"

		switch {
		case rr.direction == "income" && rr.confirmed && !isDismissed:
			data.IncomeConfirmed = append(data.IncomeConfirmed, cr)
			incomeTotal += monthlyAmt
		case rr.direction == "income" && !rr.confirmed && !isDismissed:
			data.IncomeSuggestions = append(data.IncomeSuggestions, cr)
		case rr.direction == "income" && isDismissed:
			data.IncomeDismissed = append(data.IncomeDismissed, cr)
		case rr.direction != "income" && rr.confirmed && !isDismissed:
			data.ExpenseConfirmed = append(data.ExpenseConfirmed, cr)
			expenseTotal += monthlyAmt
		case rr.direction != "income" && !rr.confirmed && !isDismissed:
			data.ExpenseSuggestions = append(data.ExpenseSuggestions, cr)
		case rr.direction != "income" && isDismissed:
			data.ExpenseDismissed = append(data.ExpenseDismissed, cr)
		}
	}

	// Detect duplicate commitments: group non-dismissed entries by merchant_norm
	// and flag groups with 2+ members.
	type dupEntry struct {
		name         string
		cadence      string
		monthlyCents int64
	}
	dupGroups := map[string][]dupEntry{}
	for _, rr := range allRows {
		if rr.source == "dismissed" || rr.merchantNorm == "" {
			continue
		}
		mult := cadenceMultiplier(rr.cadence)
		monthly := int64(math.Round(float64(rr.expected) * mult))
		dupGroups[rr.merchantNorm] = append(dupGroups[rr.merchantNorm], dupEntry{
			name:         rr.name,
			cadence:      rr.cadence,
			monthlyCents: monthly,
		})
	}
	for merchant, entries := range dupGroups {
		if len(entries) < 2 {
			continue
		}
		var totalMonthly int64
		items := make([]DuplicateItem, 0, len(entries))
		for _, e := range entries {
			totalMonthly += e.monthlyCents
			items = append(items, DuplicateItem{
				Name:         e.name,
				MonthlyCents: e.monthlyCents,
				Cadence:      e.cadence,
			})
		}
		severity := "medium"
		if totalMonthly > 5000 { // > $50/mo combined
			severity = "high"
		}
		data.Duplicates = append(data.Duplicates, DuplicateGroup{
			Detail:            merchant + " appears " + fmt.Sprintf("%d", len(entries)) + " times",
			Severity:          severity,
			TotalMonthlyCents: totalMonthly,
			Items:             items,
		})
	}

	data.IncomeMonthlyTotal = incomeTotal
	data.ExpenseMonthlyTotal = expenseTotal
	data.NetMonthly = incomeTotal - expenseTotal
	data.IncomeSuggestionCount = len(data.IncomeSuggestions)
	data.ExpenseSuggestionCount = len(data.ExpenseSuggestions)

	if err := tmpl.Render(w, "base", data); err != nil {
		log.Printf("commitments render: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Insights handler
// -----------------------------------------------------------------------------

func handleInsightsView(w http.ResponseWriter, r *http.Request, database *db.DB, tmpl *TemplateEngine) {
	// Fetch last 12 months of period totals.
	months := nPriorMonths(12)

	type periodData struct {
		label       string
		incomeCents int64
		netCents    int64
	}

	// Build chronological list (oldest first) of months with income data.
	rawPeriods := make([]periodData, 0, 12)
	for i := len(months) - 1; i >= 0; i-- {
		m := months[i]
		ps := queryPeriodTotals(database, m[0], m[1], nil)
		if ps == nil || ps.IncomeCents <= 0 {
			continue
		}
		rawPeriods = append(rawPeriods, periodData{
			label:       monthPeriodLabel(m[0]),
			incomeCents: ps.IncomeCents,
			netCents:    ps.NetCents,
		})
	}

	monthsWithData := len(rawPeriods)

	// Build SavingsEntry slice.
	entries := make([]SavingsEntry, 0, monthsWithData)
	for _, p := range rawPeriods {
		rate := 0.0
		if p.incomeCents > 0 {
			rate = math.Round(float64(p.netCents)/float64(p.incomeCents)*100*10) / 10
		}
		entries = append(entries, SavingsEntry{
			Label:          p.label,
			SavingsRatePct: rate,
			NetCents:       p.netCents,
			IncomeCents:    p.incomeCents,
		})
	}

	// Average savings rate.
	avgRate := 0.0
	if monthsWithData > 0 {
		var sum float64
		for _, e := range entries {
			sum += e.SavingsRatePct
		}
		avgRate = math.Round(sum/float64(monthsWithData)*10) / 10
	}

	// Income stability via coefficient of variation.
	incomeCV := 0.0
	stability := "stable"
	if monthsWithData >= 2 {
		incomes := make([]float64, len(entries))
		var meanIncome float64
		for i, e := range entries {
			incomes[i] = float64(e.IncomeCents)
			meanIncome += incomes[i]
		}
		meanIncome /= float64(len(incomes))
		if meanIncome > 0 {
			var variance float64
			for _, v := range incomes {
				diff := v - meanIncome
				variance += diff * diff
			}
			variance /= float64(len(incomes))
			stdDev := math.Sqrt(variance)
			incomeCV = math.Round(stdDev/meanIncome*100*10) / 10
		}
	}
	stabilityLabel := "Stable"
	if incomeCV >= 25.0 {
		stability = "variable"
		stabilityLabel = "Variable"
	} else if incomeCV >= 10.0 {
		stability = "moderate"
		stabilityLabel = "Moderate"
	}

	// Savings streak: consecutive positive-net months from most recent.
	savingsStreak := 0
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].NetCents > 0 {
			savingsStreak++
		} else {
			break
		}
	}

	// Pre-compute SVG sparkline values.
	const chartW = 480
	const chartH = 80.0
	maxRate := -math.MaxFloat64
	minRate := math.MaxFloat64
	for _, e := range entries {
		if e.SavingsRatePct > maxRate {
			maxRate = e.SavingsRatePct
		}
		if e.SavingsRatePct < minRate {
			minRate = e.SavingsRatePct
		}
	}
	if maxRate == -math.MaxFloat64 {
		maxRate = 0
		minRate = 0
	}
	rangeRate := maxRate - minRate
	if rangeRate == 0 {
		rangeRate = 1
	}

	step := int64(0)
	if monthsWithData > 1 {
		step = chartW / int64(monthsWithData-1)
	}

	// Per-entry SparkY and CX coordinates.
	for i := range entries {
		cx := int64(i) * step
		y := chartH - (entries[i].SavingsRatePct-minRate)/rangeRate*chartH
		entries[i].SparkY = y
		entries[i].CX = cx
	}

	// Build polyline points string.
	var sb strings.Builder
	for i, e := range entries {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%d,%.0f", e.CX, e.SparkY)
	}

	// Target line Y (20% target).
	targetY := chartH - (20.0-minRate)/rangeRate*chartH

	// Reversed slice for the monthly breakdown table.
	reversed := make([]SavingsEntry, len(entries))
	for i, e := range entries {
		reversed[len(entries)-1-i] = e
	}

	// X-axis labels: first, mid, last.
	sparkFirst, sparkMid, sparkLast := "", "", ""
	var sparkMidX int64
	if len(entries) > 0 {
		sparkFirst = entries[0].Label
		sparkLast = entries[len(entries)-1].Label
	}
	if len(entries) > 2 {
		midI := len(entries) / 2
		sparkMid = entries[midI].Label
		sparkMidX = entries[midI].CX
	}

	data := InsightsData{
		BaseData:               newBaseData(database, "insights", tmpl.version),
		SavingsHistory:         entries,
		SavingsHistoryReversed: reversed,
		AvgSavingsRatePct:      avgRate,
		IncomeStability:        stability,
		IncomeStabilityLabel:   stabilityLabel,
		SavingsStreak:          savingsStreak,
		MonthsWithData:         monthsWithData,
		SparklinePoints:        sb.String(),
		SparklineMaxRate:       maxRate,
		SparklineMinRate:       minRate,
		SparklineRange:         rangeRate,
		SparklineStep:          step,
		SparklineTargetY:       targetY,
		SparklineFirst:         sparkFirst,
		SparklineMid:           sparkMid,
		SparklineMidX:          sparkMidX,
		SparklineLast:          sparkLast,
	}

	if err := tmpl.Render(w, "base", data); err != nil {
		log.Printf("insights render: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Review handler
// -----------------------------------------------------------------------------

func handleReviewView(w http.ResponseWriter, r *http.Request, database *db.DB, tmpl *TemplateEngine) {
	q := r.URL.Query()
	accountsParam := q.Get("accounts")
	showNoData := strings.ToLower(accountsParam) == "none"

	var selectedAccounts []string
	if !showNoData && accountsParam != "" {
		for _, a := range strings.Split(accountsParam, ",") {
			if s := strings.TrimSpace(a); s != "" {
				selectedAccounts = append(selectedAccounts, s)
			}
		}
	}

	allCats := make([]categorize.Category, 0, len(categorize.Categories))
	for _, c := range categorize.Categories {
		allCats = append(allCats, c)
	}
	sort.Slice(allCats, func(i, j int) bool { return allCats[i].Name < allCats[j].Name })

	data := ReviewData{
		BaseData:         newBaseData(database, "review", tmpl.version),
		AllAccounts:      queryAllAccounts(database),
		SelectedAccounts: selectedAccounts,
		ShowNoData:       showNoData,
		AllCategories:    allCats,
	}

	if showNoData {
		if err := tmpl.Render(w, "base", data); err != nil {
			log.Printf("review render: %v", err)
		}
		return
	}

	now := time.Now().UTC()
	y, m, d := now.Date()
	startISO := fmt.Sprintf("%04d-%02d-01", y, m)
	// Exclusive end = tomorrow. Use time.Date so Go normalizes day overflow
	// (e.g. day 32 wraps into the next month correctly).
	tomorrow := time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
	endISO := fmt.Sprintf("%04d-%02d-%02d", tomorrow.Year(), tomorrow.Month(), tomorrow.Day())

	// Fetch category overrides to classify transactions.
	overrides, _ := database.GetCategoryOverrides()

	// Top merchants by spend.
	data.TopMerchants = queryTopMerchants(database, startISO, endISO, selectedAccounts)

	// Uncategorized expenses (ghosts) and low-confidence transactions.
	data.Ghosts, data.LowConfidence = queryReviewQueues(database, startISO, endISO, selectedAccounts, overrides)
	data.NeedsReviewCount = len(data.Ghosts) + len(data.LowConfidence)

	// Coverage stats.
	data.Coverage = queryReviewCoverage(database, startISO, endISO, selectedAccounts, overrides)

	if err := tmpl.Render(w, "base", data); err != nil {
		log.Printf("review render: %v", err)
	}
}

// queryTopMerchants returns the top 20 merchants by absolute spend for the period.
func queryTopMerchants(database *db.DB, startISO, endISO string, accountFilter []string) []MerchantSummary {
	merchantNormExpr := "TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), '')))"
	q := fmt.Sprintf(`
		SELECT %s AS merchant_norm,
		       COUNT(*) AS cnt,
		       SUM(ABS(amount_cents)) AS total,
		       MAX(posted_at) AS last_seen
		FROM transactions
		WHERE amount_cents < 0
		  AND COALESCE(pending, 0) = 0
		  AND posted_at >= ? AND posted_at < ?
	`, merchantNormExpr)
	args := []any{startISO, endISO}
	q, args = appendAccountFilter(q, args, accountFilter)
	q += " GROUP BY merchant_norm ORDER BY total DESC LIMIT 20"

	rows, err := database.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []MerchantSummary
	for rows.Next() {
		var m MerchantSummary
		if err := rows.Scan(&m.Merchant, &m.Count, &m.TotalCents, &m.LastSeen); err != nil {
			continue
		}
		m.Status = "untracked"
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	return out
}

// queryReviewQueues returns ghost (uncategorized) and low-confidence transactions.
// A transaction is a ghost if it has no category override and the classifier
// returns "other". Low confidence means confidence < 0.8.
func queryReviewQueues(
	database *db.DB,
	startISO, endISO string,
	accountFilter []string,
	overrides map[string]string,
) (ghosts []GhostTxn, lowConf []LowConfTxn) {
	merchantNorm := "TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), '')))"
	q := fmt.Sprintf(`
		SELECT %s AS mn,
		       posted_at,
		       amount_cents,
		       COALESCE(description,'') AS desc_raw,
		       COALESCE(merchant,'') AS merch_raw
		FROM transactions
		WHERE amount_cents < 0
		  AND COALESCE(pending, 0) = 0
		  AND posted_at >= ? AND posted_at < ?
	`, merchantNorm)
	args := []any{startISO, endISO}
	q, args = appendAccountFilter(q, args, accountFilter)
	q += " ORDER BY posted_at DESC LIMIT 200"

	rows, err := database.Query(q, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	catNames := make(map[string]string, len(categorize.Categories))
	for id, cat := range categorize.Categories {
		catNames[id] = cat.Name
	}

	for rows.Next() {
		var mn, postedAt, descRaw, merchRaw string
		var amtCents int64
		if err := rows.Scan(&mn, &postedAt, &amtCents, &descRaw, &merchRaw); err != nil {
			continue
		}

		date := postedAt
		if len(date) >= 10 {
			date = date[:10]
		}

		// Determine category and confidence.
		catID, conf := categorize.CategorizeMerchant(mn, descRaw)
		if overCat, ok := overrides[mn]; ok {
			catID = overCat
			conf = 1.0
		}

		if catID == "other" || catID == "" {
			ghosts = append(ghosts, GhostTxn{
				Date:           date,
				MerchantNorm:   mn,
				AmountCents:    amtCents,
				RawDescription: descRaw,
			})
		} else if conf < 0.8 {
			catName := catNames[catID]
			if catName == "" {
				catName = titleCase(catID)
			}
			lowConf = append(lowConf, LowConfTxn{
				Date:         date,
				MerchantNorm: mn,
				AmountCents:  amtCents,
				CategoryName: catName,
				Confidence:   conf * 100, // stored as 0..100 for template display
			})
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	return
}

// queryReviewCoverage computes the coverage statistics for the review page footer.
func queryReviewCoverage(
	database *db.DB,
	startISO, endISO string,
	accountFilter []string,
	overrides map[string]string,
) ReviewCoverage {
	merchantNorm := "TRIM(LOWER(COALESCE(NULLIF(merchant,''), NULLIF(description,''), '')))"
	q := fmt.Sprintf(`
		SELECT %s AS mn,
		       amount_cents,
		       COALESCE(description,'') AS desc_raw
		FROM transactions
		WHERE COALESCE(pending, 0) = 0
		  AND posted_at >= ? AND posted_at < ?
	`, merchantNorm)
	args := []any{startISO, endISO}
	q, args = appendAccountFilter(q, args, accountFilter)

	rows, err := database.Query(q, args...)
	if err != nil {
		return ReviewCoverage{}
	}
	defer rows.Close()

	var totalTxns, expenseTotal, categorized, highConf int
	trackedSet := make(map[string]bool)

	for rows.Next() {
		var mn, descRaw string
		var amtCents int64
		if err := rows.Scan(&mn, &amtCents, &descRaw); err != nil {
			continue
		}
		totalTxns++

		catID, conf := categorize.CategorizeMerchant(mn, descRaw)
		if overCat, ok := overrides[mn]; ok {
			catID = overCat
			conf = 1.0
		}

		if amtCents < 0 {
			expenseTotal++
			if catID != "" && catID != "other" {
				categorized++
				trackedSet[mn] = true
			}
		}

		if conf >= 0.8 {
			highConf++
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	catPct := 100
	if expenseTotal > 0 {
		catPct = categorized * 100 / expenseTotal
	}

	return ReviewCoverage{
		TotalTxns:        totalTxns,
		CatPct:           catPct,
		TrackedMerchants: len(trackedSet),
		LowConf:          totalTxns - highConf,
	}
}
