package quality

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func newDeterministicID() func() string {
	var n int
	return func() string {
		n++
		return "v" + strconv.Itoa(n)
	}
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestChecker_NotNull(t *testing.T) {
	c, err := NewChecker(CheckerOptions{
		Rules: []Rule{{Name: "name_present", Type: RuleNotNull, Field: "name"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{
		{"name": "Alice"}, // pass
		{"name": ""},      // fail (empty string)
		{},                // fail (absent)
		{"name": nil},     // fail (explicit nil)
		{"name": "Bob"},   // pass
	}
	vs, err := c.CheckRows(context.Background(), rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 3 {
		t.Fatalf("expected 3 violations, got %d (%+v)", len(vs), vs)
	}
	for _, v := range vs {
		if v.RuleType != RuleNotNull || v.Field != "name" {
			t.Errorf("violation has wrong shape: %+v", v)
		}
	}
}

func TestChecker_Range(t *testing.T) {
	c, err := NewChecker(CheckerOptions{
		Rules: []Rule{{Name: "amt", Type: RuleRange, Field: "amount", Min: ptr(0.0), Max: ptr(100.0)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{
		{"amount": 50},        // pass (int → 50.0)
		{"amount": 0},         // pass (boundary)
		{"amount": 100.0},     // pass (boundary)
		{"amount": -1},        // fail (below min)
		{"amount": 100.001},   // fail (above max)
		{"amount": "42.5"},    // pass (string-coerced)
		{"amount": "garbage"}, // fail (non-numeric)
		{"amount": nil},       // fail (null)
	}
	vs, err := c.CheckRows(context.Background(), rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 4 {
		t.Fatalf("expected 4 violations, got %d (%+v)", len(vs), vs)
	}
	if vs[0].RowIndex != 3 || vs[1].RowIndex != 4 || vs[2].RowIndex != 6 || vs[3].RowIndex != 7 {
		t.Errorf("unexpected row indexes: %+v", vs)
	}
}

func TestChecker_Range_MinOnly(t *testing.T) {
	c, err := NewChecker(CheckerOptions{
		Rules: []Rule{{Name: "age", Type: RuleRange, Field: "age", Min: ptr(0.0)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{{"age": 5}, {"age": -1}, {"age": 1e9}}
	vs, _ := c.CheckRows(context.Background(), rows)
	if len(vs) != 1 || vs[0].RowIndex != 1 {
		t.Fatalf("expected one violation at row 1, got %+v", vs)
	}
}

func TestChecker_Unique(t *testing.T) {
	c, err := NewChecker(CheckerOptions{
		Rules: []Rule{{Name: "uniq_email", Type: RuleUnique, Field: "email"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{
		{"email": "a@x.com"},
		{"email": "b@x.com"},
		{"email": "a@x.com"}, // dup → violation
		{"email": ""},        // skipped (null-ish)
		{},                   // skipped (absent)
		{"email": "b@x.com"}, // dup → violation
	}
	vs, err := c.CheckRows(context.Background(), rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("expected 2 violations, got %d (%+v)", len(vs), vs)
	}
	if vs[0].RowIndex != 2 || vs[1].RowIndex != 5 {
		t.Errorf("unexpected row indexes: %+v", vs)
	}
}

func TestChecker_Unique_Reset(t *testing.T) {
	c, err := NewChecker(CheckerOptions{
		Rules: []Rule{{Name: "u", Type: RuleUnique, Field: "k"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := c.CheckRows(context.Background(), []map[string]any{{"k": "x"}, {"k": "x"}})
	if len(first) != 1 {
		t.Fatalf("expected 1 violation in first pass, got %d", len(first))
	}
	c.Reset()
	second, _ := c.CheckRows(context.Background(), []map[string]any{{"k": "x"}, {"k": "x"}})
	if len(second) != 1 {
		t.Fatalf("expected 1 violation after Reset, got %d", len(second))
	}
}

func TestChecker_Regex(t *testing.T) {
	c, err := NewChecker(CheckerOptions{
		Rules: []Rule{{Name: "email", Type: RuleRegex, Field: "email", Pattern: `^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{
		{"email": "alice@example.com"}, // pass
		{"email": "no-at-symbol"},      // fail
		{"email": ""},                  // fail (null on regex)
		{"email": 42},                  // fail (non-string)
		{},                             // fail (absent)
	}
	vs, err := c.CheckRows(context.Background(), rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 4 {
		t.Fatalf("expected 4 violations, got %d (%+v)", len(vs), vs)
	}
}

func TestChecker_ForeignKey(t *testing.T) {
	known := map[string]struct{}{"u1": {}, "u2": {}}
	lookup := FKLookupFunc(func(_ context.Context, value any) (bool, error) {
		s, ok := value.(string)
		if !ok {
			return false, nil
		}
		_, found := known[s]
		return found, nil
	})
	c, err := NewChecker(CheckerOptions{
		Rules:     []Rule{{Name: "user_ref", Type: RuleForeignKey, Field: "user_id", Lookup: "users"}},
		FKLookups: map[string]FKLookup{"users": lookup},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{
		{"user_id": "u1"},      // pass
		{"user_id": "u_ghost"}, // fail
		{"user_id": ""},        // fail (null)
		{},                     // fail (absent)
	}
	vs, err := c.CheckRows(context.Background(), rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 3 {
		t.Fatalf("expected 3 violations, got %d (%+v)", len(vs), vs)
	}
	for _, v := range vs {
		if v.RuleType != RuleForeignKey {
			t.Errorf("rule type %q on FK violation", v.RuleType)
		}
	}
}

func TestChecker_ForeignKey_LookupErrorAbortsRow(t *testing.T) {
	boom := errors.New("transport boom")
	lookup := FKLookupFunc(func(context.Context, any) (bool, error) { return false, boom })
	c, err := NewChecker(CheckerOptions{
		Rules:     []Rule{{Name: "u", Type: RuleForeignKey, Field: "user_id", Lookup: "users"}},
		FKLookups: map[string]FKLookup{"users": lookup},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CheckRow(context.Background(), 0, "", map[string]any{"user_id": "u1"})
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected wrapped transport error, got %v", err)
	}
}

func TestNewChecker_ForeignKey_UnregisteredLookup(t *testing.T) {
	_, err := NewChecker(CheckerOptions{
		Rules: []Rule{{Name: "u", Type: RuleForeignKey, Field: "user_id", Lookup: "users"}},
	})
	if err == nil {
		t.Fatal("expected error for unregistered lookup")
	}
}

func TestChecker_AllRuleTypesTogether(t *testing.T) {
	now := time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC)
	lookup := FKLookupFunc(func(_ context.Context, v any) (bool, error) {
		return v == "u1", nil
	})
	c, err := NewChecker(CheckerOptions{
		PipelineID: "p1",
		RunID:      "r1",
		NodeName:   "validate",
		NowFunc:    fixedClock(now),
		IDFunc:     newDeterministicID(),
		FKLookups:  map[string]FKLookup{"users": lookup},
		Rules: []Rule{
			{Name: "name_present", Type: RuleNotNull, Field: "name"},
			{Name: "amt_range", Type: RuleRange, Field: "amount", Min: ptr(0.0), Max: ptr(100.0)},
			{Name: "uniq_email", Type: RuleUnique, Field: "email"},
			{Name: "email_format", Type: RuleRegex, Field: "email", Pattern: `^\S+@\S+\.\S+$`},
			{Name: "user_ref", Type: RuleForeignKey, Field: "user_id", Lookup: "users"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{
		{"name": "Alice", "amount": 10.0, "email": "alice@example.com", "user_id": "u1"},
		{"name": "", "amount": 200.0, "email": "garbage", "user_id": "ghost"},
		{"name": "Carol", "amount": 50.0, "email": "alice@example.com", "user_id": "u1"},
	}
	vs, err := c.CheckRows(context.Background(), rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 5 {
		t.Fatalf("expected 5 violations (notNull, range, regex, fk, unique), got %d (%+v)", len(vs), vs)
	}
	wantBy := map[string]int{
		"name_present": 1,
		"amt_range":    1,
		"email_format": 1,
		"user_ref":     1,
		"uniq_email":   1,
	}
	for _, v := range vs {
		wantBy[v.RuleName]--
		if v.PipelineID != "p1" || v.RunID != "r1" || v.NodeName != "validate" {
			t.Errorf("violation missing run-scoped metadata: %+v", v)
		}
		if v.DetectedAt != now {
			t.Errorf("DetectedAt = %v, want %v", v.DetectedAt, now)
		}
		if v.ID == "" {
			t.Error("violation id must be set")
		}
	}
	for name, remaining := range wantBy {
		if remaining != 0 {
			t.Errorf("rule %q: balance %d (negative=extra, positive=missing)", name, remaining)
		}
	}
}

func TestChecker_RowKeyAndID(t *testing.T) {
	c, err := NewChecker(CheckerOptions{
		Rules:   []Rule{{Name: "name", Type: RuleNotNull, Field: "name"}},
		IDFunc:  newDeterministicID(),
		NowFunc: fixedClock(time.Unix(1700000000, 0).UTC()),
	})
	if err != nil {
		t.Fatal(err)
	}
	vs, err := c.CheckRow(context.Background(), 7, "row-pk-7", map[string]any{"name": ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(vs))
	}
	v := vs[0]
	if v.ID != "v1" {
		t.Errorf("ID = %q, want v1", v.ID)
	}
	if v.RowIndex != 7 || v.RowKey != "row-pk-7" {
		t.Errorf("row identity wrong: idx=%d key=%q", v.RowIndex, v.RowKey)
	}
}

func TestChecker_NoRulesIsValid(t *testing.T) {
	c, err := NewChecker(CheckerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vs, err := c.CheckRows(context.Background(), []map[string]any{{"x": 1}, {"x": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("expected no violations with empty ruleset, got %d", len(vs))
	}
}

func TestChecker_RulesReturnsCopy(t *testing.T) {
	rules := []Rule{{Name: "n", Type: RuleNotNull, Field: "f"}}
	c, err := NewChecker(CheckerOptions{Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	out := c.Rules()
	out[0].Name = "mutated"
	if c.Rules()[0].Name != "n" {
		t.Error("Rules() did not return a copy")
	}
}

func TestNewChecker_RegexInvalidStillRejectedAtCompile(t *testing.T) {
	// Validate already rejects invalid patterns; this guards against
	// future skew between Validate's compile probe and the Checker's
	// real compile call.
	_, err := NewChecker(CheckerOptions{
		Rules: []Rule{{Name: "r", Type: RuleRegex, Field: "f", Pattern: "[broken"}},
	})
	if err == nil {
		t.Fatal("expected NewChecker to reject invalid regex pattern")
	}
}
