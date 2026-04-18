package oss_test

import (
	"context"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oss"
)

// stubMarkingRepo is an in-memory MarkingRepository used by the filter
// tests. It returns whatever names are seeded in userGrants for the
// requested user, so tests can simulate any combination of grants
// without standing up Postgres.
type stubMarkingRepo struct {
	userGrants map[string][]string
	err        error
}

func newStubMarkingRepo() *stubMarkingRepo {
	return &stubMarkingRepo{userGrants: make(map[string][]string)}
}

func (s *stubMarkingRepo) ListMarkings(_ context.Context) ([]auth.Marking, error) {
	return nil, s.err
}

func (s *stubMarkingRepo) GetUserMarkings(_ context.Context, userID string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.userGrants[userID], nil
}

func (s *stubMarkingRepo) GrantMarking(_ context.Context, _, _, _ string, _ *time.Time) error {
	return s.err
}

func (s *stubMarkingRepo) RevokeMarking(_ context.Context, _, _ string) error {
	return s.err
}

// markedObject is a tiny helper that constructs a WireObject and stamps a
// __markings array onto its Properties map. The MarkingFilter reads
// markings out of that field, exactly the way it would on a real Bleve
// hit.
func markedObject(t *testing.T, pk string, markings []string) *oss.WireObject {
	t.Helper()
	props := map[string]interface{}{"id": pk}
	if markings != nil {
		// Use []interface{} so the wire shape exactly matches what the
		// Bleve store returns from a multi-value keyword field.
		mv := make([]interface{}, len(markings))
		for i, m := range markings {
			mv[i] = m
		}
		props[auth.MarkingsField] = mv
	}
	return oss.FormatObject("doc", pk, props)
}

// TestMarkingFilter_NoMarkings_AllVisible asserts the back-compat path:
// when an object has no __markings field at all, it is treated as PUBLIC
// and visible to anyone, including users with zero grants. This is what
// preserves existing un-marked datasets.
func TestMarkingFilter_NoMarkings_AllVisible(t *testing.T) {
	repo := newStubMarkingRepo()
	filter := oss.NewMarkingFilter(repo)

	objs := []*oss.WireObject{
		markedObject(t, "d1", nil),
		markedObject(t, "d2", nil),
		markedObject(t, "d3", nil),
	}

	out, err := filter.FilterObjects(context.Background(), "user:bob", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("expected all 3 unmarked objects visible, got %d", len(out))
	}
}

// TestMarkingFilter_UserLacksMarking_Hidden verifies the core enforcement
// rule: an object carrying SECRET is dropped for a user who does not
// hold a SECRET grant.
func TestMarkingFilter_UserLacksMarking_Hidden(t *testing.T) {
	repo := newStubMarkingRepo()
	repo.userGrants["user:bob"] = []string{"PUBLIC"}
	filter := oss.NewMarkingFilter(repo)

	objs := []*oss.WireObject{
		markedObject(t, "d1", []string{"PUBLIC"}),
		markedObject(t, "d2", []string{"SECRET"}),
	}

	out, err := filter.FilterObjects(context.Background(), "user:bob", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 visible object, got %d", len(out))
	}
	if out[0].PrimaryKey != "d1" {
		t.Errorf("expected d1 to remain, got %v", out[0].PrimaryKey)
	}
}

// TestMarkingFilter_UserHasAllMarkings_Visible asserts that an object
// with multiple markings is visible when the user has every required
// grant. This is the "fully cleared" path.
func TestMarkingFilter_UserHasAllMarkings_Visible(t *testing.T) {
	repo := newStubMarkingRepo()
	repo.userGrants["user:alice"] = []string{"PUBLIC", "PII", "CONFIDENTIAL"}
	filter := oss.NewMarkingFilter(repo)

	objs := []*oss.WireObject{
		markedObject(t, "d1", []string{"PII", "CONFIDENTIAL"}),
	}

	out, err := filter.FilterObjects(context.Background(), "user:alice", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("expected fully-cleared object to be visible, got %d", len(out))
	}
}

// TestMarkingFilter_MultipleMarkings_AllRequired verifies the AND
// semantics: an object carrying both PII and SECRET is dropped when the
// user only holds one of them. This is the difference from ABAC, where
// any matching allow rule lets a row through; markings require *every*
// label to be granted.
func TestMarkingFilter_MultipleMarkings_AllRequired(t *testing.T) {
	repo := newStubMarkingRepo()
	repo.userGrants["user:bob"] = []string{"PII"} // missing SECRET
	filter := oss.NewMarkingFilter(repo)

	objs := []*oss.WireObject{
		markedObject(t, "d1", []string{"PII"}),
		markedObject(t, "d2", []string{"PII", "SECRET"}),
	}

	out, err := filter.FilterObjects(context.Background(), "user:bob", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 object, got %d", len(out))
	}
	if out[0].PrimaryKey != "d1" {
		t.Errorf("expected d1 (single matching marking) to remain, got %v", out[0].PrimaryKey)
	}
}

// TestMarkingFilter_NilFilter_PassThrough is the defensive path: when no
// MarkingFilter is wired (e.g. older tests that have not opted in), the
// service should behave exactly as before. This guards the
// applyAccessFilters chain from breaking on nil receivers.
func TestMarkingFilter_NilFilter_PassThrough(t *testing.T) {
	var filter *oss.MarkingFilter

	objs := []*oss.WireObject{
		markedObject(t, "d1", []string{"SECRET"}),
		markedObject(t, "d2", []string{"PII"}),
	}

	out, err := filter.FilterObjects(context.Background(), "user:bob", objs)
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("expected nil filter to pass through, got %d", len(out))
	}
}

// TestMarkingFilter_EmptyInput verifies that an empty or nil input slice
// is returned unchanged. This is the optimization fast-path the filter
// hits before any repo lookup, so it must not call GetUserMarkings.
func TestMarkingFilter_EmptyInput(t *testing.T) {
	repo := newStubMarkingRepo()
	repo.err = context.DeadlineExceeded // would fail if called
	filter := oss.NewMarkingFilter(repo)

	out, err := filter.FilterObjects(context.Background(), "user:bob", nil)
	if err != nil {
		t.Fatalf("FilterObjects nil: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(out))
	}

	out, err = filter.FilterObjects(context.Background(), "user:bob", []*oss.WireObject{})
	if err != nil {
		t.Fatalf("FilterObjects empty: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty result for empty input, got %d", len(out))
	}
}

// TestMarkingFilter_AcceptsStringSliceShape verifies that __markings is
// also recognised when stored as a plain []string (not just an
// []interface{}). Some writer paths build the doc map with []string and
// the filter must understand both shapes.
func TestMarkingFilter_AcceptsStringSliceShape(t *testing.T) {
	repo := newStubMarkingRepo()
	repo.userGrants["user:bob"] = []string{"PUBLIC"}
	filter := oss.NewMarkingFilter(repo)

	obj := oss.FormatObject("doc", "d1", map[string]interface{}{
		"id":               "d1",
		auth.MarkingsField: []string{"SECRET"},
	})

	out, err := filter.FilterObjects(context.Background(), "user:bob", []*oss.WireObject{obj})
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected SECRET object to be hidden from user with only PUBLIC, got %d", len(out))
	}
}

// TestMarkingFilter_SingleStringMarking covers the case where Bleve
// flattens a single-element multi-value field into a bare string rather
// than a slice. Bleve sometimes does this for stored fields, so the
// filter must accept it.
func TestMarkingFilter_SingleStringMarking(t *testing.T) {
	repo := newStubMarkingRepo()
	repo.userGrants["user:alice"] = []string{"INTERNAL"}
	filter := oss.NewMarkingFilter(repo)

	visible := oss.FormatObject("doc", "d1", map[string]interface{}{
		"id":               "d1",
		auth.MarkingsField: "INTERNAL",
	})
	hidden := oss.FormatObject("doc", "d2", map[string]interface{}{
		"id":               "d2",
		auth.MarkingsField: "SECRET",
	})

	out, err := filter.FilterObjects(context.Background(), "user:alice", []*oss.WireObject{visible, hidden})
	if err != nil {
		t.Fatalf("FilterObjects: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 visible object, got %d", len(out))
	}
	if out[0].PrimaryKey != "d1" {
		t.Errorf("expected d1, got %v", out[0].PrimaryKey)
	}
}
