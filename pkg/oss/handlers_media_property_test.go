package oss_test

import (
	"bytes"
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

// setupMediaPropertyTest wires an OSS handler with an AttachmentStore (reused
// as the media blob backend), creates a photo object whose "cover" property
// holds a media reference RID, and returns the router plus store and blob.
func setupMediaPropertyTest(t *testing.T) (http.Handler, *attachment.LocalStore, *attachment.Attachment) {
	t.Helper()

	svc, mgr, repo, _ := setupOSSTest(t)

	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.photo",
		OntologyRID: testOntologyRID,
		APIName:     "photo",
		DisplayName: "Photo",
		PrimaryKey:  "photoId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	if _, err := mgr.EnsureIndex("photo", []index.Property{
		{APIName: "photoId", BaseType: "string", IsSearchable: true},
		{APIName: "cover", BaseType: "mediaReference", IsSearchable: false},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	store := attachment.NewLocalStore(t.TempDir())
	blob, err := store.Put(context.Background(), attachment.BlobMeta{
		Filename:  "cover.jpg",
		MediaType: "image/jpeg",
	}, strings.NewReader("jpeg-bytes"))
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	if err := mgr.IndexDocument("photo", "p1", map[string]interface{}{
		"photoId": "p1",
		"cover":   blob.RID,
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	h := oss.NewHandler(svc)
	h.SetAttachmentStore(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, store, blob
}

func TestGetMediaProperty_Metadata(t *testing.T) {
	r, _, blob := setupMediaPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/photo/p1/media/cover/metadata",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["path"] != "cover.jpg" {
		t.Errorf("path = %v, want cover.jpg", out["path"])
	}
	if out["mediaType"] != "image/jpeg" {
		t.Errorf("mediaType = %v, want image/jpeg", out["mediaType"])
	}
	// sizeBytes is JSON number → float64
	if sz, _ := out["sizeBytes"].(float64); int64(sz) != blob.SizeBytes {
		t.Errorf("sizeBytes = %v, want %d", out["sizeBytes"], blob.SizeBytes)
	}
}

func TestGetMediaProperty_Content(t *testing.T) {
	r, _, _ := setupMediaPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/photo/p1/media/cover/content",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "jpeg-bytes" {
		t.Errorf("body = %q", string(body))
	}
}

func TestGetMediaProperty_PropertyNotFound(t *testing.T) {
	r, _, _ := setupMediaPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/photo/p1/media/missing/metadata",
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

func TestGetMediaProperty_ObjectNotFound(t *testing.T) {
	r, _, _ := setupMediaPropertyTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/photo/nope/media/cover/metadata",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetMediaProperty_NotConfigured(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	h := oss.NewHandler(svc)
	// No SetAttachmentStore call — store is nil.
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/photo/p1/media/cover/metadata",
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
	if apiErr.ErrorName != "MediaStoreNotConfigured" {
		t.Errorf("errorName = %q, want MediaStoreNotConfigured", apiErr.ErrorName)
	}
}

func TestUploadMediaProperty_Success(t *testing.T) {
	r, _, _ := setupMediaPropertyTest(t)

	body := bytes.NewReader([]byte("new-media-bytes"))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objectTypes/photo/media/cover/upload?mediaItemPath=newcover.png",
		body)
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var out struct {
		MimeType  string `json:"mimeType"`
		Reference struct {
			Type            string `json:"type"`
			MediaSetViewItem struct {
				MediaSetRid     string `json:"mediaSetRid"`
				MediaSetViewRid string `json:"mediaSetViewRid"`
				MediaItemRid    string `json:"mediaItemRid"`
			} `json:"mediaSetViewItem"`
		} `json:"reference"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.MimeType != "image/png" {
		t.Errorf("mimeType = %q, want image/png", out.MimeType)
	}
	if out.Reference.Type != "mediaSetViewItem" {
		t.Errorf("reference.type = %q, want mediaSetViewItem", out.Reference.Type)
	}
	if out.Reference.MediaSetViewItem.MediaItemRid == "" {
		t.Errorf("mediaItemRid is empty")
	}
	if !strings.HasPrefix(out.Reference.MediaSetViewItem.MediaItemRid, "ri.attachments.main.attachment.") {
		t.Errorf("mediaItemRid = %q, want ri.attachments.main.attachment.* prefix",
			out.Reference.MediaSetViewItem.MediaItemRid)
	}
}

func TestUploadMediaProperty_MissingMediaItemPath(t *testing.T) {
	r, _, _ := setupMediaPropertyTest(t)

	body := bytes.NewReader([]byte("bytes"))
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objectTypes/photo/media/cover/upload",
		body)
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "MissingMediaItemPath" {
		t.Errorf("errorName = %q, want MissingMediaItemPath", apiErr.ErrorName)
	}
}

func TestUploadMediaProperty_NotConfigured(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	h := oss.NewHandler(svc)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objectTypes/photo/media/cover/upload?mediaItemPath=x.png",
		bytes.NewReader([]byte("x")))
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}
