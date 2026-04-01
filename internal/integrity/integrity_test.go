package integrity_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/arclighteng/fin-go/internal/classify"
	"github.com/arclighteng/fin-go/internal/integrity"
)

// periodRange returns a fixed start/end pair for test reports.
func periodRange() (time.Time, time.Time) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	return start, end
}

func ptrTransfer(s classify.TransferStatus) *classify.TransferStatus { return &s }

// buildCleanReport constructs a report with no integrity flags.
func buildCleanReport(t *testing.T) *classify.Report {
	t.Helper()
	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "i1", TxnType: classify.TxnIncome, AmountCents: 500000},
	}
	return classify.BuildReport(txns, classify.TimePeriodMonth, start, end)
}

// ---- Badge -----------------------------------------------------------------

func TestBadge_Excellent(t *testing.T) {
	t.Parallel()
	label, css := integrity.Badge(1.0)
	if label != "Excellent" {
		t.Errorf("Badge(1.0) label = %q, want Excellent", label)
	}
	if css != "badge-success" {
		t.Errorf("Badge(1.0) css = %q, want badge-success", css)
	}
}

func TestBadge_ExcellentBoundary(t *testing.T) {
	t.Parallel()
	label, _ := integrity.Badge(0.95)
	if label != "Excellent" {
		t.Errorf("Badge(0.95) label = %q, want Excellent", label)
	}
}

func TestBadge_Good(t *testing.T) {
	t.Parallel()
	label, css := integrity.Badge(0.85)
	if label != "Good" {
		t.Errorf("Badge(0.85) label = %q, want Good", label)
	}
	if css != "badge-info" {
		t.Errorf("Badge(0.85) css = %q, want badge-info", css)
	}
}

func TestBadge_GoodBoundary(t *testing.T) {
	t.Parallel()
	label, _ := integrity.Badge(0.80)
	if label != "Good" {
		t.Errorf("Badge(0.80) label = %q, want Good", label)
	}
}

func TestBadge_Fair(t *testing.T) {
	t.Parallel()
	label, css := integrity.Badge(0.70)
	if label != "Fair" {
		t.Errorf("Badge(0.70) label = %q, want Fair", label)
	}
	if css != "badge-warning" {
		t.Errorf("Badge(0.70) css = %q, want badge-warning", css)
	}
}

func TestBadge_FairBoundary(t *testing.T) {
	t.Parallel()
	label, _ := integrity.Badge(0.60)
	if label != "Fair" {
		t.Errorf("Badge(0.60) label = %q, want Fair", label)
	}
}

func TestBadge_NeedsAttention(t *testing.T) {
	t.Parallel()
	label, css := integrity.Badge(0.50)
	if label != "Needs Attention" {
		t.Errorf("Badge(0.50) label = %q, want Needs Attention", label)
	}
	if css != "badge-danger" {
		t.Errorf("Badge(0.50) css = %q, want badge-danger", css)
	}
}

func TestBadge_Zero(t *testing.T) {
	t.Parallel()
	label, css := integrity.Badge(0.0)
	if label != "Needs Attention" {
		t.Errorf("Badge(0.0) label = %q, want Needs Attention", label)
	}
	if css != "badge-danger" {
		t.Errorf("Badge(0.0) css = %q, want badge-danger", css)
	}
}

// ---- PriorityLabel ---------------------------------------------------------

func TestPriorityLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		priority int
		want     string
	}{
		{1, "Critical"},
		{2, "High"},
		{3, "Medium"},
		{4, "Low"},
		{5, "Low"},  // out of range → Low
		{0, "Low"},  // out of range → Low
		{-1, "Low"}, // negative → Low
	}

	for _, tc := range tests {
		name := fmt.Sprintf("priority_%d", tc.priority)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := integrity.PriorityLabel(tc.priority)
			if got != tc.want {
				t.Errorf("PriorityLabel(%d) = %q, want %q", tc.priority, got, tc.want)
			}
		})
	}
}

// ---- Assess ----------------------------------------------------------------

func TestAssess_PerfectScore(t *testing.T) {
	t.Parallel()

	report := buildCleanReport(t)
	a := integrity.Assess(report)

	if a.Score != 1.0 {
		t.Errorf("Score = %f, want 1.0", a.Score)
	}
	if !a.IsActionable {
		t.Error("IsActionable = false, want true for perfect score")
	}
	if a.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want empty for actionable report", a.BlockedReason)
	}
	if len(a.Flags) != 0 {
		t.Errorf("Flags = %v, want empty", a.Flags)
	}
	if len(a.ResolutionTasks) != 0 {
		t.Errorf("ResolutionTasks = %v, want empty", a.ResolutionTasks)
	}
}

func TestAssess_UnclassifiedCreditReducesScore(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "cr1", TxnType: classify.TxnCreditOther, AmountCents: 5000},
	}
	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)
	a := integrity.Assess(report)

	// FlagUnclassifiedCredit penalty = 0.10 → score = 0.90.
	if a.Score < 0.89 || a.Score > 0.91 {
		t.Errorf("Score = %f, want ~0.90", a.Score)
	}
	if !a.IsActionable {
		t.Error("IsActionable should be true at 0.90")
	}

	taskFound := false
	for _, task := range a.ResolutionTasks {
		if task.TaskType == "CLASSIFY_CREDIT" {
			taskFound = true
		}
	}
	if !taskFound {
		t.Error("expected CLASSIFY_CREDIT task")
	}
}

func TestAssess_UnmatchedTransferReducesScore(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{
			Fingerprint:    "xfr",
			TxnType:        classify.TxnTransfer,
			AmountCents:    -1000,
			TransferStatus: ptrTransfer(classify.TransferUnmatched),
		},
	}
	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)
	a := integrity.Assess(report)

	// FlagUnmatchedTransfer penalty = 0.05 → score = 0.95.
	if a.Score < 0.94 || a.Score > 0.96 {
		t.Errorf("Score = %f, want ~0.95", a.Score)
	}

	taskFound := false
	for _, task := range a.ResolutionTasks {
		if task.TaskType == "MATCH_TRANSFER" {
			taskFound = true
		}
	}
	if !taskFound {
		t.Error("expected MATCH_TRANSFER task")
	}
}

func TestAssess_ReconciliationFailedTask(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)
	// Manually inject the reconciliation flag.
	report.Integrity.Flags = append(report.Integrity.Flags, classify.FlagReconciliationFailed)
	report.Integrity.ReconciliationDeltaCents = 1500

	a := integrity.Assess(report)

	taskFound := false
	for _, task := range a.ResolutionTasks {
		if task.TaskType == "RECONCILE" {
			taskFound = true
			if task.AffectedCents != 1500 {
				t.Errorf("RECONCILE AffectedCents = %d, want 1500", task.AffectedCents)
			}
		}
	}
	if !taskFound {
		t.Error("expected RECONCILE task")
	}
}

func TestAssess_DuplicateSuspectedTask(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)
	report.Integrity.Flags = append(report.Integrity.Flags, classify.FlagDuplicateSuspected)
	report.Integrity.DuplicateSuspectCount = 3

	a := integrity.Assess(report)

	taskFound := false
	for _, task := range a.ResolutionTasks {
		if task.TaskType == "REVIEW_DUPLICATES" {
			taskFound = true
			if task.Priority != 3 {
				t.Errorf("REVIEW_DUPLICATES priority = %d, want 3", task.Priority)
			}
		}
	}
	if !taskFound {
		t.Error("expected REVIEW_DUPLICATES task")
	}
}

func TestAssess_PendingInTotalsTask(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)
	report.Integrity.Flags = append(report.Integrity.Flags, classify.FlagPendingInTotals)

	a := integrity.Assess(report)

	taskFound := false
	for _, task := range a.ResolutionTasks {
		if task.TaskType == "REVIEW_PENDING" {
			taskFound = true
		}
	}
	if !taskFound {
		t.Error("expected REVIEW_PENDING task")
	}
}

func TestAssess_InformationalFlagsProduceNoTask(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)
	report.Integrity.Flags = append(report.Integrity.Flags, classify.FlagEmptyAccountFilter)

	a := integrity.Assess(report)

	for _, task := range a.ResolutionTasks {
		if task.TaskType == "REVIEW" || task.TaskType == "EMPTY_ACCOUNT" {
			t.Errorf("informational flag should not produce task, got %q", task.TaskType)
		}
	}
}

func TestAssess_ScoreClampedAtZero(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)
	// Stack many heavy flags to push penalty past 1.0.
	report.Integrity.Flags = []classify.IntegrityFlag{
		classify.FlagReconciliationFailed, // 0.20
		classify.FlagFutureDataLeak,       // 0.15
		classify.FlagUnclassifiedCredit,   // 0.10
		classify.FlagPendingInTotals,      // 0.10
		classify.FlagUnmatchedTransfer,    // 0.05
		classify.FlagDuplicateSuspected,   // 0.05
	}

	a := integrity.Assess(report)

	if a.Score < 0 {
		t.Errorf("Score = %f, must never be negative", a.Score)
	}
	if a.IsActionable {
		t.Error("IsActionable should be false when score is very low")
	}
	if a.BlockedReason == "" {
		t.Error("BlockedReason should be non-empty when not actionable")
	}
}

func TestAssess_BelowThresholdBlockedReason(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)
	// Reconciliation failure alone = 0.80 score, right at threshold.
	// Add unclassified credit (0.10) to push below 0.80.
	report.Integrity.Flags = []classify.IntegrityFlag{
		classify.FlagReconciliationFailed, // 0.20 penalty → score 0.80 (at threshold)
		classify.FlagUnclassifiedCredit,   // 0.10 more → score 0.70 (below)
	}

	a := integrity.Assess(report)

	if a.IsActionable {
		t.Error("IsActionable should be false at score ~0.70")
	}
	if a.BlockedReason == "" {
		t.Error("BlockedReason must not be empty when not actionable")
	}
}

func TestAssess_TasksSortedByPriority(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)
	report.Integrity.Flags = []classify.IntegrityFlag{
		classify.FlagDuplicateSuspected,  // priority 3
		classify.FlagUnmatchedTransfer,   // priority 2
		classify.FlagUnclassifiedCredit,  // priority 1
	}
	report.Integrity.UnclassifiedCreditCount = 1
	report.Integrity.UnmatchedTransferCount = 1
	report.Integrity.DuplicateSuspectCount = 1

	a := integrity.Assess(report)

	for i := 1; i < len(a.ResolutionTasks); i++ {
		if a.ResolutionTasks[i-1].Priority > a.ResolutionTasks[i].Priority {
			t.Errorf("tasks not sorted: [%d].Priority=%d > [%d].Priority=%d",
				i-1, a.ResolutionTasks[i-1].Priority, i, a.ResolutionTasks[i].Priority)
		}
	}
}

// ---- CanShowRecommendations ------------------------------------------------

func TestCanShowRecommendations_TrueWhenActionable(t *testing.T) {
	t.Parallel()
	report := buildCleanReport(t)
	if !integrity.CanShowRecommendations(report) {
		t.Error("CanShowRecommendations should be true for clean report")
	}
}

func TestCanShowRecommendations_FalseWhenNotActionable(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	report := classify.BuildReport(nil, classify.TimePeriodMonth, start, end)
	// Drive score below threshold.
	report.Integrity.Flags = []classify.IntegrityFlag{
		classify.FlagReconciliationFailed,
		classify.FlagFutureDataLeak,
		classify.FlagUnclassifiedCredit,
	}

	if integrity.CanShowRecommendations(report) {
		t.Error("CanShowRecommendations should be false when score is below threshold")
	}
}

// ---- Summary ---------------------------------------------------------------

func TestSummary_Keys(t *testing.T) {
	t.Parallel()

	report := buildCleanReport(t)
	s := integrity.Summary(report)

	requiredKeys := []string{
		"integrity_score",
		"integrity_percent",
		"is_actionable",
		"blocked_reason",
		"tasks",
		"flag_count",
	}
	for _, key := range requiredKeys {
		if _, ok := s[key]; !ok {
			t.Errorf("Summary missing key %q", key)
		}
	}
}

func TestSummary_PercentField(t *testing.T) {
	t.Parallel()

	report := buildCleanReport(t)
	s := integrity.Summary(report)

	pct, ok := s["integrity_percent"].(int)
	if !ok {
		t.Fatalf("integrity_percent is not int: %T", s["integrity_percent"])
	}
	if pct != 100 {
		t.Errorf("integrity_percent = %d, want 100", pct)
	}
}

func TestSummary_FlagCount(t *testing.T) {
	t.Parallel()

	start, end := periodRange()
	txns := []classify.ClassifiedTransaction{
		{Fingerprint: "cr1", TxnType: classify.TxnCreditOther, AmountCents: 5000},
	}
	report := classify.BuildReport(txns, classify.TimePeriodMonth, start, end)
	s := integrity.Summary(report)

	flagCount, ok := s["flag_count"].(int)
	if !ok {
		t.Fatalf("flag_count is not int: %T", s["flag_count"])
	}
	if flagCount != 1 {
		t.Errorf("flag_count = %d, want 1", flagCount)
	}
}
