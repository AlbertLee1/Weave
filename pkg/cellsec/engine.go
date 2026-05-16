package cellsec

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cellsec/celmask"
	"github.com/liyang/weave/pkg/masking"
)

// compiledCellMask wraps a CellMask with the optional pre-compiled CEL
// program (US-376). The program is non-nil iff the mask carries a non-empty
// Expression that compiled cleanly during Reload. Authoring errors surface
// in compileErr so /api/admin/cell-masks consumers can introspect bad rows
// without breaking enforcement of the well-formed siblings.
type compiledCellMask struct {
	mask       *CellMask
	program    *celmask.Program
	compileErr error
}

// Engine indexes CellMask rows by (ObjectType RID, primary key) so a query-
// time lookup for a specific row yields its applicable cell-level transforms
// in O(1). Policies are kept in memory for the lifetime of the process —
// call Reload after any write to refresh the cache. Safe for concurrent use.
type Engine struct {
	store       Store
	groupLookup GroupMembershipLookup

	mu      sync.RWMutex
	byOTKey map[string]map[string][]*compiledCellMask // otRID → primaryKey → masks
}

// New returns an Engine with an empty cache. Call Reload before relying on
// Compile. A nil groupLookup disables Group-scoped AppliesTo matching; the
// engine behaves as if the caller is in no groups.
func New(store Store, gl GroupMembershipLookup) *Engine {
	return &Engine{
		store:       store,
		groupLookup: gl,
		byOTKey:     make(map[string]map[string][]*compiledCellMask),
	}
}

// Reload pulls the current set of masks from the store and replaces the
// in-memory index. A store failure aborts the reload so a transient DB
// hiccup does not wipe enforcement. Per-row CEL programs (US-376) are
// compiled here so the per-request hot path stays a flat Eval.
func (e *Engine) Reload(ctx context.Context) error {
	if e == nil || e.store == nil {
		return nil
	}
	rows, err := e.store.List(ctx)
	if err != nil {
		return fmt.Errorf("cellsec.Reload: %w", err)
	}
	index := make(map[string]map[string][]*compiledCellMask, len(rows))
	for _, m := range rows {
		entry := buildCompiled(m)
		pkIndex, ok := index[m.ObjectTypeRID]
		if !ok {
			pkIndex = make(map[string][]*compiledCellMask)
			index[m.ObjectTypeRID] = pkIndex
		}
		pkIndex[m.PrimaryKey] = append(pkIndex[m.PrimaryKey], entry)
	}
	e.mu.Lock()
	e.byOTKey = index
	e.mu.Unlock()
	return nil
}

func buildCompiled(m *CellMask) *compiledCellMask {
	entry := &compiledCellMask{mask: m}
	if m == nil || m.Expression == "" {
		return entry
	}
	prg, err := celmask.Compile(m.Expression)
	if err != nil {
		entry.compileErr = err
		return entry
	}
	entry.program = prg
	return entry
}

// Compile resolves the cell-mask transforms that SHOULD be applied to the
// caller for the cell located at (objectTypeRID, primaryKey).
//
// This is the legacy US-258 entry point; it ignores Expression-bearing
// masks (those need a row binding — call CompileForRow). It returns the
// MaskRule shape so existing callers keep working unchanged.
//
// Semantics:
//   - nil user                     → nil (no masks)
//   - admin (PermUserManage)       → nil (bypass)
//   - no masks on (OT, PK)         → nil
//   - masks exist, all allow caller → empty map
//   - masks exist, some apply      → map[propertyApiName]MaskRule
//
// When multiple masks target the same property on the same cell, the LAST
// mask in iteration order wins; admins should author one mask per (cell,
// property) tuple.
func (e *Engine) Compile(ctx context.Context, user *auth.User, objectTypeRID, primaryKey string) (map[string]masking.MaskRule, error) {
	strategies, err := e.compileInternal(ctx, user, objectTypeRID, primaryKey, nil, false)
	if err != nil {
		return nil, err
	}
	if strategies == nil {
		return nil, nil
	}
	out := make(map[string]masking.MaskRule, len(strategies))
	for k, s := range strategies {
		if rule := masking.RuleFromStrategy(s); rule != "" {
			out[k] = rule
		}
	}
	return out, nil
}

// CompileForRow is the US-376 entry point that evaluates CEL Expression
// masks against the caller AND the row's properties. Strategy masks are
// returned in the canonical MaskStrategy taxonomy (REDACT|HASH|NULL|PARTIAL)
// so the caller can dispatch through masking.ApplyStrategyTransforms.
//
// row may be nil; CEL programs that reference row.<field> evaluate against
// an empty map. AppliesTo-only masks (no Expression) are still evaluated so
// the new and legacy paths can coexist on the same (OT, PK).
//
// When multiple masks target the same property on the same cell and both
// fire, the LAST mask in store iteration order wins. Authors should keep
// one (cell, property) → one mask to avoid surprises.
func (e *Engine) CompileForRow(ctx context.Context, user *auth.User, objectTypeRID, primaryKey string, row map[string]any) (map[string]masking.MaskStrategy, error) {
	return e.compileInternal(ctx, user, objectTypeRID, primaryKey, row, true)
}

// compileInternal is the shared core. includeExpression toggles whether
// Expression-bearing masks are evaluated; Compile (legacy) passes false to
// preserve backwards compatibility with US-258 callers that have no row.
func (e *Engine) compileInternal(ctx context.Context, user *auth.User, objectTypeRID, primaryKey string, row map[string]any, includeExpression bool) (map[string]masking.MaskStrategy, error) {
	if e == nil || user == nil {
		return nil, nil
	}
	if auth.HasPermission(user.Roles, auth.PermUserManage) {
		return nil, nil
	}

	e.mu.RLock()
	pkIndex, ok := e.byOTKey[objectTypeRID]
	var entries []*compiledCellMask
	if ok {
		entries = pkIndex[primaryKey]
	}
	e.mu.RUnlock()
	if len(entries) == 0 {
		return nil, nil
	}

	var userGroups []string
	if e.groupLookup != nil {
		g, err := e.groupLookup.UserGroups(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("cellsec: group lookup: %w", err)
		}
		userGroups = g
	}

	view := userViewFromAuth(user)

	out := make(map[string]masking.MaskStrategy)
	for _, entry := range entries {
		m := entry.mask
		if m == nil {
			continue
		}
		switch {
		case m.Expression != "":
			if !includeExpression {
				continue
			}
			fire, err := evaluateProgram(entry, view, row, rowMarkings(row))
			if err != nil {
				// Fail closed: a broken expression masks the cell rather
				// than silently leaking the clear value.
				out[m.PropertyAPIName] = m.EffectiveStrategy()
				continue
			}
			if fire {
				out[m.PropertyAPIName] = m.EffectiveStrategy()
			}
		default:
			if hasAllowList(m.AppliesTo) && m.AppliesTo.IsApplicable(user, userGroups) {
				continue
			}
			out[m.PropertyAPIName] = m.EffectiveStrategy()
		}
	}
	return out, nil
}

// evaluateProgram runs the compiled CEL program. A nil program (compile
// failed at Reload) is treated as "fail closed" so a malformed expression
// never opens a hole in enforcement. marking is the cell/row's
// classification markings (US-488); pass nil for an empty list.
func evaluateProgram(entry *compiledCellMask, view celmask.UserView, row map[string]any, marking []string) (bool, error) {
	if entry == nil {
		return false, errors.New("cellsec: nil compiled entry")
	}
	if entry.program == nil {
		if entry.compileErr != nil {
			return false, entry.compileErr
		}
		return false, errors.New("cellsec: missing compiled program")
	}
	return entry.program.EvalWithMarking(view, row, marking)
}

// rowMarkings extracts the row's classification markings from the reserved
// auth.MarkingsField key (`__markings`). Tolerates the same raw shapes the
// upstream writer paths emit — []string, []any, scalar string — and returns
// an empty slice for any other shape (including nil). The result is the
// `marking` binding handed to CEL programs at evaluation time.
func rowMarkings(row map[string]any) []string {
	if len(row) == 0 {
		return nil
	}
	raw, ok := row[auth.MarkingsField]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

// userViewFromAuth bridges auth.User into the celmask binding shape. The
// markings list goes through the canonical normaliser so admins can author
// expressions like '"PII" in user.markings' regardless of whether the
// upstream JWT carried []string, []any, or a scalar.
func userViewFromAuth(u *auth.User) celmask.UserView {
	if u == nil {
		return celmask.UserView{}
	}
	markings := normaliseMarkings(u.Attributes)
	return celmask.UserView{
		ID:         u.ID,
		Email:      u.Email,
		Roles:      u.Roles,
		Markings:   markings,
		Attributes: u.Attributes,
	}
}

func normaliseMarkings(attrs map[string]any) []string {
	if len(attrs) == 0 {
		return nil
	}
	raw, ok := attrs[auth.MarkingsAttributeKey]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

// Size returns the number of masks cached for an ObjectType RID. Useful for
// handler tests (verifying post-write Reload) and health probes.
func (e *Engine) Size(objectTypeRID string) int {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	total := 0
	for _, masks := range e.byOTKey[objectTypeRID] {
		total += len(masks)
	}
	return total
}

// SetMasks replaces the cached masks for a single (ObjectType RID, primaryKey)
// pair. Used by tests and the admin handler's fast-path refresh. Passing an
// empty slice drops the entry. CEL programs are compiled in this path so
// tests do not need to round-trip through Reload to exercise expression
// behaviour.
func (e *Engine) SetMasks(objectTypeRID, primaryKey string, masks []*CellMask) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	pkIndex, ok := e.byOTKey[objectTypeRID]
	if len(masks) == 0 {
		if ok {
			delete(pkIndex, primaryKey)
			if len(pkIndex) == 0 {
				delete(e.byOTKey, objectTypeRID)
			}
		}
		return
	}
	if !ok {
		pkIndex = make(map[string][]*compiledCellMask)
		e.byOTKey[objectTypeRID] = pkIndex
	}
	copied := make([]*compiledCellMask, len(masks))
	for i, m := range masks {
		cp := *m
		copied[i] = buildCompiled(&cp)
	}
	pkIndex[primaryKey] = copied
}

func hasAllowList(a masking.AppliesTo) bool {
	return len(a.Roles) > 0 || len(a.Groups) > 0 || len(a.Users) > 0
}
