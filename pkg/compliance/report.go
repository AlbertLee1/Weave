// Package compliance assembles a SOC2 / ISO27001-style control-evidence
// report for a Weave deployment (US-270). The generator composes over
// narrow source interfaces (audit events, markings, policies) so each
// subsystem can be wired independently and degraded-mode deployments
// still emit a useful report with only the sources they have.
//
// Report shape is stable over the wire: handlers render it as JSON
// directly, and RenderHTML stamps out a printable single-file HTML
// document from the same Report value. The generator does NO I/O of its
// own — every number in the report comes from a source call, so test
// fakes plug in trivially.
package compliance

import (
	"context"
	"sort"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

// Report is the top-level control-evidence bundle. Sections with no data
// (absent source, empty result) MUST emit an empty-but-non-nil slice /
// map so downstream HTML rendering and JSON consumers don't have to do
// presence-checks on every field.
type Report struct {
	// GeneratedAt is the instant the report was assembled, UTC.
	GeneratedAt time.Time `json:"generatedAt"`
	// WindowFrom / WindowTo define the inclusive time window the access
	// statistics cover. Zero value of From means "since the start of
	// recorded audit history"; zero value of To means "until now".
	WindowFrom time.Time `json:"windowFrom,omitempty"`
	WindowTo   time.Time `json:"windowTo,omitempty"`
	// Access is the per-action and per-actor breakdown of audit events
	// within the window. Always non-nil; empty when the AuditSource is
	// not configured.
	Access AccessStatistics `json:"access"`
	// Markings summarises every marking defined on the deployment and
	// the number of users holding grants on each one. Always non-nil.
	Markings MarkingDistribution `json:"markings"`
	// Policies summarises the row-level / column-level / cell-level
	// policy surface and how many ObjectTypes each one covers. Always
	// non-nil.
	Policies PolicyCoverage `json:"policies"`
}

// AccessStatistics is the summary of audit events in the report window.
// Totals are computed from the same set of events so the numbers add
// up: sum over ByAction == Total; len(TopActors) <= len(unique actors).
type AccessStatistics struct {
	// Total is the total number of audit events in the window.
	Total int `json:"total"`
	// UniqueActors is the count of distinct actor_ids in the window.
	UniqueActors int `json:"uniqueActors"`
	// ByAction is a sorted slice of (action, count) pairs. Sorted by
	// count DESC then action ASC so stable test assertions are easy.
	ByAction []ActionCount `json:"byAction"`
	// TopActors is a sorted slice of (actorId, count) pairs capped at
	// the top 10 actors by event count. Same sort as ByAction.
	TopActors []ActorCount `json:"topActors"`
}

// ActionCount is one (action, count) tuple in the ByAction histogram.
type ActionCount struct {
	Action string `json:"action"`
	Count  int    `json:"count"`
}

// ActorCount is one (actor, count) tuple in the TopActors histogram.
type ActorCount struct {
	ActorID string `json:"actorId"`
	Count   int    `json:"count"`
}

// MarkingDistribution is the per-marking grant-count summary. Every
// marking defined on the deployment appears in Markings even when no
// user holds a grant; this is intentional — operators need to see that
// an SECRET marking exists and has zero grants as actively as they need
// to see PUBLIC with many.
type MarkingDistribution struct {
	// Total is len(Markings).
	Total int `json:"total"`
	// Markings is the sorted (by Name) list of (marking, grant count)
	// tuples. Always non-nil.
	Markings []MarkingSummary `json:"markings"`
}

// MarkingSummary is one row in MarkingDistribution.Markings.
type MarkingSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"`
	GrantCount  int    `json:"grantCount"`
}

// PolicyCoverage summarises the enforcement surfaces wired on the
// deployment. "Coverage" is the ratio of ObjectTypes with at least one
// policy to the total number of ObjectTypes — a rough operator-facing
// heuristic, not a strict compliance SLA.
type PolicyCoverage struct {
	// ObjectTypesTotal is the total number of ObjectTypes across every
	// ontology. Zero when no ObjectTypeSource is configured.
	ObjectTypesTotal int `json:"objectTypesTotal"`
	// RowPolicies counts row_policies rows. CoveredObjectTypes is the
	// number of distinct ObjectTypes that have at least one row policy.
	RowPolicies        PolicySurface `json:"rowPolicies"`
	ColumnMasks        PolicySurface `json:"columnMasks"`
	CellMasks          PolicySurface `json:"cellMasks"`
	// CoveredObjectTypes is the union of ObjectTypes covered by ANY of
	// the three surfaces above.
	CoveredObjectTypes int `json:"coveredObjectTypes"`
	// CoverageRatio is CoveredObjectTypes / ObjectTypesTotal, clamped
	// to 0 when the denominator is zero so callers don't need to guard
	// against NaN.
	CoverageRatio float64 `json:"coverageRatio"`
}

// PolicySurface is the per-surface row-count + ObjectType-coverage tuple.
type PolicySurface struct {
	Total              int `json:"total"`
	CoveredObjectTypes int `json:"coveredObjectTypes"`
}

// AuditSource reads audit events inside the report window. The
// implementation MAY cap the returned slice — the generator treats the
// slice as the sole authoritative event set and does not page. Production
// adapters wrap audit.Store.List with a large PageSize so month-scale
// windows fit in memory; callers wanting month-scale evidence should
// narrow the window.
type AuditSource interface {
	ListEvents(ctx context.Context, from, to time.Time) ([]audit.AuditEvent, error)
}

// MarkingSource lists the markings defined on the deployment and the
// number of live grants for each one. Split into two calls so degraded-
// mode deployments with only the request-hot-path MarkingRepository
// (no admin-grant surface) still populate the Markings list with zero
// grant counts.
type MarkingSource interface {
	ListMarkings(ctx context.Context) ([]MarkingInfo, error)
	CountGrants(ctx context.Context, markingName string) (int, error)
}

// MarkingInfo is the compliance-scoped view of a marking definition.
// Duplicating the shape keeps pkg/compliance free of pkg/auth imports,
// matching the same decoupling pattern gdpr.MediaAssetInfo uses.
type MarkingInfo struct {
	Name        string
	DisplayName string
	Description string
	Color       string
}

// ObjectTypeSource counts ObjectTypes across every ontology. Used to
// compute the coverage denominator; absent source leaves the ratio at 0.
type ObjectTypeSource interface {
	CountObjectTypes(ctx context.Context) (int, error)
}

// PolicySource reports counts for each enforcement surface. Each method
// returns the total row count and the list of distinct ObjectType RIDs
// covered; absent source (nil method set) contributes zero-valued stats.
type PolicySource interface {
	RowPolicyStats(ctx context.Context) (total int, objectTypes []string, err error)
	ColumnMaskStats(ctx context.Context) (total int, objectTypes []string, err error)
	CellMaskStats(ctx context.Context) (total int, objectTypes []string, err error)
}

// Generator assembles a Report from the wired sources. Every source is
// optional — nil produces an empty section. Constructor is New() with
// no source args; callers populate the fields directly.
type Generator struct {
	Audit       AuditSource
	Markings    MarkingSource
	ObjectTypes ObjectTypeSource
	Policies    PolicySource
	nowFunc     func() time.Time
	// MaxTopActors caps the TopActors slice in AccessStatistics. Zero
	// means no cap; default is 10 applied in New().
	MaxTopActors int
}

// DefaultMaxTopActors is the default cap on AccessStatistics.TopActors.
const DefaultMaxTopActors = 10

// New returns a Generator with sensible defaults and no sources wired.
func New() *Generator {
	return &Generator{nowFunc: time.Now, MaxTopActors: DefaultMaxTopActors}
}

// SetNowFunc overrides the clock for deterministic tests. Matches the
// oms.CachedRepository.nowFunc convention.
func (g *Generator) SetNowFunc(fn func() time.Time) {
	if fn != nil {
		g.nowFunc = fn
	}
}

// Generate builds a Report covering [from, to]. Zero from means "since
// the beginning of audit history"; zero to means "as of now". A source
// that returns an error aborts the whole generation and surfaces the
// underlying error — partial reports would silently under-count without
// signalling that a section is missing. Callers that want partial
// reports can wrap individual sources with their own log-and-return-zero
// adapter.
func (g *Generator) Generate(ctx context.Context, from, to time.Time) (*Report, error) {
	now := g.nowFunc().UTC()
	if to.IsZero() {
		to = now
	}
	report := &Report{
		GeneratedAt: now,
		WindowFrom:  from.UTC(),
		WindowTo:    to.UTC(),
		Access:      AccessStatistics{ByAction: []ActionCount{}, TopActors: []ActorCount{}},
		Markings:    MarkingDistribution{Markings: []MarkingSummary{}},
		Policies:    PolicyCoverage{},
	}

	if g.Audit != nil {
		evts, err := g.Audit.ListEvents(ctx, from, to)
		if err != nil {
			return nil, err
		}
		report.Access = summariseAccess(evts, g.MaxTopActors)
	}

	if g.Markings != nil {
		markings, err := g.Markings.ListMarkings(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]MarkingSummary, 0, len(markings))
		for _, m := range markings {
			cnt, cerr := g.Markings.CountGrants(ctx, m.Name)
			if cerr != nil {
				return nil, cerr
			}
			out = append(out, MarkingSummary{
				Name:        m.Name,
				DisplayName: m.DisplayName,
				Description: m.Description,
				Color:       m.Color,
				GrantCount:  cnt,
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		report.Markings = MarkingDistribution{Total: len(out), Markings: out}
	}

	if g.ObjectTypes != nil {
		n, err := g.ObjectTypes.CountObjectTypes(ctx)
		if err != nil {
			return nil, err
		}
		report.Policies.ObjectTypesTotal = n
	}

	if g.Policies != nil {
		rowTotal, rowOTs, err := g.Policies.RowPolicyStats(ctx)
		if err != nil {
			return nil, err
		}
		colTotal, colOTs, err := g.Policies.ColumnMaskStats(ctx)
		if err != nil {
			return nil, err
		}
		cellTotal, cellOTs, err := g.Policies.CellMaskStats(ctx)
		if err != nil {
			return nil, err
		}
		report.Policies.RowPolicies = PolicySurface{Total: rowTotal, CoveredObjectTypes: len(dedupe(rowOTs))}
		report.Policies.ColumnMasks = PolicySurface{Total: colTotal, CoveredObjectTypes: len(dedupe(colOTs))}
		report.Policies.CellMasks = PolicySurface{Total: cellTotal, CoveredObjectTypes: len(dedupe(cellOTs))}
		union := make(map[string]struct{})
		for _, s := range [][]string{rowOTs, colOTs, cellOTs} {
			for _, rid := range s {
				if rid == "" {
					continue
				}
				union[rid] = struct{}{}
			}
		}
		report.Policies.CoveredObjectTypes = len(union)
		if report.Policies.ObjectTypesTotal > 0 {
			report.Policies.CoverageRatio = float64(report.Policies.CoveredObjectTypes) / float64(report.Policies.ObjectTypesTotal)
		}
	}

	return report, nil
}

// summariseAccess computes the per-action + per-actor breakdown of a
// pre-filtered event slice. maxActors caps the TopActors output; <= 0
// means no cap.
func summariseAccess(events []audit.AuditEvent, maxActors int) AccessStatistics {
	byAction := make(map[string]int, 8)
	byActor := make(map[string]int, 8)
	for _, e := range events {
		if e.Action != "" {
			byAction[e.Action]++
		}
		if e.ActorID != "" {
			byActor[e.ActorID]++
		}
	}

	actions := make([]ActionCount, 0, len(byAction))
	for a, c := range byAction {
		actions = append(actions, ActionCount{Action: a, Count: c})
	}
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Count != actions[j].Count {
			return actions[i].Count > actions[j].Count
		}
		return actions[i].Action < actions[j].Action
	})

	actors := make([]ActorCount, 0, len(byActor))
	for a, c := range byActor {
		actors = append(actors, ActorCount{ActorID: a, Count: c})
	}
	sort.Slice(actors, func(i, j int) bool {
		if actors[i].Count != actors[j].Count {
			return actors[i].Count > actors[j].Count
		}
		return actors[i].ActorID < actors[j].ActorID
	})
	if maxActors > 0 && len(actors) > maxActors {
		actors = actors[:maxActors]
	}

	return AccessStatistics{
		Total:        len(events),
		UniqueActors: len(byActor),
		ByAction:     actions,
		TopActors:    actors,
	}
}

// dedupe returns xs with duplicates removed, preserving first-seen order.
// Empty string is treated as a non-value and dropped.
func dedupe(xs []string) []string {
	seen := make(map[string]struct{}, len(xs))
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if x == "" {
			continue
		}
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}
