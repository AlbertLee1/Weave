package objectset_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// recordingAuditor is a simple stub that implements
// objectset.DataAccessAuditor. Tests assert on the captured events.
type recordingAuditor struct {
	mu     sync.Mutex
	events []recordedAuditEvent
}

type recordedAuditEvent struct {
	OntologyRID       string
	ObjectTypeAPIName string
	Details           map[string]any
}

func (a *recordingAuditor) RecordLoadObjectSet(_ context.Context, ontologyRID, objectTypeAPIName string, details map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := make(map[string]any, len(details))
	for k, v := range details {
		cp[k] = v
	}
	a.events = append(a.events, recordedAuditEvent{
		OntologyRID:       ontologyRID,
		ObjectTypeAPIName: objectTypeAPIName,
		Details:           cp,
	})
}

func (a *recordingAuditor) snapshot() []recordedAuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]recordedAuditEvent, len(a.events))
	copy(out, a.events)
	return out
}

func TestLoadObjects_DataAccessAudit_Invoked(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	auditor := &recordingAuditor{}
	handler.SetDataAccessAuditor(auditor)

	body := objectset.LoadObjectSetRequest{
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Select:    []string{"id", "name"},
	}
	raw, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/myOntology/objectSets/loadObjects", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("loadObjects: code = %d, body = %s", w.Code, w.Body.String())
	}

	events := auditor.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.OntologyRID != "myOntology" {
		t.Errorf("OntologyRID = %q, want myOntology", evt.OntologyRID)
	}
	if evt.ObjectTypeAPIName != "employee" {
		t.Errorf("ObjectTypeAPIName = %q, want employee", evt.ObjectTypeAPIName)
	}
	if _, ok := evt.Details["count"]; !ok {
		t.Errorf("expected 'count' detail, got %#v", evt.Details)
	}
	if _, ok := evt.Details["totalCount"]; !ok {
		t.Errorf("expected 'totalCount' detail, got %#v", evt.Details)
	}
}

func TestLoadObjects_DataAccessAudit_UnsetIsNoOp(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	// No auditor wired — handler must not panic and must still return 200.

	body := objectset.LoadObjectSetRequest{
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
		Select:    []string{"id"},
	}
	raw, _ := json.Marshal(body)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/myOntology/objectSets/loadObjects", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("loadObjects: code = %d, body = %s", w.Code, w.Body.String())
	}
}
