package cellsec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/internal/testprofile"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/masking"
)

// US-376 Cell-Level Masking CEL Expression Engine — acceptance suite.
//
// Covers:
//   - 10+ named expressions exercising user.markings / user.roles /
//     user.email / row.<property> / numeric / compound clauses
//   - 4 mask strategies (REDACT / HASH / NULL / PARTIAL) end-to-end
//   - Negative case: invalid expressions rejected by CellMask.Validate
//   - Per-row perf gate: 1000 rows × 10 columns < 50 ms (PRD)

func mkCELMask(rid, otRID, pk, prop string, strategy masking.MaskStrategy, expr string) *CellMask {
	return &CellMask{
		RID:             rid,
		ObjectTypeRID:   otRID,
		PrimaryKey:      pk,
		PropertyAPIName: prop,
		MaskStrategy:    strategy,
		Expression:      expr,
	}
}

func userWithMarkings(id string, roles []string, markings ...string) *auth.User {
	attrs := map[string]any{}
	if len(markings) > 0 {
		attrs[auth.MarkingsAttributeKey] = markings
	}
	return &auth.User{ID: id, Roles: roles, Attributes: attrs}
}

func TestUS376_CompileForRow_PRDExample_PIIOrCountry(t *testing.T) {
	otRID := "ri.ontology.main.object-type.Customer"
	store := NewMemoryStore()
	mask := mkCELMask("m1", otRID, "c-100", "ssn", masking.MaskStrategyHash,
		`("PII" in user.markings) || row.country == "CN"`)
	if err := store.Create(context.Background(), mask); err != nil {
		t.Fatalf("seed: %v", err)
	}
	engine := New(store, nil)
	if err := engine.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	cases := []struct {
		name     string
		user     *auth.User
		row      map[string]any
		expected map[string]masking.MaskStrategy
	}{
		{
			"PII marking triggers mask",
			userWithMarkings("u:alice", []string{"viewer"}, "PII"),
			map[string]any{"country": "US", "ssn": "555-12-3456"},
			map[string]masking.MaskStrategy{"ssn": masking.MaskStrategyHash},
		},
		{
			"CN row triggers mask",
			userWithMarkings("u:bob", []string{"viewer"}),
			map[string]any{"country": "CN", "ssn": "999-99-9999"},
			map[string]masking.MaskStrategy{"ssn": masking.MaskStrategyHash},
		},
		{
			"neither condition fires",
			userWithMarkings("u:carol", []string{"viewer"}, "PUBLIC"),
			map[string]any{"country": "DE", "ssn": "111-22-3333"},
			map[string]masking.MaskStrategy{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.CompileForRow(context.Background(), tc.user, otRID, "c-100", tc.row)
			if err != nil {
				t.Fatalf("CompileForRow: %v", err)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %d transforms, got %d (%v)", len(tc.expected), len(got), got)
			}
			for k, v := range tc.expected {
				if got[k] != v {
					t.Fatalf("transforms[%q]: expected %q got %q", k, v, got[k])
				}
			}
		})
	}
}

func TestUS376_AdminBypassesCELMasks(t *testing.T) {
	otRID := "ri.ontology.main.object-type.X"
	store := NewMemoryStore()
	_ = store.Create(context.Background(), mkCELMask("m", otRID, "k", "p", masking.MaskStrategyRedact, `true`))
	engine := New(store, nil)
	_ = engine.Reload(context.Background())

	admin := &auth.User{ID: "u:root", Roles: []string{auth.RoleAdmin}}
	got, err := engine.CompileForRow(context.Background(), admin, otRID, "k", map[string]any{"p": "secret"})
	if err != nil {
		t.Fatalf("CompileForRow: %v", err)
	}
	if got != nil {
		t.Fatalf("expected admin bypass to return nil, got %v", got)
	}
}

func TestUS376_AllFourStrategiesAppliedEndToEnd(t *testing.T) {
	otRID := "ri.ontology.main.object-type.Customer"
	store := NewMemoryStore()

	cases := []struct {
		strategy masking.MaskStrategy
		input    interface{}
		check    func(t *testing.T, value interface{})
	}{
		{
			masking.MaskStrategyRedact,
			"value",
			func(t *testing.T, v interface{}) {
				// US-433: REDACT emits literal "***".
				if v != "***" {
					t.Fatalf("REDACT: expected ***, got %v", v)
				}
			},
		},
		{
			masking.MaskStrategyHash,
			"sensitive",
			func(t *testing.T, v interface{}) {
				s, ok := v.(string)
				if !ok || !strings.HasPrefix(s, "sha256:") {
					t.Fatalf("HASH: expected sha256: prefix, got %v", v)
				}
				if len(s) != len("sha256:")+64 {
					t.Fatalf("HASH: expected 64 hex chars, got %d", len(s)-len("sha256:"))
				}
			},
		},
		{
			masking.MaskStrategyNull,
			"non-nil",
			func(t *testing.T, v interface{}) {
				if v != nil {
					t.Fatalf("NULL: expected nil, got %v", v)
				}
			},
		},
		{
			masking.MaskStrategyPartial,
			"abcdefgh",
			func(t *testing.T, v interface{}) {
				// US-433: PARTIAL keeps the first AND last TWO characters.
				if v != "ab****gh" {
					t.Fatalf("PARTIAL: expected ab****gh, got %v", v)
				}
			},
		},
	}

	for i, tc := range cases {
		t.Run(string(tc.strategy), func(t *testing.T) {
			pk := "row-" + string(tc.strategy)
			mask := mkCELMask("m"+string(tc.strategy), otRID, pk, "field", tc.strategy, `true`)
			mask.RID = "m-" + string(tc.strategy) + "-" + time.Now().Format("150405.000000")
			if err := store.Create(context.Background(), mask); err != nil {
				t.Fatalf("seed: %v", err)
			}
			engine := New(store, nil)
			_ = engine.Reload(context.Background())

			user := userWithMarkings("u:alice", []string{"viewer"})
			row := map[string]interface{}{"field": tc.input}
			transforms, err := engine.CompileForRow(context.Background(), user, otRID, pk, row)
			if err != nil {
				t.Fatalf("CompileForRow[%d]: %v", i, err)
			}
			masking.ApplyStrategyTransforms(row, transforms)
			tc.check(t, row["field"])
		})
	}
}

func TestUS376_LegacyAppliesToPathStillWorks(t *testing.T) {
	otRID := "ri.ontology.main.object-type.Customer"
	store := NewMemoryStore()
	legacy := &CellMask{
		RID: "legacy", ObjectTypeRID: otRID, PrimaryKey: "c-100",
		PropertyAPIName: "ssn",
		MaskRule:        masking.MaskRuleHash,
		AppliesTo:       masking.AppliesTo{Roles: []string{"finance"}},
	}
	if err := store.Create(context.Background(), legacy); err != nil {
		t.Fatalf("seed: %v", err)
	}
	engine := New(store, nil)
	_ = engine.Reload(context.Background())

	t.Run("non-finance user gets the mask", func(t *testing.T) {
		viewer := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
		got, err := engine.CompileForRow(context.Background(), viewer, otRID, "c-100", nil)
		if err != nil {
			t.Fatalf("CompileForRow: %v", err)
		}
		if got["ssn"] != masking.MaskStrategyHash {
			t.Fatalf("expected HASH for viewer, got %v", got)
		}
	})
	t.Run("finance user is on the allow list", func(t *testing.T) {
		fin := &auth.User{ID: "u:fin", Roles: []string{"finance"}}
		got, err := engine.CompileForRow(context.Background(), fin, otRID, "c-100", nil)
		if err != nil {
			t.Fatalf("CompileForRow: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected no transforms for finance, got %v", got)
		}
	})
	t.Run("legacy Compile still returns MaskRule shape", func(t *testing.T) {
		viewer := &auth.User{ID: "u:alice", Roles: []string{"viewer"}}
		got, err := engine.Compile(context.Background(), viewer, otRID, "c-100")
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if got["ssn"] != masking.MaskRuleHash {
			t.Fatalf("expected hash MaskRule, got %v", got)
		}
	})
}

func TestUS376_InvalidExpression_RejectedAtValidate(t *testing.T) {
	bad := []string{
		`row.country ===`,
		`user.markings has 'PII'`, // unsupported "has" infix
		`unknown.symbol == 1`,
	}
	for _, src := range bad {
		t.Run(src, func(t *testing.T) {
			m := &CellMask{
				ObjectTypeRID:   "ot",
				PrimaryKey:      "k",
				PropertyAPIName: "p",
				MaskStrategy:    masking.MaskStrategyRedact,
				Expression:      src,
			}
			if err := m.Validate(); err == nil {
				t.Fatalf("expected Validate to reject %q", src)
			}
		})
	}
}

func TestUS376_InvalidStrategy_RejectedAtValidate(t *testing.T) {
	m := &CellMask{
		ObjectTypeRID:   "ot",
		PrimaryKey:      "k",
		PropertyAPIName: "p",
		MaskStrategy:    masking.MaskStrategy("ENCRYPT"),
	}
	if err := m.Validate(); err == nil {
		t.Fatalf("expected Validate to reject unknown strategy")
	}
}

func TestUS376_StrategyNormalizedToUppercase(t *testing.T) {
	m := &CellMask{
		ObjectTypeRID:   "ot",
		PrimaryKey:      "k",
		PropertyAPIName: "p",
		MaskStrategy:    masking.MaskStrategy("redact"),
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if m.MaskStrategy != masking.MaskStrategyRedact {
		t.Fatalf("expected canonical REDACT, got %q", m.MaskStrategy)
	}
}

func TestUS376_BrokenExpressionFailsClosed(t *testing.T) {
	otRID := "ot"
	store := NewMemoryStore()
	// Bypass Validate by using SetMasks directly with a mask whose
	// expression compiles but raises at runtime (missing field on a strict
	// row). The engine should fail closed and apply the mask anyway.
	engine := New(store, nil)
	mask := mkCELMask("m", otRID, "k", "p", masking.MaskStrategyRedact,
		`row.required_but_missing == "x"`)
	engine.SetMasks(otRID, "k", []*CellMask{mask})

	viewer := &auth.User{ID: "u:viewer", Roles: []string{"viewer"}}
	got, err := engine.CompileForRow(context.Background(), viewer, otRID, "k", map[string]any{})
	if err != nil {
		t.Fatalf("CompileForRow: %v", err)
	}
	if got["p"] != masking.MaskStrategyRedact {
		t.Fatalf("expected fail-closed REDACT, got %v", got)
	}
}

func TestUS376_PerfGate_1000Rows_10Columns_Under50ms(t *testing.T) {
	if testing.Short() {
		t.Skip("perf gate skipped under -short")
	}
	otRID := "ri.ontology.main.object-type.Wide"
	const cols = 10
	const rows = 1000

	store := NewMemoryStore()
	user := userWithMarkings("u:alice", []string{"viewer"}, "INTERNAL")
	colNames := make([]string, cols)
	for c := 0; c < cols; c++ {
		colNames[c] = "col_" + string(rune('a'+c))
	}
	// Mix of 5 distinct expressions across the 10 columns; each row gets a
	// CellMask per column. Different expressions to exercise CEL's runtime
	// dispatch (not just one cached evaluation).
	exprs := []string{
		`("PII" in user.markings) || row.region == "CN"`,
		`!("admin" in user.roles) && row.amount > 1000`,
		`row.classification == "SECRET"`,
		`size(user.markings) > 0`,
		`user.email != "trusted@ex.com"`,
	}
	for r := 0; r < rows; r++ {
		pk := "pk-" + itoa(r)
		for c, name := range colNames {
			m := mkCELMask("m-"+itoa(r)+"-"+itoa(c), otRID, pk, name, masking.MaskStrategyRedact, exprs[c%len(exprs)])
			if err := store.Create(context.Background(), m); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
	}
	engine := New(store, nil)
	if err := engine.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	rowsData := make([]map[string]interface{}, rows)
	for r := 0; r < rows; r++ {
		row := make(map[string]interface{}, cols+3)
		row["region"] = "US"
		row["amount"] = 5000
		row["classification"] = "INTERNAL"
		for _, name := range colNames {
			row[name] = "value-" + name
		}
		rowsData[r] = row
	}

	start := time.Now()
	for r := 0; r < rows; r++ {
		pk := "pk-" + itoa(r)
		transforms, err := engine.CompileForRow(context.Background(), user, otRID, pk, rowsData[r])
		if err != nil {
			t.Fatalf("CompileForRow[%d]: %v", r, err)
		}
		masking.ApplyStrategyTransforms(rowsData[r], transforms)
	}
	elapsed := time.Since(start)
	budget := 50 * time.Millisecond
	if testprofile.Instrumented(testing.CoverMode()) {
		budget = 250 * time.Millisecond
	}
	if elapsed > budget {
		t.Fatalf("PRD perf gate failed: %d rows × %d cols took %v (>%v)", rows, cols, elapsed, budget)
	}
	t.Logf("PRD perf gate: 1000 rows × 10 cols in %v (budget %v)", elapsed, budget)
}

// itoa is a tiny base-10 conversion helper to avoid pulling strconv into the
// hot path of the perf gate (negligible difference but keeps allocations
// observable in the bench profile).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
