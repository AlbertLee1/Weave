package compliance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
)

type stubAudit struct {
	events []audit.AuditEvent
	err    error
	called bool
}

func (s *stubAudit) ListEvents(_ context.Context, _, _ time.Time) ([]audit.AuditEvent, error) {
	s.called = true
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

type stubMarkings struct {
	markings []MarkingInfo
	counts   map[string]int
	listErr  error
	countErr error
}

func (s *stubMarkings) ListMarkings(_ context.Context) ([]MarkingInfo, error) {
	return s.markings, s.listErr
}

func (s *stubMarkings) CountGrants(_ context.Context, name string) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.counts[name], nil
}

type stubObjectTypes struct {
	count int
	err   error
}

func (s *stubObjectTypes) CountObjectTypes(_ context.Context) (int, error) {
	return s.count, s.err
}

type stubPolicies struct {
	rowTotal, colTotal, cellTotal int
	rowOTs, colOTs, cellOTs       []string
	rowErr, colErr, cellErr       error
}

func (s *stubPolicies) RowPolicyStats(_ context.Context) (int, []string, error) {
	return s.rowTotal, s.rowOTs, s.rowErr
}

func (s *stubPolicies) ColumnMaskStats(_ context.Context) (int, []string, error) {
	return s.colTotal, s.colOTs, s.colErr
}

func (s *stubPolicies) CellMaskStats(_ context.Context) (int, []string, error) {
	return s.cellTotal, s.cellOTs, s.cellErr
}

func fixedNow() time.Time {
	return time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
}

func TestGenerate_EmptyGeneratorProducesEmptyReport(t *testing.T) {
	g := New()
	g.SetNowFunc(fixedNow)
	r, err := g.Generate(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.Access.Total != 0 || r.Access.UniqueActors != 0 {
		t.Errorf("expected zero access stats, got %+v", r.Access)
	}
	if r.Access.ByAction == nil || r.Access.TopActors == nil {
		t.Errorf("byAction/topActors must be non-nil slices, got nil")
	}
	if r.Markings.Markings == nil {
		t.Error("markings list must be non-nil")
	}
	if r.Policies.CoverageRatio != 0 {
		t.Errorf("expected 0 coverage ratio, got %f", r.Policies.CoverageRatio)
	}
	if !r.GeneratedAt.Equal(fixedNow()) {
		t.Errorf("GeneratedAt: expected %v, got %v", fixedNow(), r.GeneratedAt)
	}
	if !r.WindowTo.Equal(fixedNow()) {
		t.Errorf("WindowTo should default to now, got %v", r.WindowTo)
	}
}

func TestGenerate_AccessStatistics(t *testing.T) {
	g := New()
	g.SetNowFunc(fixedNow)
	g.Audit = &stubAudit{
		events: []audit.AuditEvent{
			{ActorID: "user:a", Action: "login", Timestamp: fixedNow()},
			{ActorID: "user:a", Action: "login", Timestamp: fixedNow()},
			{ActorID: "user:a", Action: "read", Timestamp: fixedNow()},
			{ActorID: "user:b", Action: "login", Timestamp: fixedNow()},
			{ActorID: "user:c", Action: "write", Timestamp: fixedNow()},
		},
	}
	r, err := g.Generate(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.Access.Total != 5 {
		t.Errorf("Total: want 5, got %d", r.Access.Total)
	}
	if r.Access.UniqueActors != 3 {
		t.Errorf("UniqueActors: want 3, got %d", r.Access.UniqueActors)
	}
	if len(r.Access.ByAction) != 3 {
		t.Fatalf("ByAction: want 3 rows, got %+v", r.Access.ByAction)
	}
	if r.Access.ByAction[0].Action != "login" || r.Access.ByAction[0].Count != 3 {
		t.Errorf("ByAction[0]: want login=3, got %+v", r.Access.ByAction[0])
	}
	if r.Access.TopActors[0].ActorID != "user:a" || r.Access.TopActors[0].Count != 3 {
		t.Errorf("TopActors[0]: want user:a=3, got %+v", r.Access.TopActors[0])
	}
}

func TestGenerate_TopActorsRespectsCap(t *testing.T) {
	g := New()
	g.MaxTopActors = 2
	evts := []audit.AuditEvent{}
	for i, count := range []int{5, 4, 3, 2, 1} {
		actor := string(rune('a' + i))
		for j := 0; j < count; j++ {
			evts = append(evts, audit.AuditEvent{ActorID: "user:" + actor, Action: "login"})
		}
	}
	g.Audit = &stubAudit{events: evts}
	r, err := g.Generate(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(r.Access.TopActors) != 2 {
		t.Errorf("TopActors should be capped at 2, got %d", len(r.Access.TopActors))
	}
	if r.Access.UniqueActors != 5 {
		t.Errorf("UniqueActors should still reflect the full set (5), got %d", r.Access.UniqueActors)
	}
}

func TestGenerate_MarkingDistributionSortedByName(t *testing.T) {
	g := New()
	g.Markings = &stubMarkings{
		markings: []MarkingInfo{
			{Name: "SECRET", DisplayName: "Secret"},
			{Name: "PUBLIC", DisplayName: "Public"},
			{Name: "PII", DisplayName: "PII"},
		},
		counts: map[string]int{"SECRET": 2, "PUBLIC": 50, "PII": 0},
	}
	r, err := g.Generate(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.Markings.Total != 3 {
		t.Errorf("Total: want 3, got %d", r.Markings.Total)
	}
	names := []string{}
	for _, m := range r.Markings.Markings {
		names = append(names, m.Name)
	}
	want := []string{"PII", "PUBLIC", "SECRET"}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("Markings[%d]: want %q, got %q", i, n, names[i])
		}
	}
	if r.Markings.Markings[0].GrantCount != 0 {
		t.Errorf("PII grant count: want 0, got %d", r.Markings.Markings[0].GrantCount)
	}
	if r.Markings.Markings[1].GrantCount != 50 {
		t.Errorf("PUBLIC grant count: want 50, got %d", r.Markings.Markings[1].GrantCount)
	}
}

func TestGenerate_PolicyCoverageRatio(t *testing.T) {
	g := New()
	g.ObjectTypes = &stubObjectTypes{count: 10}
	g.Policies = &stubPolicies{
		rowTotal: 3, rowOTs: []string{"ri.oms.main.ot.1", "ri.oms.main.ot.2"},
		colTotal: 2, colOTs: []string{"ri.oms.main.ot.2", "ri.oms.main.ot.3"},
		cellTotal: 1, cellOTs: []string{"ri.oms.main.ot.3"},
	}
	r, err := g.Generate(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.Policies.ObjectTypesTotal != 10 {
		t.Errorf("ObjectTypesTotal: want 10, got %d", r.Policies.ObjectTypesTotal)
	}
	if r.Policies.RowPolicies.Total != 3 || r.Policies.RowPolicies.CoveredObjectTypes != 2 {
		t.Errorf("RowPolicies: want 3/2, got %+v", r.Policies.RowPolicies)
	}
	if r.Policies.CoveredObjectTypes != 3 {
		t.Errorf("CoveredObjectTypes union: want 3 (ot.1,ot.2,ot.3), got %d", r.Policies.CoveredObjectTypes)
	}
	if r.Policies.CoverageRatio != 0.3 {
		t.Errorf("CoverageRatio: want 0.3, got %f", r.Policies.CoverageRatio)
	}
}

func TestGenerate_PolicyCoverageRatioSafeWithZeroDenominator(t *testing.T) {
	g := New()
	g.Policies = &stubPolicies{
		rowTotal: 1, rowOTs: []string{"ri.oms.main.ot.1"},
	}
	r, err := g.Generate(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.Policies.CoverageRatio != 0 {
		t.Errorf("CoverageRatio should clamp to 0 when denominator is 0, got %f", r.Policies.CoverageRatio)
	}
}

func TestGenerate_SourceErrorsBubble(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name string
		set  func(g *Generator)
	}{
		{"audit", func(g *Generator) { g.Audit = &stubAudit{err: boom} }},
		{"markings-list", func(g *Generator) {
			g.Markings = &stubMarkings{listErr: boom}
		}},
		{"markings-count", func(g *Generator) {
			g.Markings = &stubMarkings{
				markings: []MarkingInfo{{Name: "X"}},
				countErr: boom,
			}
		}},
		{"objecttypes", func(g *Generator) { g.ObjectTypes = &stubObjectTypes{err: boom} }},
		{"policies-row", func(g *Generator) { g.Policies = &stubPolicies{rowErr: boom} }},
		{"policies-col", func(g *Generator) { g.Policies = &stubPolicies{colErr: boom} }},
		{"policies-cell", func(g *Generator) { g.Policies = &stubPolicies{cellErr: boom} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := New()
			tc.set(g)
			_, err := g.Generate(context.Background(), time.Time{}, time.Time{})
			if !errors.Is(err, boom) {
				t.Errorf("%s: want boom, got %v", tc.name, err)
			}
		})
	}
}

func TestGenerate_WindowDefaultsAndPropagation(t *testing.T) {
	g := New()
	g.SetNowFunc(fixedNow)
	aud := &stubAudit{}
	g.Audit = aud
	from := fixedNow().Add(-24 * time.Hour)
	r, err := g.Generate(context.Background(), from, time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !aud.called {
		t.Error("audit source should have been queried")
	}
	if !r.WindowFrom.Equal(from.UTC()) {
		t.Errorf("WindowFrom: want %v, got %v", from, r.WindowFrom)
	}
	if !r.WindowTo.Equal(fixedNow()) {
		t.Errorf("WindowTo should default to now, got %v", r.WindowTo)
	}
}

func TestDedupePreservesFirstSeenOrderAndDropsEmpty(t *testing.T) {
	got := dedupe([]string{"a", "", "b", "a", "c", "b", ""})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("dedupe: want %v, got %v", want, got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("dedupe[%d]: want %q, got %q", i, w, got[i])
		}
	}
}
