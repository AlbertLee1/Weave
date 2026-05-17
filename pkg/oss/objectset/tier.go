package objectset

import (
	"context"
	"time"
)

// DefaultHotWindow is the rolling window the cold-tier router uses when
// no override is set via Executor.SetHotWindow. Mirrors the PRD value
// exposed via WEAVE_HOT_WINDOW_HOURS=24 (US-407): rows that landed within
// the last 24h live in Bleve (hot); older rows live in the Parquet cold
// tier and surface here via TierRouter.
const DefaultHotWindow = 24 * time.Hour

// TierRouter is the optional cold-tier read path the executor consults
// alongside its primary Bleve query (US-407). When wired the executor
// merges hot (Bleve) and cold (Parquet) primary keys with hot-wins
// dedup so rows older than the hot window remain queryable even when the
// hot tier has dropped them (eviction, truncation, or a configured
// hot-only cache).
//
// `before` is the inclusive upper bound on the cold view's wall-clock
// cutoff: rows whose materialised timestamp is later than `before` belong
// to the hot tier and must NOT be returned. Implementations are expected
// to be safe for concurrent use.
type TierRouter interface {
	ColdPrimaryKeys(ctx context.Context, ontologyAPIName, objectType string, before time.Time) ([]string, error)
}

// SetTierRouter wires the optional cold-tier router (US-407). Passing a
// nil argument detaches the router so subsequent base reads short-circuit
// straight back to the hot tier — the contract used by degraded-mode
// boots and tests that opt out of cold lookups mid-fixture.
func (e *Executor) SetTierRouter(r TierRouter) {
	e.tierRouter = r
}

// SetHotWindow overrides the rolling window the cold tier sees as its
// upper bound. Non-positive values are ignored so callers can pass
// time.Duration zero values without clobbering the default.
func (e *Executor) SetHotWindow(d time.Duration) {
	if d <= 0 {
		return
	}
	e.hotWindow = d
}

// SetTierNowFunc replaces the wall-clock the executor stamps onto cold
// lookups. Tests use it to pin the cutoff so the merge is deterministic.
// nil is a no-op so callers can pass through an absent override.
func (e *Executor) SetTierNowFunc(fn func() time.Time) {
	if fn == nil {
		return
	}
	e.tierNowFn = fn
}

// effectiveHotWindow returns the configured hot window, falling back to
// DefaultHotWindow when the executor wasn't explicitly tuned.
func (e *Executor) effectiveHotWindow() time.Duration {
	if e.hotWindow > 0 {
		return e.hotWindow
	}
	return DefaultHotWindow
}

// effectiveNow is the wall-clock anchor used to compute cold cutoffs.
// Falls back to time.Now().UTC() when no override is wired.
func (e *Executor) effectiveNow() time.Time {
	if e.tierNowFn != nil {
		return e.tierNowFn()
	}
	return time.Now().UTC()
}

// tierRoutingPlan captures the executor's decision for a single base
// query: whether to fan out to hot, cold, or both, and what cutoff to
// stamp on the cold-tier lookup.
//
// Centralising the rules here keeps `executeBase` linear and makes the
// US-485 classifier independently unit-testable.
type tierRoutingPlan struct {
	queryHot   bool
	queryCold  bool
	coldCutoff time.Time
}

// classifyTierRouting decides hot/cold routing for an optional
// caller-declared TimeRangeHint (US-485). When no hint is supplied the
// executor preserves the US-407 behaviour: query both tiers and merge.
//
// Rules (with hotBoundary = now - hotWindow):
//   - Hot-only: TimeRange.From != nil AND From >= hotBoundary →
//     skip cold, cutoff irrelevant.
//   - Cold-only: TimeRange.To != nil AND To <= hotBoundary →
//     skip hot, cold cutoff = *To (so the router clips rows past the
//     request's upper bound).
//   - Cross-window or open-ended: both tiers, cold cutoff = hotBoundary.
//
// Both bounds may be nil — a hint with only To set falls into either the
// cold-only or cross-window branch depending on where To lands. A hint
// whose From sits beyond now (in the future) is treated as hot-only
// because no cold rows can satisfy "later than now".
func classifyTierRouting(tr *TimeRangeHint, now time.Time, hotWindow time.Duration) tierRoutingPlan {
	hotBoundary := now.Add(-hotWindow)
	if tr == nil {
		return tierRoutingPlan{queryHot: true, queryCold: true, coldCutoff: hotBoundary}
	}
	if tr.From != nil && !tr.From.Before(hotBoundary) {
		return tierRoutingPlan{queryHot: true, queryCold: false}
	}
	if tr.To != nil && !tr.To.After(hotBoundary) {
		return tierRoutingPlan{queryHot: false, queryCold: true, coldCutoff: *tr.To}
	}
	return tierRoutingPlan{queryHot: true, queryCold: true, coldCutoff: hotBoundary}
}

// mergeHotColdPKs returns the union of hot and cold primary keys, dedup'd
// by string, preserving the hot tier's original ordering and appending
// cold-only PKs in the order the cold tier delivered them. Hot-wins is
// the canonical merge contract: when a PK shows up in both tiers the hot
// position wins because the hot tier carries the post-conflict-resolution
// state — its ordering is what callers (cursor pagination, e.g.) already
// rely on.
func mergeHotColdPKs(hot, cold []string) []string {
	if len(cold) == 0 {
		return hot
	}
	if len(hot) == 0 {
		out := make([]string, 0, len(cold))
		seen := make(map[string]struct{}, len(cold))
		for _, pk := range cold {
			if _, ok := seen[pk]; ok {
				continue
			}
			seen[pk] = struct{}{}
			out = append(out, pk)
		}
		return out
	}
	seen := make(map[string]struct{}, len(hot)+len(cold))
	out := make([]string, 0, len(hot)+len(cold))
	for _, pk := range hot {
		if _, ok := seen[pk]; ok {
			continue
		}
		seen[pk] = struct{}{}
		out = append(out, pk)
	}
	for _, pk := range cold {
		if _, ok := seen[pk]; ok {
			continue
		}
		seen[pk] = struct{}{}
		out = append(out, pk)
	}
	return out
}
