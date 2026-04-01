// Package integrity provides integrity scoring and resolution task generation
// for classified transaction reports.
//
// The integrity score runs from 0.0 (broken) to 1.0 (perfect). Scores below
// the IntegrityThreshold (0.8) gate recommendations: callers should show
// resolution tasks instead of financial advice when IsActionable returns false.
//
// Truth Contract alignment:
//   - Score is derived solely from IntegrityFlag penalties defined in types.go.
//   - Resolution tasks describe exactly what the user must do to raise the score.
//   - Recommendations are never shown below IntegrityThreshold.
package integrity

import (
	"fmt"
	"sort"

	"github.com/arclighteng/fin-go/internal/classify"
)

// IntegrityThreshold is the minimum score required before recommendations are shown.
const IntegrityThreshold = 0.8

// priorityLabel maps a 1-based priority integer to a human-readable label.
// Priority 1 = Critical, 2 = High, 3 = Medium, 4+ = Low.
var priorityLabel = []string{"Critical", "High", "Medium", "Low"}

// PriorityLabel returns the display label for a 1-based priority value.
// Values outside the known range return "Low".
func PriorityLabel(priority int) string {
	idx := priority - 1
	if idx < 0 || idx >= len(priorityLabel) {
		return "Low"
	}
	return priorityLabel[idx]
}

// Assessment is the complete integrity assessment for a report.
type Assessment struct {
	// Score is the computed integrity score in [0.0, 1.0].
	Score float64
	// IsActionable reports whether recommendations should be shown.
	// True when Score >= IntegrityThreshold.
	IsActionable bool
	// Flags lists the data quality issues present in the report.
	Flags []classify.IntegrityFlag
	// ResolutionTasks is the ordered list of actions the user should take.
	// Sorted by Priority ascending (1 = most urgent).
	ResolutionTasks []classify.ResolutionTask
	// BlockedReason is a human-readable explanation of why recommendations are
	// withheld. Empty when IsActionable is true.
	BlockedReason string
}

// Assess evaluates the integrity of a report and returns a full Assessment.
//
// Resolution tasks are generated for each flag that has a user-facing action.
// Flags that are purely informational (FlagEmptyAccountFilter) produce no task.
func Assess(report *classify.Report) Assessment {
	ir := &report.Integrity
	score := ir.Score()
	actionable := score >= IntegrityThreshold

	tasks := buildResolutionTasks(ir)

	var blockedReason string
	if !actionable {
		blockedReason = fmt.Sprintf(
			"Integrity score (%.0f%%) is below the required threshold (%.0f%%). Resolve the tasks below first.",
			score*100,
			IntegrityThreshold*100,
		)
	}

	return Assessment{
		Score:           score,
		IsActionable:    actionable,
		Flags:           ir.Flags,
		ResolutionTasks: tasks,
		BlockedReason:   blockedReason,
	}
}

// CanShowRecommendations is a convenience wrapper that returns true when the
// report's integrity score is at or above IntegrityThreshold.
func CanShowRecommendations(report *classify.Report) bool {
	return report.Integrity.IsActionable()
}

// Badge returns a display label and CSS class appropriate for the given score.
// Callers can map the class strings to their own styling conventions.
//
// Ranges:
//   - >= 0.95 → "Excellent", "badge-success"
//   - >= 0.80 → "Good",      "badge-info"
//   - >= 0.60 → "Fair",      "badge-warning"
//   - < 0.60  → "Needs Attention", "badge-danger"
func Badge(score float64) (label, cssClass string) {
	switch {
	case score >= 0.95:
		return "Excellent", "badge-success"
	case score >= 0.80:
		return "Good", "badge-info"
	case score >= 0.60:
		return "Fair", "badge-warning"
	default:
		return "Needs Attention", "badge-danger"
	}
}

// Summary returns a map of display-ready values suitable for JSON serialisation
// or template rendering. This mirrors the Python get_resolution_summary output.
//
// Keys: integrity_score, integrity_percent, is_actionable, blocked_reason,
// tasks ([]map[string]any), flag_count.
func Summary(report *classify.Report) map[string]any {
	a := Assess(report)

	taskList := make([]map[string]any, 0, len(a.ResolutionTasks))
	for _, t := range a.ResolutionTasks {
		entry := map[string]any{
			"type":        t.TaskType,
			"description": t.Description,
			"priority":    t.Priority,
			"priority_label": PriorityLabel(t.Priority),
		}
		if t.AffectedCents > 0 {
			entry["affected_amount"] = fmt.Sprintf("$%.2f", float64(t.AffectedCents)/100.0)
		} else {
			entry["affected_amount"] = nil
		}
		taskList = append(taskList, entry)
	}

	return map[string]any{
		"integrity_score":   a.Score,
		"integrity_percent": int(a.Score * 100),
		"is_actionable":     a.IsActionable,
		"blocked_reason":    a.BlockedReason,
		"tasks":             taskList,
		"flag_count":        len(a.Flags),
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// buildResolutionTasks constructs the ordered set of resolution tasks from the
// flags present in the IntegrityReport. Each flag with a user-facing action
// produces exactly one task. Tasks are sorted by Priority ascending.
func buildResolutionTasks(ir *classify.IntegrityReport) []classify.ResolutionTask {
	var tasks []classify.ResolutionTask

	for _, flag := range ir.Flags {
		switch flag {
		case classify.FlagUnclassifiedCredit:
			tasks = append(tasks, classify.ResolutionTask{
				TaskType: "CLASSIFY_CREDIT",
				Description: fmt.Sprintf(
					"Classify %d unclassified credit(s) ($%.2f)",
					ir.UnclassifiedCreditCount,
					float64(ir.UnclassifiedCreditCents)/100.0,
				),
				Priority:      1,
				AffectedCents: ir.UnclassifiedCreditCents,
			})

		case classify.FlagUnmatchedTransfer:
			tasks = append(tasks, classify.ResolutionTask{
				TaskType: "MATCH_TRANSFER",
				Description: fmt.Sprintf(
					"Match or classify %d unmatched transfer(s)",
					ir.UnmatchedTransferCount,
				),
				Priority:      2,
				AffectedCents: 0,
			})

		case classify.FlagReconciliationFailed:
			delta := ir.ReconciliationDeltaCents
			if delta < 0 {
				delta = -delta
			}
			tasks = append(tasks, classify.ResolutionTask{
				TaskType:      "RECONCILE",
				Description:   fmt.Sprintf("Reconcile statement (delta: $%.2f)", float64(delta)/100.0),
				Priority:      1,
				AffectedCents: delta,
			})

		case classify.FlagDuplicateSuspected:
			tasks = append(tasks, classify.ResolutionTask{
				TaskType:      "REVIEW_DUPLICATES",
				Description:   fmt.Sprintf("Review %d suspected duplicate(s)", ir.DuplicateSuspectCount),
				Priority:      3,
				AffectedCents: 0,
			})

		case classify.FlagPendingInTotals:
			tasks = append(tasks, classify.ResolutionTask{
				TaskType:      "REVIEW_PENDING",
				Description:   "Pending transactions are included in totals — post or remove them before finalizing.",
				Priority:      4,
				AffectedCents: 0,
			})

		// FlagEmptyAccountFilter and FlagFutureDataLeak are informational;
		// they do not produce user-facing tasks.
		case classify.FlagEmptyAccountFilter, classify.FlagFutureDataLeak:
			// no task

		default:
			// Forward-compatibility: unknown flags produce a generic task at low priority.
			tasks = append(tasks, classify.ResolutionTask{
				TaskType:      "REVIEW",
				Description:   fmt.Sprintf("Data quality issue detected (flag %d) — review your transactions.", int(flag)),
				Priority:      4,
				AffectedCents: 0,
			})
		}
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Priority < tasks[j].Priority
	})
	return tasks
}
