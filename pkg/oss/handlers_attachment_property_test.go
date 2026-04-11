package oss_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// setupAttachmentPropertyTest wires an OSS handler with an AttachmentStore,
// creates an invoice object whose "scan" property is an attachment RID, and
// returns the router plus the underlying store and uploaded attachment.
func setupAttachmentPropertyTest(t *testing.T) (http.Handler, *attachment.LocalStore, *attachment.Attachment) {
	t.Helper()

	svc, mgr, repo, _ := setupOSSTest(t)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.invoice",
		OntologyRID: testOntologyRID,
		APIName:     "invoice",
		DisplayName: "Invoice",
		PrimaryKey:  "invoiceId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	if _, err := mgr.EnsureIndex("invoice", []index.Property{
		{APIName: "invoiceId", BaseType: "string", IsSearchable: true},
		{APIName: "scan", BaseType: "attachment", IsSearchable: false},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	store := attachment.NewLocalStore(t.TempDir())
	att, err := store.Put(context.Background(), attachment.BlobMeta{
		Filename:  "scan.pdf",
		MediaType: "application/pdf",
	}, strings.NewReader("blob-body"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	if err := mgr.IndexDocument("invoice", "inv1", map[string]interface{}{
		"invoiceId": "inv1",
		"scan":      att.RID,
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	h := oss.NewHandler(svc)
	h.SetAttachmentStore(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, store, att
}

func TestGetAttachmentProperty_Metadata(t *testing.T) {
	r, _, att := setupAttachmentPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/invoice/inv1/attachments/scan",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var out attachment.Attachment
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.RID != att.RID {
		t.Errorf("rid = %q, want %q", out.RID, att.RID)
	}
	if out.Filename != "scan.pdf" {
		t.Errorf("filename = %q", out.Filename)
	}
	if out.MediaType != "application/pdf" {
		t.Errorf("mediaType = %q", out.MediaType)
	}
}

func TestGetAttachmentProperty_Content(t *testing.T) {
	r, _, _ := setupAttachmentPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/invoice/inv1/attachments/scan/content",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "blob-body" {
		t.Errorf("body = %q", string(body))
	}
}

func TestGetAttachmentProperty_MetadataByRID(t *testing.T) {
	r, _, att := setupAttachmentPropertyTest(t)

	url := "/api/v2/ontologies/" + testOntologyRID + "/objects/invoice/inv1/attachments/scan/" + att.RID
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out attachment.Attachment
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.RID != att.RID {
		t.Errorf("rid = %q, want %q", out.RID, att.RID)
	}
}

func TestGetAttachmentProperty_ContentByRID(t *testing.T) {
	r, _, att := setupAttachmentPropertyTest(t)

	url := "/api/v2/ontologies/" + testOntologyRID + "/objects/invoice/inv1/attachments/scan/" + att.RID + "/content"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "blob-body" {
		t.Errorf("body = %q", string(body))
	}
}

func TestGetAttachmentProperty_WrongRID_404(t *testing.T) {
	r, _, _ := setupAttachmentPropertyTest(t)

	wrong := "ri.attachments.main.attachment.00000000-0000-0000-0000-000000000000"
	url := "/api/v2/ontologies/" + testOntologyRID + "/objects/invoice/inv1/attachments/scan/" + wrong
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "AttachmentNotFound" {
		t.Errorf("errorName = %q, want AttachmentNotFound", apiErr.ErrorName)
	}
}

func TestGetAttachmentProperty_PropertyNotFound(t *testing.T) {
	r, _, _ := setupAttachmentPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/invoice/inv1/attachments/missing",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "PropertyNotFound" {
		t.Errorf("errorName = %q, want PropertyNotFound", apiErr.ErrorName)
	}
}

func TestGetAttachmentProperty_ObjectNotFound(t *testing.T) {
	r, _, _ := setupAttachmentPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/invoice/nope/attachments/scan",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAttachmentProperty_NotConfigured(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	h := oss.NewHandler(svc)
	// No SetAttachmentStore call — store is nil.
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/invoice/inv1/attachments/scan",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "AttachmentStoreNotConfigured" {
		t.Errorf("errorName = %q, want AttachmentStoreNotConfigured", apiErr.ErrorName)
	}
}
