//go:build integration

package actions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// recordingPublisher captures every EditBatch published so the BDD scenario
// can assert "publish never happened when schema validation rejects" without
// needing a real NATS connection.
type recordingPublisher struct {
	batches []*funnel.EditBatch
	offset  uint64
}

func (p *recordingPublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	p.batches = append(p.batches, batch)
	p.offset++
	return p.offset, nil
}

// TestBDD_US473_CollapseSchemaValidation pins the PRD literal acceptance:
//
//	Given an ObjectType whose declared schema lists only {id, title}
//	  And an ActionType whose rule writes propertyBindings for both `title`
//	      (declared) and `unknown_field` (NOT declared)
//	When a client POSTs /actions/apply with that action
//	Then the executor's post-collapse schema check rejects the batch with
//	     HTTP 400 errorName=SchemaViolation pointing at the undeclared
//	     property; nothing is published to the funnel.
//
// A control scenario in the same test confirms an apply that only writes
// declared properties (`title`) succeeds with HTTP 200 and a single
// published EditBatch — so the guard is precise, not a blanket rejection.
func TestBDD_US473_PostCollapseSchemaRejectsUndeclaredProperty(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "us473_bdd",
		DisplayName: "US-473 BDD",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	docOT := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "doc",
		DisplayName: "Doc",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, docOT); err != nil {
		t.Fatalf("create object type: %v", err)
	}

	// Declare only id + title. Any other property name an action rule emits
	// must trip the US-473 schema check at collapse time.
	for _, p := range []*oms.Property{
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: docOT.RID,
			APIName:       "id",
			DisplayName:   "ID",
			BaseType:      "string",
			Status:        "ACTIVE",
		},
		{
			RID:           rid.NewPropertyRID(),
			ObjectTypeRID: docOT.RID,
			APIName:       "title",
			DisplayName:   "Title",
			BaseType:      "string",
			Status:        "ACTIVE",
		},
	} {
		if err := repo.CreateProperty(ctx, p); err != nil {
			t.Fatalf("create property %s: %v", p.APIName, err)
		}
	}

	// ActionType whose rule writes BOTH a declared and an undeclared field.
	actionParams, _ := json.Marshal([]map[string]interface{}{
		{"id": "docId", "type": "string", "required": true},
		{"id": "title", "type": "string", "required": true},
		{"id": "noise", "type": "string", "required": true},
	})
	badRules, _ := json.Marshal([]map[string]interface{}{
		{
			"type":       "modifyObject",
			"objectType": "doc",
			"propertyBindings": map[string]interface{}{
				"title":         map[string]interface{}{"type": "parameter", "value": "title"},
				"unknown_field": map[string]interface{}{"type": "parameter", "value": "noise"},
			},
		},
	})
	badAction := &oms.ActionType{
		RID:         rid.NewActionTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "updateDocBadly",
		DisplayName: "Update Doc Badly",
		Status:      "ACTIVE",
		Parameters:  actionParams,
		Rules:       badRules,
	}
	if err := repo.CreateActionType(ctx, badAction); err != nil {
		t.Fatalf("create bad action type: %v", err)
	}

	goodParams, _ := json.Marshal([]map[string]interface{}{
		{"id": "docId", "type": "string", "required": true},
		{"id": "title", "type": "string", "required": true},
	})
	goodRules, _ := json.Marshal([]map[string]interface{}{
		{
			"type":       "modifyObject",
			"objectType": "doc",
			"propertyBindings": map[string]interface{}{
				"title": map[string]interface{}{"type": "parameter", "value": "title"},
			},
		},
	})
	goodAction := &oms.ActionType{
		RID:         rid.NewActionTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "updateDocCleanly",
		DisplayName: "Update Doc Cleanly",
		Status:      "ACTIVE",
		Parameters:  goodParams,
		Rules:       goodRules,
	}
	if err := repo.CreateActionType(ctx, goodAction); err != nil {
		t.Fatalf("create good action type: %v", err)
	}

	pub := &recordingPublisher{}
	executor := actions.NewExecutor(repo, pub)
	handler := actions.NewHandler(executor)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", handler.Apply)

	post := func(actionAPIName string, body []byte) (int, []byte) {
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/actions/%s/apply", ont.APIName, actionAPIName),
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code, w.Body.Bytes()
	}

	// ---------- Scenario A: undeclared property → 400 SchemaViolation ----
	badBody, _ := json.Marshal(map[string]interface{}{
		"parameters": map[string]interface{}{
			"docId": "doc-1",
			"title": "Hello",
			"noise": "ignored",
		},
	})

	code, body := post("updateDocBadly", badBody)
	if code != http.StatusBadRequest {
		t.Fatalf("undeclared-property apply expected 400, got %d: %s", code, body)
	}

	var apiErr struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(body, &apiErr); err != nil {
		t.Fatalf("decode bad-apply error body: %v", err)
	}
	if apiErr.ErrorCode != "BAD_REQUEST" {
		t.Errorf("errorCode = %q, want BAD_REQUEST", apiErr.ErrorCode)
	}
	if apiErr.ErrorName != "SchemaViolation" {
		t.Errorf("errorName = %q, want SchemaViolation", apiErr.ErrorName)
	}
	if got := apiErr.Parameters["objectType"]; got != "doc" {
		t.Errorf("parameters.objectType = %q, want doc", got)
	}
	if got := apiErr.Parameters["property"]; got != "unknown_field" {
		t.Errorf("parameters.property = %q, want unknown_field", got)
	}
	if got := apiErr.Parameters["primaryKey"]; got != "doc-1" {
		t.Errorf("parameters.primaryKey = %q, want doc-1", got)
	}

	// Crucial invariant: no publish happened. Schema rejection is upstream
	// of NATS so a violation can never poison downstream consumers.
	if len(pub.batches) != 0 {
		t.Errorf("schema violation published %d batch(es), want 0 (must reject pre-publish)", len(pub.batches))
	}

	// ---------- Scenario B: declared-only properties → 200, one publish --
	goodBody, _ := json.Marshal(map[string]interface{}{
		"parameters": map[string]interface{}{
			"docId": "doc-2",
			"title": "World",
		},
	})

	code, body = post("updateDocCleanly", goodBody)
	if code != http.StatusOK {
		t.Fatalf("declared-only apply expected 200, got %d: %s", code, body)
	}
	if len(pub.batches) != 1 {
		t.Fatalf("declared-only apply published %d batch(es), want 1", len(pub.batches))
	}
	publishedEdits := pub.batches[0].Edits
	if len(publishedEdits) != 1 {
		t.Fatalf("published batch carries %d edits, want 1", len(publishedEdits))
	}
	if publishedEdits[0].Type != funnel.EditTypeModify {
		t.Errorf("published edit type = %q, want MODIFY", publishedEdits[0].Type)
	}
	if publishedEdits[0].Properties["title"] != "World" {
		t.Errorf("published title = %v, want World", publishedEdits[0].Properties["title"])
	}
}
