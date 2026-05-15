// Package timeline implements the Vertex Timeline events overview API.
// It collects ObjectTypes marked IsEvent=true on a given ontology and
// produces a flattened Event[] within a [from, to] window, suitable for
// rendering as horizontal time bars.
//
// The package is split into:
//   - ObjectTypeLister: pkg/oms ListObjectTypes contract (so tests stub
//     metadata without a Postgres container)
//   - ObjectFetcher: load object rows of a given ObjectType, optionally
//     filtered by a RID allow-list (so tests stub OSS reads)
//   - Service: composes the above and returns []Event
package timeline

import (
	"context"
	"fmt"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// ObjectTypeLister is the slice of pkg/oms.Repository the timeline service
// needs. Concrete impl: *oms.PGRepository.
type ObjectTypeLister interface {
	ListObjectTypes(ctx context.Context, ontologyRID string) ([]oms.ObjectType, error)
}

// ObjectFilter narrows the FetchObjects result set.
type ObjectFilter struct {
	// ObjectRIDs, when non-empty, restricts the returned set to these
	// object RIDs. Empty means "all rows of this ObjectType".
	ObjectRIDs []string
}

// ObjectRow is the minimum shape the timeline service consumes per row.
// pkg/oss adapter must produce these (RID + Properties map).
type ObjectRow struct {
	RID        string
	Properties map[string]any
}

// ObjectFetcher loads ObjectRow slices for one ObjectType at a time.
type ObjectFetcher interface {
	FetchObjects(ctx context.Context, objectTypeRID string, filter ObjectFilter) ([]ObjectRow, error)
}

// EventsQuery is the input shape for QueryEvents.
type EventsQuery struct {
	OntologyRID string
	From, To    time.Time
	// ObjectRIDs optionally narrows to a specific set of objects.
	ObjectRIDs []string
}

// Event is one Timeline bar.
type Event struct {
	RID        string     `json:"rid"`
	ObjectType string     `json:"objectType"`
	Title      string     `json:"title,omitempty"`
	Start      time.Time  `json:"start"`
	End        *time.Time `json:"end,omitempty"`
}

// Service queries events.
type Service struct {
	lister  ObjectTypeLister
	fetcher ObjectFetcher
}

// NewService builds a Service. Both deps must be non-nil (programmer
// error to pass nil — surfaced as a panic).
func NewService(lister ObjectTypeLister, fetcher ObjectFetcher) *Service {
	if lister == nil || fetcher == nil {
		panic("timeline: nil dependency")
	}
	return &Service{lister: lister, fetcher: fetcher}
}

// QueryEvents materializes the event list for a given window. It walks
// every is_event=true ObjectType on the ontology; for each, it asks the
// fetcher for matching rows; and produces an Event per row whose Start
// is within [From, To]. Out-of-window rows are filtered.
func (s *Service) QueryEvents(ctx context.Context, q EventsQuery) ([]Event, error) {
	ots, err := s.lister.ListObjectTypes(ctx, q.OntologyRID)
	if err != nil {
		return nil, fmt.Errorf("timeline: list object types: %w", err)
	}
	filter := ObjectFilter{ObjectRIDs: q.ObjectRIDs}
	var out []Event
	for _, ot := range ots {
		if !ot.IsEvent || ot.EventStartProp == "" {
			continue
		}
		rows, err := s.fetcher.FetchObjects(ctx, ot.RID, filter)
		if err != nil {
			return nil, fmt.Errorf("timeline: fetch %s: %w", ot.RID, err)
		}
		for _, row := range rows {
			start, ok := timeProp(row.Properties, ot.EventStartProp)
			if !ok {
				continue
			}
			if start.Before(q.From) || start.After(q.To) {
				continue
			}
			ev := Event{
				RID:        row.RID,
				ObjectType: ot.RID,
				Start:      start,
			}
			if ot.EventEndProp != "" {
				if end, ok := timeProp(row.Properties, ot.EventEndProp); ok {
					ev.End = &end
				}
			}
			if title, ok := row.Properties["title"].(string); ok {
				ev.Title = title
			}
			out = append(out, ev)
		}
	}
	return out, nil
}

func timeProp(props map[string]any, key string) (time.Time, bool) {
	v, ok := props[key]
	if !ok {
		return time.Time{}, false
	}
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return time.Time{}, false
		}
		return parsed, true
	}
	return time.Time{}, false
}
