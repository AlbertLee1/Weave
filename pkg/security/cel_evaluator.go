// US-432: CEL-based row-level policy evaluator with two layered caches.
//
//   - Compile cache (per-rule): cel.Program is shared across every Evaluate
//     call that references the same (PolicyRID, Version) pair. Bumping the
//     version forces a recompile, dropping nothing else.
//   - Decision cache (per (user, row, ruleSet)): allow/deny verdicts are
//     memoised so the hot serialisation path can answer N row visibility
//     questions with N hash lookups instead of N×K CEL evaluations.
//
// The evaluator is intentionally free of OSS / OMS dependencies so it can be
// embedded by future row-level enforcement surfaces (Search post-filter,
// Aggregation row gate, Materialized snapshot reader) without dragging in
// the bleve query path that pkg/security.Engine compiles to.
package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"

	"github.com/liyang/weave/pkg/auth"
)

// CELRule is one row-level CEL clause.
//
// Expression returns bool: true means ALLOW the caller to see the row.
// AND-combination across N rules → row visible iff every rule returns true.
// PolicyRID identifies the source row in security_policies and is used as
// the compile-cache key alongside Version. Bumping Version on author change
// (mirroring Engine.SetPolicies' version bump) automatically retires the
// old compiled program and any dependent decision-cache entries.
type CELRule struct {
	PolicyRID  string
	Version    int64
	Expression string
}

// CELEvaluator holds the compile + decision caches. Safe for concurrent use.
type CELEvaluator struct {
	env *cel.Env

	programMu sync.RWMutex
	programs  map[programCacheKey]*celProgram

	decisionCache *DecisionCache

	compileHits   atomic.Uint64
	compileMisses atomic.Uint64
}

type programCacheKey struct {
	rid     string
	version int64
}

type celProgram struct {
	program cel.Program
	src     string
}

// NewCELEvaluator builds a fresh evaluator with an empty compile cache and
// no decision cache wired. Call SetDecisionCache to enable per-row
// memoisation.
func NewCELEvaluator() *CELEvaluator {
	env, err := cel.NewEnv(
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("row", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		panic(fmt.Sprintf("security: build CEL env: %v", err))
	}
	return &CELEvaluator{
		env:      env,
		programs: make(map[programCacheKey]*celProgram),
	}
}

// SetDecisionCache wires (or replaces) the per-row decision cache. Pass nil
// to disable decision memoisation; the per-rule compile cache stays active.
func (e *CELEvaluator) SetDecisionCache(c *DecisionCache) {
	e.decisionCache = c
}

// DecisionCache returns the wired cache, or nil if none. Mostly useful for
// tests that want to assert cache stats after Evaluate.
func (e *CELEvaluator) DecisionCache() *DecisionCache { return e.decisionCache }

// Compile parses + type-checks the rule and stores the resulting program in
// the compile cache. Calling Compile is optional: Evaluate compiles lazily
// the first time it sees a (PolicyRID, Version) pair.
func (e *CELEvaluator) Compile(rule CELRule) error {
	_, err := e.compileRule(rule)
	return err
}

// CompileStats returns cumulative hit/miss counters for the program cache.
// Stats are atomic-counter reads; safe to call concurrently with Evaluate.
func (e *CELEvaluator) CompileStats() (hits, misses uint64) {
	return e.compileHits.Load(), e.compileMisses.Load()
}

// CompileSize returns the number of cached programs.
func (e *CELEvaluator) CompileSize() int {
	e.programMu.RLock()
	defer e.programMu.RUnlock()
	return len(e.programs)
}

// InvalidatePolicy drops every cached program for the given PolicyRID
// (across all versions) so the next Evaluate recompiles.
func (e *CELEvaluator) InvalidatePolicy(policyRID string) {
	e.programMu.Lock()
	for k := range e.programs {
		if k.rid == policyRID {
			delete(e.programs, k)
		}
	}
	e.programMu.Unlock()
}

func (e *CELEvaluator) compileRule(rule CELRule) (*celProgram, error) {
	src := strings.TrimSpace(rule.Expression)
	if src == "" {
		return nil, errors.New("security: empty CEL expression")
	}
	key := programCacheKey{rid: rule.PolicyRID, version: rule.Version}

	e.programMu.RLock()
	if cached, ok := e.programs[key]; ok && cached.src == src {
		e.programMu.RUnlock()
		e.compileHits.Add(1)
		return cached, nil
	}
	e.programMu.RUnlock()

	ast, iss := e.env.Compile(src)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("security: compile %q: %w", src, iss.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("security: expression %q must return bool, got %s", src, ast.OutputType().String())
	}
	prg, err := e.env.Program(ast, cel.EvalOptions(cel.OptOptimize))
	if err != nil {
		return nil, fmt.Errorf("security: program %q: %w", src, err)
	}
	p := &celProgram{program: prg, src: src}

	e.programMu.Lock()
	if existing, ok := e.programs[key]; ok && existing.src == src {
		e.programMu.Unlock()
		e.compileHits.Add(1)
		return existing, nil
	}
	e.programs[key] = p
	e.programMu.Unlock()
	e.compileMisses.Add(1)
	return p, nil
}

// RuleSet is the precompiled bundle of CEL rules used on the hot path.
// Build once at policy load time via CELEvaluator.BuildRuleSet so the
// per-row Evaluate skips the rule-set signature recomputation and the
// per-rule compile-cache lookup. Two RuleSets that contain the same logical
// (PolicyRID, Version) pairs hash to the same Sig regardless of order, so
// the decision cache treats them as one cache entry.
type RuleSet struct {
	rules    []CELRule
	programs []*celProgram
	sig      uint64
}

// Sig returns the order-independent fingerprint of the underlying rules.
// Callers may persist Sig to a sidecar table if they want to track which
// policy bundles a cached decision was computed against.
func (s *RuleSet) Sig() uint64 {
	if s == nil {
		return 0
	}
	return s.sig
}

// Len returns the number of rules in the bundle.
func (s *RuleSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.rules)
}

// BuildRuleSet compiles every rule eagerly and computes the order-
// independent rule-set signature. Returned errors are wrapped CEL compile
// failures; callers should treat a non-nil error as fail-closed (the
// downstream evaluation surface should deny rows).
func (e *CELEvaluator) BuildRuleSet(rules []CELRule) (*RuleSet, error) {
	progs := make([]*celProgram, len(rules))
	for i, r := range rules {
		p, err := e.compileRule(r)
		if err != nil {
			return nil, err
		}
		progs[i] = p
	}
	return &RuleSet{
		rules:    rules,
		programs: progs,
		sig:      ruleSetSignature(rules),
	}, nil
}

// Evaluate runs every rule against (user, row) and returns the allow/deny
// verdict. AND-combination across rules: the row is visible iff every rule
// returns true. An empty rule set returns true (no policy → no restriction).
//
// rowKey is the caller-supplied stable identifier for the row state used as
// the decision-cache key. Use HashRowProperties for the canonical content
// hash; callers that already have a stable hash (object_history.row_hash,
// parquet __row_hash) should reuse it. Empty rowKey OR a nil decision
// cache disables memoisation for this call (the verdict is still computed
// but not stored).
//
// Fail-closed: any compile or eval error returns (false, err) and the
// false verdict is stored in the decision cache so a transient policy-side
// fault doesn't open a hole at the next request. Callers MUST treat the
// returned error as deny — propagating allow=false is the safe default.
//
// Hot-path callers should prefer EvaluateRuleSet to skip the per-call
// signature recomputation and program-cache lookup; this method is
// provided for callers that don't want to manage the RuleSet lifecycle.
func (e *CELEvaluator) Evaluate(ctx context.Context, user *auth.User, rules []CELRule, rowKey string, row map[string]any) (bool, error) {
	_ = ctx
	if len(rules) == 0 {
		return true, nil
	}
	set, err := e.BuildRuleSet(rules)
	if err != nil {
		if e.decisionCache != nil && rowKey != "" {
			var userID string
			if user != nil {
				userID = user.ID
			}
			e.decisionCache.Put(decisionKey(userID, rowKey, ruleSetSignature(rules)), false)
		}
		return false, err
	}
	return e.EvaluateRuleSet(ctx, user, set, rowKey, row)
}

// EvaluateRuleSet is the hot-path entry point. The precompiled RuleSet
// carries the per-rule cel.Program references and the order-independent
// signature so the per-call cost on a cache hit is one decisionKey hash +
// one DecisionCache.Get (no slice sort, no per-rule compile-cache lookup).
//
// The semantics — AND-combination, fail-closed on error, decision cache
// disabled when rowKey is empty — match Evaluate.
func (e *CELEvaluator) EvaluateRuleSet(ctx context.Context, user *auth.User, set *RuleSet, rowKey string, row map[string]any) (bool, error) {
	_ = ctx
	if set == nil || len(set.programs) == 0 {
		return true, nil
	}

	var userID string
	if user != nil {
		userID = user.ID
	}

	var key uint64
	cacheActive := e.decisionCache != nil && rowKey != ""
	if cacheActive {
		key = decisionKey(userID, rowKey, set.sig)
		if cached, ok := e.decisionCache.Get(key); ok {
			return cached, nil
		}
	}

	allow, err := e.evaluatePrograms(user, set.programs, row)
	if cacheActive {
		// Store the verdict (including fail-closed false) so a follow-up
		// for the same (user, row, ruleSet) doesn't repay the eval cost.
		e.decisionCache.Put(key, allow)
	}
	return allow, err
}

func (e *CELEvaluator) evaluatePrograms(user *auth.User, programs []*celProgram, row map[string]any) (bool, error) {
	bindings := userBindingsForCEL(user)
	rowBinding := row
	if rowBinding == nil {
		rowBinding = map[string]any{}
	}
	activation := map[string]any{
		"user": bindings,
		"row":  rowBinding,
	}
	for _, prg := range programs {
		out, _, err := prg.program.Eval(activation)
		if err != nil {
			return false, fmt.Errorf("security: eval %q: %w", prg.src, err)
		}
		b, ok := celBoolValue(out)
		if !ok {
			return false, fmt.Errorf("security: expression %q returned non-bool", prg.src)
		}
		if !b {
			return false, nil
		}
	}
	return true, nil
}

func celBoolValue(v any) (bool, bool) {
	if b, ok := v.(types.Bool); ok {
		return bool(b), true
	}
	if val, ok := v.(interface{ Value() any }); ok {
		if b, ok := val.Value().(bool); ok {
			return b, true
		}
	}
	if b, ok := v.(bool); ok {
		return b, true
	}
	return false, false
}

func userBindingsForCEL(u *auth.User) map[string]any {
	if u == nil {
		return map[string]any{
			"id":         "",
			"email":      "",
			"roles":      []string{},
			"markings":   []string{},
			"attributes": map[string]any{},
		}
	}
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	attrs := u.Attributes
	if attrs == nil {
		attrs = map[string]any{}
	}
	markings := normaliseMarkingsForCEL(attrs)
	if markings == nil {
		markings = []string{}
	}
	return map[string]any{
		"id":         u.ID,
		"email":      u.Email,
		"roles":      roles,
		"markings":   markings,
		"attributes": attrs,
	}
}

func normaliseMarkingsForCEL(attrs map[string]any) []string {
	raw, ok := attrs[userMarkingsKey]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
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

// ruleSetSignature returns a stable 64-bit fingerprint of the rule set's
// (RID, Version) pairs. Order-independent: two callers that pass the same
// logical rule set hash to the same value, so the decision cache treats
// them as one cache entry regardless of slice order.
func ruleSetSignature(rules []CELRule) uint64 {
	if len(rules) == 0 {
		return 0
	}
	pairs := make([]string, len(rules))
	for i, r := range rules {
		pairs[i] = r.PolicyRID + "@" + strconv.FormatInt(r.Version, 10)
	}
	sort.Strings(pairs)
	h := fnv.New64a()
	for _, p := range pairs {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// decisionKey hashes (userID, rowKey, ruleSetSig) into a single uint64
// suitable for the DecisionCache.
func decisionKey(userID, rowKey string, sig uint64) uint64 {
	h := fnv64aOffset
	h = fnv64aString(h, userID)
	h = fnv64aByte(h, 0)
	h = fnv64aString(h, rowKey)
	h = fnv64aByte(h, 0)
	for shift := 56; shift >= 0; shift -= 8 {
		h = fnv64aByte(h, byte(sig>>shift))
	}
	return h
}

const (
	fnv64aOffset uint64 = 14695981039346656037
	fnv64aPrime  uint64 = 1099511628211
)

func fnv64aString(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h = fnv64aByte(h, s[i])
	}
	return h
}

func fnv64aByte(h uint64, b byte) uint64 {
	h ^= uint64(b)
	h *= fnv64aPrime
	return h
}

// HashRowProperties returns a stable rowKey suitable for the decision
// cache, computed by sorting keys, JSON-encoding values, and FNV1a-hashing
// the canonical bytes. Returned as a hex-encoded uint64 so it round-trips
// through HTTP / log lines without escaping. Callers that already hold a
// stable row hash (object_history.row_hash, parquet __row_hash) should
// prefer that over recomputing here.
func HashRowProperties(row map[string]any) string {
	if len(row) == 0 {
		return "0"
	}
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := fnv.New64a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		b, _ := json.Marshal(row[k])
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	return strconv.FormatUint(h.Sum64(), 16)
}
