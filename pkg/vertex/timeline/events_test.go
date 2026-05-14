package timeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// fakeObjectTypeLister returns a fixed slice of ObjectTypes for an
// ontology. Used to stub out pkg/oms ListObjectTypes without dragging in
// a real PG container.
type fakeObjectTypeLister struct {
	byOnt map[string][]oms.ObjectType
}

func (f *fakeObjectTypeLister) ListObjectTypes(_ context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	if f.byOnt == nil {
		return nil, errors.New("uninitialised stub")
	}
	return f.byOnt[ontologyRID], nil
}

// fakeObjectFetcher returns canned ObjectRow slices keyed by ObjectType RID.
type fakeObjectFetcher struct {
	rowsByType map[string][]ObjectRow
}

func (f *fakeObjectFetcher) FetchObjects(_ context.Context, objectTypeRID string, filter ObjectFilter) ([]ObjectRow, error) {
	rows := f.rowsByType[objectTypeRID]
	if len(filter.ObjectRIDs) > 0 {
		want := map[string]struct{}{}
		for _, rid := range filter.ObjectRIDs {
			want[rid] = struct{}{}
		}
		out := rows[:0:0]
		for _, r := range rows {
			if _, ok := want[r.RID]; ok {
				out = append(out, r)
			}
		}
		return out, nil
	}
	return rows, nil
}

func TestEvents_Given_OneEventType_When_Query_Then_ReturnsEvents(t *testing.T) {
	ontRID := "ri.ontology.main.ontology.main"
	otRID := "ri.ontology.main.object-type.flight-delay"

	lister := &fakeObjectTypeLister{byOnt: map[string][]oms.ObjectType{
		ontRID: {{
			RID:            otRID,
			IsEvent:        true,
			EventStartProp: "startedAt",
			EventEndProp:   "resolvedAt",
		}},
	}}
	fetcher := &fakeObjectFetcher{rowsByType: map[string][]ObjectRow{
		otRID: {
			{
				RID: "fd1",
				Properties: map[string]any{
					"startedAt":  time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
					"resolvedAt": time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC),
					"title":      "JFK delay",
				},
			},
		},
	}}
	svc := NewService(lister, fetcher)

	got, err := svc.QueryEvents(context.Background(), EventsQuery{
		OntologyRID: ontRID,
		From:        time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		To:          time.Date(2026, 5, 14, 23, 59, 59, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].RID != "fd1" {
		t.Errorf("rid: want fd1, got %s", got[0].RID)
	}
	if !got[0].Start.Equal(time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("start: %v", got[0].Start)
	}
	if got[0].End == nil || !got[0].End.Equal(time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("end: %v", got[0].End)
	}
	if got[0].ObjectType != otRID {
		t.Errorf("object type: want %s, got %s", otRID, got[0].ObjectType)
	}
}

func TestEvents_Given_NonEventType_When_Query_Then_Skipped(t *testing.T) {
	ontRID := "ri.ontology.main.ontology.main"
	lister := &fakeObjectTypeLister{byOnt: map[string][]oms.ObjectType{
		ontRID: {
			{RID: "rt.airport", IsEvent: false},
		},
	}}
	fetcher := &fakeObjectFetcher{rowsByType: map[string][]ObjectRow{}}
	svc := NewService(lister, fetcher)
	got, err := svc.QueryEvents(context.Background(), EventsQuery{
		OntologyRID: ontRID,
		From:        time.Now().Add(-time.Hour),
		To:          time.Now(),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 events for non-event types, got %d", len(got))
	}
}

func TestEvents_Given_EventOutsideWindow_When_Query_Then_Filtered(t *testing.T) {
	ontRID := "ri.ontology.main.ontology.main"
	otRID := "ri.ontology.main.object-type.flight-delay"

	lister := &fakeObjectTypeLister{byOnt: map[string][]oms.ObjectType{
		ontRID: {{
			RID:            otRID,
			IsEvent:        true,
			EventStartProp: "startedAt",
		}},
	}}
	fetcher := &fakeObjectFetcher{rowsByType: map[string][]ObjectRow{
		otRID: {
			{
				RID: "in-window",
				Properties: map[string]any{
					"startedAt": time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
				},
			},
			{
				RID: "future",
				Properties: map[string]any{
					"startedAt": time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			},
		},
	}}
	svc := NewService(lister, fetcher)

	got, err := svc.QueryEvents(context.Background(), EventsQuery{
		OntologyRID: ontRID,
		From:        time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		To:          time.Date(2026, 5, 14, 23, 59, 59, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 in-window, got %d", len(got))
	}
	if got[0].RID != "in-window" {
		t.Errorf("rid: %s", got[0].RID)
	}
}

func TestEvents_Given_ObjectRidsFilter_When_Query_Then_OnlyThoseReturned(t *testing.T) {
	ontRID := "ri.ontology.main.ontology.main"
	otRID := "ri.ontology.main.object-type.flight-delay"

	lister := &fakeObjectTypeLister{byOnt: map[string][]oms.ObjectType{
		ontRID: {{
			RID:            otRID,
			IsEvent:        true,
			EventStartProp: "startedAt",
		}},
	}}
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	fetcher := &fakeObjectFetcher{rowsByType: map[string][]ObjectRow{
		otRID: {
			{RID: "a", Properties: map[string]any{"startedAt": now}},
			{RID: "b", Properties: map[string]any{"startedAt": now}},
			{RID: "c", Properties: map[string]any{"startedAt": now}},
		},
	}}
	svc := NewService(lister, fetcher)

	got, err := svc.QueryEvents(context.Background(), EventsQuery{
		OntologyRID: ontRID,
		From:        now.Add(-time.Hour),
		To:          now.Add(time.Hour),
		ObjectRIDs:  []string{"a", "c"},
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}

func TestEvents_Given_PointEventNoEndProp_When_Query_Then_EndIsNil(t *testing.T) {
	ontRID := "ri.ontology.main.ontology.main"
	otRID := "ri.ontology.main.object-type.alert"

	lister := &fakeObjectTypeLister{byOnt: map[string][]oms.ObjectType{
		ontRID: {{
			RID:            otRID,
			IsEvent:        true,
			EventStartProp: "firedAt",
		}},
	}}
	fired := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	fetcher := &fakeObjectFetcher{rowsByType: map[string][]ObjectRow{
		otRID: {{RID: "a", Properties: map[string]any{"firedAt": fired}}},
	}}
	svc := NewService(lister, fetcher)

	got, err := svc.QueryEvents(context.Background(), EventsQuery{
		OntologyRID: ontRID,
		From:        fired.Add(-time.Hour),
		To:          fired.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
	if got[0].End != nil {
		t.Errorf("expected nil End for point event, got %v", got[0].End)
	}
}

func TestEvents_Given_EventTypeWithoutStartProp_When_Query_Then_Skipped(t *testing.T) {
	ontRID := "ri.ontology.main.ontology.main"
	otRID := "ri.ontology.main.object-type.broken"
	lister := &fakeObjectTypeLister{byOnt: map[string][]oms.ObjectType{
		ontRID: {{RID: otRID, IsEvent: true, EventStartProp: ""}},
	}}
	fetcher := &fakeObjectFetcher{rowsByType: map[string][]ObjectRow{
		otRID: {{RID: "a", Properties: map[string]any{"foo": 1}}},
	}}
	svc := NewService(lister, fetcher)
	got, err := svc.QueryEvents(context.Background(), EventsQuery{
		OntologyRID: ontRID,
		From:        time.Now().Add(-time.Hour),
		To:          time.Now(),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 events (no start prop = unconfigured), got %d", len(got))
	}
}
