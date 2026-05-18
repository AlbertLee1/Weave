package oss

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

type fakeIngestMetadataRepo struct {
	objectTypes map[string]*oms.ObjectType
	properties  map[string][]oms.Property
	valueTypes  map[string]*oms.ValueType
}

func (f *fakeIngestMetadataRepo) GetObjectTypeByAPIName(_ context.Context, ontologyAPIName, apiName string) (*oms.ObjectType, error) {
	if ot, ok := f.objectTypes[ontologyAPIName+":"+apiName]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func (f *fakeIngestMetadataRepo) ListProperties(_ context.Context, objectTypeRID string) ([]oms.Property, error) {
	return f.properties[objectTypeRID], nil
}

func (f *fakeIngestMetadataRepo) GetValueTypeByAPIName(_ context.Context, apiName string) (*oms.ValueType, error) {
	if vt, ok := f.valueTypes[apiName]; ok {
		return vt, nil
	}
	return nil, oms.ErrNotFound
}

func newIngestRouterWithMetadataValidator(pub IngestPublisher, repo *fakeIngestMetadataRepo) chi.Router {
	h := NewStreamIngestHandler(pub)
	h.SetMetadataValidator(NewStreamIngestMetadataValidator(repo))
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/streams/{objectType}/ingest", h.ServeHTTP)
	return r
}

func newSelf102MetadataRepo() *fakeIngestMetadataRepo {
	const otRID = "ri.ontology.main.object-type.ai-news"
	mustRaw := func(v map[string]interface{}) json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		return b
	}
	return &fakeIngestMetadataRepo{
		objectTypes: map[string]*oms.ObjectType{
			"ainews:AI_News": {
				RID:         otRID,
				OntologyRID: "ri.ontology.main.ontology.ainews",
				APIName:     "AI_News",
				PrimaryKey:  "id",
				Status:      "ACTIVE",
				Visibility:  "NORMAL",
			},
		},
		properties: map[string][]oms.Property{
			otRID: {
				{RID: "ri.property.id", ObjectTypeRID: otRID, APIName: "id", BaseType: "string"},
				{RID: "ri.property.title", ObjectTypeRID: otRID, APIName: "title", BaseType: "string"},
				{RID: "ri.property.ticker", ObjectTypeRID: otRID, APIName: "ticker", BaseType: "string", TypeConfig: mustRaw(map[string]interface{}{
					"valueTypeApiName": "TickerSymbol",
				})},
			},
		},
		valueTypes: map[string]*oms.ValueType{
			"TickerSymbol": {
				RID:      "ri.ontology.main.value-type.ticker",
				APIName:  "TickerSymbol",
				BaseType: "string",
				Constraints: mustRaw(map[string]interface{}{
					"pattern": "^[A-Z]{1,5}$",
				}),
			},
		},
	}
}

func postSelf102Ingest(t *testing.T, r chi.Router, body string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ainews/streams/AI_News/ingest",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	var decoded map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response body %q: %v", rr.Body.String(), err)
	}
	return rr.Code, decoded
}

func TestStreamIngest_BDD_FailsFastOnSchemaAndValueTypeViolations(t *testing.T) {
	t.Run("Given an undeclared property, When ingest runs, Then it returns SchemaViolation and does not publish", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		r := newIngestRouterWithMetadataValidator(pub, newSelf102MetadataRepo())

		code, body := postSelf102Ingest(t, r, `{"edits":[
			{"type":"CREATE","objectType":"IgnoredByURL","primaryKey":"news-1","properties":{"id":"news-1","title":"AI","unknownField":"x"}}
		]}`)

		if code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body = %#v", code, body)
		}
		if body["errorName"] != "SchemaViolation" {
			t.Fatalf("errorName = %v, want SchemaViolation", body["errorName"])
		}
		params, _ := body["parameters"].(map[string]interface{})
		if params["objectType"] != "AI_News" {
			t.Fatalf("parameters.objectType = %v, want AI_News", params["objectType"])
		}
		if params["property"] != "unknownField" {
			t.Fatalf("parameters.property = %v, want unknownField", params["property"])
		}
		if len(pub.batches) != 0 {
			t.Fatalf("publisher received %d batches, want 0", len(pub.batches))
		}
	})

	t.Run("Given a ValueType pattern violation, When ingest runs, Then it returns a structured validation error and does not publish", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		r := newIngestRouterWithMetadataValidator(pub, newSelf102MetadataRepo())

		code, body := postSelf102Ingest(t, r, `{"edits":[
			{"type":"CREATE","objectType":"AI_News","primaryKey":"news-2","properties":{"id":"news-2","title":"AI","ticker":"bad-1"}}
		]}`)

		if code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body = %#v", code, body)
		}
		if body["errorName"] != "ValueTypeConstraintViolation" {
			t.Fatalf("errorName = %v, want ValueTypeConstraintViolation", body["errorName"])
		}
		params, _ := body["parameters"].(map[string]interface{})
		if params["property"] != "ticker" {
			t.Fatalf("parameters.property = %v, want ticker", params["property"])
		}
		if !strings.Contains(params["reason"].(string), "pattern") {
			t.Fatalf("parameters.reason = %v, want pattern failure", params["reason"])
		}
		if len(pub.batches) != 0 {
			t.Fatalf("publisher received %d batches, want 0", len(pub.batches))
		}
	})

	t.Run("Given valid edits, When ingest runs, Then source tagging objectType coercion and publish response remain unchanged", func(t *testing.T) {
		pub := &mockIngestPublisher{}
		r := newIngestRouterWithMetadataValidator(pub, newSelf102MetadataRepo())

		code, body := postSelf102Ingest(t, r, `{"edits":[
			{"type":"CREATE","objectType":"IgnoredByURL","primaryKey":"news-3","properties":{"id":"news-3","title":"AI","ticker":"NVDA"}}
		]}`)

		if code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %#v", code, body)
		}
		if body["editCount"] != float64(1) {
			t.Fatalf("editCount = %v, want 1", body["editCount"])
		}
		if len(pub.batches) != 1 {
			t.Fatalf("publisher received %d batches, want 1", len(pub.batches))
		}
		edit := pub.batches[0].Edits[0]
		if edit.ObjectType != "AI_News" {
			t.Fatalf("published ObjectType = %q, want AI_News", edit.ObjectType)
		}
		if edit.Source != "ingest" {
			t.Fatalf("published Source = %q, want ingest", edit.Source)
		}
	})
}
