package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// stubEditOnlyRepo is a minimal in-memory editOnlyResolverRepo for unit
// tests. It mirrors the shape of stubLinkPropagationRepo so the test stays
// portable: callers configure ontologies → object types → properties via
// plain maps and can inject errors on any of the three list methods.
type stubEditOnlyRepo struct {
	ontologies     []oms.Ontology
	objectTypes    map[string][]oms.ObjectType // ontologyRID -> OTs
	properties     map[string][]oms.Property   // objectTypeRID -> props
	listOntsErr    error
	listOTsErr     error
	listPropsErr   error
	listOTsCalls   atomic.Int32
	listPropsCalls atomic.Int32
	listOntCalls   atomic.Int32
}

func (s *stubEditOnlyRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	s.listOntCalls.Add(1)
	if s.listOntsErr != nil {
		return nil, s.listOntsErr
	}
	return s.ontologies, nil
}

func (s *stubEditOnlyRepo) ListObjectTypes(_ context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	s.listOTsCalls.Add(1)
	if s.listOTsErr != nil {
		return nil, s.listOTsErr
	}
	return s.objectTypes[ontologyRID], nil
}

func (s *stubEditOnlyRepo) ListProperties(_ context.Context, objectTypeRID string) ([]oms.Property, error) {
	s.listPropsCalls.Add(1)
	if s.listPropsErr != nil {
		return nil, s.listPropsErr
	}
	return s.properties[objectTypeRID], nil
}

func TestEditOnlyResolver_NilRepo_AlwaysFalse(t *testing.T) {
	r := newEditOnlyResolver(nil)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh on nil repo: %v", err)
	}
	if r.IsEditOnly("order", "notes") {
		t.Fatal("expected IsEditOnly=false from nil-repo resolver")
	}
}

func TestEditOnlyResolver_NilReceiver_AlwaysFalse(t *testing.T) {
	var r *editOnlyResolver
	if r.IsEditOnly("order", "notes") {
		t.Fatal("expected nil receiver to return false")
	}
}

func TestEditOnlyResolver_Refresh_PopulatesEditOnlyFieldsOnly(t *testing.T) {
	repo := &stubEditOnlyRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main", APIName: "main"}},
		objectTypes: map[string][]oms.ObjectType{
			"ri.ontology.main": {{RID: "ri.ot.order", APIName: "order"}},
		},
		properties: map[string][]oms.Property{
			"ri.ot.order": {
				{APIName: "orderID", IsEditOnly: false},
				{APIName: "status", IsEditOnly: false},
				{APIName: "notes", IsEditOnly: true},
			},
		},
	}
	r := newEditOnlyResolver(repo)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !r.IsEditOnly("order", "notes") {
		t.Fatal("expected IsEditOnly(order, notes)=true after refresh")
	}
	if r.IsEditOnly("order", "status") {
		t.Fatal("expected IsEditOnly(order, status)=false (not flagged)")
	}
	if r.IsEditOnly("order", "orderID") {
		t.Fatal("expected IsEditOnly(order, orderID)=false (not flagged)")
	}
}

func TestEditOnlyResolver_UnknownObjectTypeOrField_ReturnsFalse(t *testing.T) {
	repo := &stubEditOnlyRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main", APIName: "main"}},
		objectTypes: map[string][]oms.ObjectType{
			"ri.ontology.main": {{RID: "ri.ot.order", APIName: "order"}},
		},
		properties: map[string][]oms.Property{
			"ri.ot.order": {{APIName: "notes", IsEditOnly: true}},
		},
	}
	r := newEditOnlyResolver(repo)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if r.IsEditOnly("customer", "notes") {
		t.Fatal("expected unknown objectType to return false")
	}
	if r.IsEditOnly("order", "ship_address") {
		t.Fatal("expected unknown field to return false")
	}
}

func TestEditOnlyResolver_MultipleOntologies_SameAPINameORed(t *testing.T) {
	// Two ontologies (legal, hr) both define an `order` ObjectType. Only the
	// hr.order.notes property is flagged IsEditOnly=true; the production hook
	// signature drops the ontology dimension so the resolver must OR-aggregate
	// — over-protecting is the safer failure mode than silently leaking.
	repo := &stubEditOnlyRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.legal", APIName: "legal"},
			{RID: "ri.ontology.hr", APIName: "hr"},
		},
		objectTypes: map[string][]oms.ObjectType{
			"ri.ontology.legal": {{RID: "ri.ot.legal_order", APIName: "order"}},
			"ri.ontology.hr":    {{RID: "ri.ot.hr_order", APIName: "order"}},
		},
		properties: map[string][]oms.Property{
			"ri.ot.legal_order": {{APIName: "notes", IsEditOnly: false}},
			"ri.ot.hr_order":    {{APIName: "notes", IsEditOnly: true}},
		},
	}
	r := newEditOnlyResolver(repo)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !r.IsEditOnly("order", "notes") {
		t.Fatal("expected OR-aggregation across ontologies sharing OT APIName")
	}
}

func TestEditOnlyResolver_Refresh_PerOntologyErrorSkipsButContinues(t *testing.T) {
	// A failing ListObjectTypes for one ontology must not poison the rest.
	// We can't easily inject "fail only on ontology #1" without expanding the
	// stub; instead, verify the global error from ListOntologies bubbles up
	// while a per-OT property error degrades to a partial cache (the OT is
	// skipped, the IsEditOnly check returns false for it).
	repo := &stubEditOnlyRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main", APIName: "main"}},
		objectTypes: map[string][]oms.ObjectType{
			"ri.ontology.main": {{RID: "ri.ot.order", APIName: "order"}},
		},
		listPropsErr: errors.New("boom"),
	}
	r := newEditOnlyResolver(repo)
	// Refresh must NOT return an error for a per-ObjectType property failure
	// — partial cache is preferable to empty cache. The contract is that the
	// missing OT simply returns IsEditOnly=false until the next refresh.
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh should swallow per-OT errors: %v", err)
	}
	if r.IsEditOnly("order", "notes") {
		t.Fatal("expected IsEditOnly=false when ListProperties failed and cache is empty")
	}
}

func TestEditOnlyResolver_Refresh_ListOntologiesError_BubblesUp(t *testing.T) {
	repo := &stubEditOnlyRepo{listOntsErr: errors.New("pg down")}
	r := newEditOnlyResolver(repo)
	if err := r.Refresh(context.Background()); err == nil {
		t.Fatal("expected ListOntologies error to surface from Refresh")
	}
	if r.IsEditOnly("order", "notes") {
		t.Fatal("expected empty cache after failed Refresh")
	}
}

func TestEditOnlyResolver_Refresh_SwapsCacheAtomically(t *testing.T) {
	// First refresh: notes is editOnly. Second refresh: notes is no longer
	// editOnly (admin cleared the flag). The cache must reflect the latest
	// truth — no stale entries linger across refreshes.
	repo := &stubEditOnlyRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main", APIName: "main"}},
		objectTypes: map[string][]oms.ObjectType{
			"ri.ontology.main": {{RID: "ri.ot.order", APIName: "order"}},
		},
		properties: map[string][]oms.Property{
			"ri.ot.order": {{APIName: "notes", IsEditOnly: true}},
		},
	}
	r := newEditOnlyResolver(repo)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if !r.IsEditOnly("order", "notes") {
		t.Fatal("first refresh: expected notes editOnly")
	}
	repo.properties["ri.ot.order"] = []oms.Property{{APIName: "notes", IsEditOnly: false}}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if r.IsEditOnly("order", "notes") {
		t.Fatal("second refresh: expected notes no longer editOnly after admin cleared flag")
	}
}

func TestRunEditOnlyRefreshLoop_TicksAndStopsOnContext(t *testing.T) {
	repo := &stubEditOnlyRepo{
		ontologies: []oms.Ontology{{RID: "ri.ontology.main", APIName: "main"}},
		objectTypes: map[string][]oms.ObjectType{
			"ri.ontology.main": {{RID: "ri.ot.order", APIName: "order"}},
		},
		properties: map[string][]oms.Property{
			"ri.ot.order": {{APIName: "notes", IsEditOnly: true}},
		},
	}
	r := newEditOnlyResolver(repo)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runEditOnlyRefreshLoop(ctx, r, 5*time.Millisecond, nil)
		close(done)
	}()
	deadline := time.After(500 * time.Millisecond)
	for {
		if repo.listOntCalls.Load() >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected at least 2 Refresh ticks, got %d", repo.listOntCalls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("loop did not exit after ctx cancel")
	}
}

func TestRunEditOnlyRefreshLoop_NilOrZeroIntervalIsNoop(t *testing.T) {
	repo := &stubEditOnlyRepo{ontologies: []oms.Ontology{{RID: "ri.x", APIName: "x"}}}
	r := newEditOnlyResolver(repo)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// nil resolver
	done := make(chan struct{})
	go func() {
		runEditOnlyRefreshLoop(ctx, nil, 5*time.Millisecond, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("nil resolver: loop did not return immediately")
	}
	// zero interval
	done2 := make(chan struct{})
	go func() {
		runEditOnlyRefreshLoop(ctx, r, 0, nil)
		close(done2)
	}()
	select {
	case <-done2:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("zero interval: loop did not return immediately")
	}
	if repo.listOntCalls.Load() != 0 {
		t.Fatalf("expected no Refresh calls for noop loop, got %d", repo.listOntCalls.Load())
	}
}

func TestRunEditOnlyRefreshLoop_OnErrorContinues(t *testing.T) {
	repo := &stubEditOnlyRepo{listOntsErr: errors.New("transient")}
	r := newEditOnlyResolver(repo)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 4)
	done := make(chan struct{})
	go func() {
		runEditOnlyRefreshLoop(ctx, r, 5*time.Millisecond, func(err error) {
			select {
			case errs <- err:
			default:
			}
		})
		close(done)
	}()
	// Collect at least two error reports — the loop must keep ticking
	// despite the failure, not bail out.
	got := 0
	deadline := time.After(500 * time.Millisecond)
	for got < 2 {
		select {
		case <-errs:
			got++
		case <-deadline:
			t.Fatalf("expected at least 2 onError calls, got %d", got)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("loop did not exit after ctx cancel despite errors")
	}
}
