# ADR-001: Fix template `slice` override and data assembly gaps

**Date**: 2026-04-03
**Status**: Proposed
**Deciders**: Project maintainer
**Tags**: architecture, frontend-contract, data-integrity

---

## Context

An architecture review of the fin-go server identified two classes of issues:

1. **The custom `slice` template function overrides Go's built-in `slice`**, but only
   handles three concrete types (`[]CategoryItem`, `[]PeriodSummary`, `[]AttentionItem`).
   Templates use `slice` on strings (e.g., `slice .AccountID 0 12` to truncate long IDs,
   `slice .Stats.Earliest 0 10` to extract date prefixes). Because the custom function
   falls through to `default: return v` for unrecognized types, these calls silently
   return the full string instead of the expected substring. Users see raw ISO timestamps
   and full account IDs where the UI intends truncated values.

2. **Several view-model fields are declared and referenced in templates but never populated
   by the data assembly layer**, including `RecurringCents`, `TransferCents`,
   `ClosedPeriod`, `AttentionItems` (DashboardData), and `Duplicates`/`NotYetPosted`
   (CommitmentsData). Templates guard these with `{{if}}` so there are no panics, but
   entire UI sections (Heads Up alerts, Baseline income calculation, duplicate detection)
   are permanently hidden.

3. **The search API returns `AccountID` as `account_name`**, showing raw identifiers
   instead of human-readable account names.

The consequence of doing nothing is that the UI degrades silently: truncation never works,
several dashboard features are invisible, and search results display internal IDs.

---

## Decision

We will:

1. **Add `string` handling to `templateSlice`** so that `slice` on strings delegates to
   Go's built-in substring behavior (rune-safe slicing with bounds clamping).

2. **Populate `RecurringCents`** in `queryPeriodTotals` by joining against `commitments`
   (or `category_overrides` where category is recurring), so the cash-flow card and
   baseline calculation work correctly. `TransferCents` can remain zero until transfer
   detection is implemented.

3. **Wire up `AttentionItems`** in `populateDashboard` or remove the struct field and
   template section to avoid dead code. Same for `Duplicates`/`NotYetPosted` in
   CommitmentsData.

4. **Join accounts in `SearchTransactions`** (or the handler mapping) so `account_name`
   returns the display name rather than the raw `account_id`.

---

## Decision Drivers

- **Correctness**: Template output must match what the view model intends. Silent fallthrough
  in `templateSlice` violates the principle of least surprise.
- **Feature completeness**: Declared-but-unpopulated fields create confusion about what
  the app actually does vs. what it intends to do.
- **User trust**: A finance dashboard that shows raw account IDs or broken truncation
  erodes confidence in the data.

---

## Consequences

### Positive
- `slice` calls on strings will produce correct truncated output.
- Dashboard cash-flow card will distinguish recurring from discretionary spending.
- Search results will show human-readable account names.
- Dead template sections either come alive or are removed, reducing code confusion.

### Negative
- Populating `RecurringCents` requires a definition of "recurring" that may need
  iteration (commitment-based vs. category-based vs. pattern-detected).
- Adding string support to `templateSlice` makes it a more complex function with
  more type branches to maintain.

### Neutral / Ongoing
- `AttentionItems` and `Duplicates`/`NotYetPosted` remain feature decisions: populate
  them or remove the dead code. Either path requires a deliberate choice.

---

## Alternatives Considered

### Option A: Rename custom `slice` to `sliceList` and keep built-in
**Description**: Use a different name for the custom function so Go's built-in `slice`
handles strings natively.
**Pros**: No need to reimplement string slicing; simpler custom function.
**Cons**: Requires updating all templates that call `slice` on list types.
**Reason rejected**: Viable, but updating templates is more churn than adding a string
case to the existing function.

### Doing nothing
**Description**: Leave the current behavior in place.
**Reason rejected**: String truncation is visibly broken in the UI, and unpopulated
fields leave declared features permanently hidden.

---

## Implementation Notes

- When adding string support to `templateSlice`, use `[]rune` conversion for
  Unicode safety and clamp `lo`/`hi` to `[0, len(runes)]`.
- For the `RecurringCents` population, start with commitments marked `confirmed=true`
  and `direction=expense` as the source of recurring spend classification.
- The `account_name` fix in search can be done with a LEFT JOIN on `accounts` in the
  `SearchTransactions` query, matching the pattern used in `queryRecentInserts`.

---

## Review Trigger

Revisit this ADR if:
- A new template type needs `slice` support (consider the rename approach at that point).
- The definition of "recurring" changes (e.g., ML-based detection vs. commitment-based).

---

## References

- Related files: `internal/server/templates.go`, `internal/server/views.go`,
  `internal/server/views_extra.go`, `internal/server/api.go`
- Template files: `ui/templates/dashboard.html`, `ui/templates/sync-log.html`,
  `ui/templates/commitments.html`
