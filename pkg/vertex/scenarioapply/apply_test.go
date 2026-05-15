package scenarioapply

import (
	"context"
	"errors"
	"testing"
)

// Stub reader returns a canned scenario + edits.
type stubReader struct {
	scenarios map[string]Scenario
	edits     map[string][]ScenarioEdit
}

func (s *stubReader) GetScenario(_ context.Context, rid string) (Scenario, error) {
	sc, ok := s.scenarios[rid]
	if !ok {
		return Scenario{}, ErrScenarioNotFound
	}
	return sc, nil
}

func (s *stubReader) ListEdits(_ context.Context, rid string) ([]ScenarioEdit, error) {
	return s.edits[rid], nil
}

// Stub writer records applied edits + can be configured to fail on the
// nth call to simulate a mid-apply conflict.
type stubWriter struct {
	applied      []ScenarioEdit
	failOnEdit   int // 0 = never; 1 = fail on first edit; etc.
	stateUpdates []string
	published    []string
}

func (s *stubWriter) ApplyEditToMain(_ context.Context, e ScenarioEdit) error {
	if s.failOnEdit > 0 && len(s.applied)+1 == s.failOnEdit {
		return errors.New("conflict on " + e.Op)
	}
	s.applied = append(s.applied, e)
	return nil
}

func (s *stubWriter) MarkScenarioApplied(_ context.Context, rid string) error {
	s.stateUpdates = append(s.stateUpdates, rid)
	return nil
}

func (s *stubWriter) PublishEditBatch(_ context.Context, rid string) error {
	s.published = append(s.published, rid)
	return nil
}

// Stub tx runner inlines the closure synchronously.
type stubTxRunner struct {
	rollbackOn error
}

func (s *stubTxRunner) RunTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := fn(ctx); err != nil {
		s.rollbackOn = err
		return err
	}
	return nil
}

func TestApply_Given_DraftScenarioWith2Edits_When_Apply_Then_AllApplied(t *testing.T) {
	r := &stubReader{
		scenarios: map[string]Scenario{
			"s1": {RID: "s1", Status: ScenarioStatusDraft},
		},
		edits: map[string][]ScenarioEdit{
			"s1": {{Op: "modifyProperty"}, {Op: "createObject"}},
		},
	}
	w := &stubWriter{}
	svc := NewService(r, w, &stubTxRunner{})
	if err := svc.Apply(context.Background(), "s1"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(w.applied) != 2 {
		t.Errorf("expected 2 applied, got %d", len(w.applied))
	}
	if len(w.stateUpdates) != 1 || w.stateUpdates[0] != "s1" {
		t.Errorf("expected scenario marked applied, got %v", w.stateUpdates)
	}
	if len(w.published) != 1 {
		t.Errorf("expected publish event")
	}
}

func TestApply_Given_AlreadyApplied_When_Apply_Then_ErrAlreadyApplied(t *testing.T) {
	r := &stubReader{
		scenarios: map[string]Scenario{
			"s1": {RID: "s1", Status: ScenarioStatusApplied},
		},
	}
	w := &stubWriter{}
	svc := NewService(r, w, &stubTxRunner{})
	err := svc.Apply(context.Background(), "s1")
	if !errors.Is(err, ErrAlreadyApplied) {
		t.Errorf("got %v, want ErrAlreadyApplied", err)
	}
	if len(w.applied) != 0 {
		t.Errorf("expected no edits applied")
	}
}

func TestApply_Given_ConflictMidApply_When_Apply_Then_Rollback(t *testing.T) {
	r := &stubReader{
		scenarios: map[string]Scenario{
			"s1": {RID: "s1", Status: ScenarioStatusDraft},
		},
		edits: map[string][]ScenarioEdit{
			"s1": {{Op: "edit1"}, {Op: "edit2"}, {Op: "edit3"}},
		},
	}
	w := &stubWriter{failOnEdit: 2}
	tx := &stubTxRunner{}
	svc := NewService(r, w, tx)
	err := svc.Apply(context.Background(), "s1")
	if err == nil {
		t.Fatal("expected error on conflict")
	}
	if tx.rollbackOn == nil {
		t.Errorf("expected tx rollback path")
	}
	if len(w.stateUpdates) != 0 {
		t.Errorf("scenario should not be marked applied on rollback")
	}
	if len(w.published) != 0 {
		t.Errorf("no publish on rollback")
	}
}

func TestApply_Given_UnknownScenario_When_Apply_Then_ErrNotFound(t *testing.T) {
	r := &stubReader{scenarios: map[string]Scenario{}}
	svc := NewService(r, &stubWriter{}, &stubTxRunner{})
	err := svc.Apply(context.Background(), "zzz")
	if !errors.Is(err, ErrScenarioNotFound) {
		t.Errorf("got %v, want ErrScenarioNotFound", err)
	}
}
