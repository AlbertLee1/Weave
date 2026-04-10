package main

// US-033: the 4 Foundry global attachment endpoints must be reachable
// through NewFullRouter when an AttachmentStore is wired. Without the
// store, the routes stay unregistered (degraded mode).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/attachment"
)

func TestUS033_AttachmentRoutesRegistered(t *testing.T) {
	store := attachment.NewLocalStore(t.TempDir())
	deps := &ServerDeps{AttachmentStore: store}
	router := NewFullRouter(deps)

	tests := []struct {
		name    string
		method  string
		path    string
		wantNot int // status code that would indicate the route is unmounted
	}{
		{
			name:    "upload generated rid",
			method:  http.MethodPost,
			path:    "/api/v2/ontologies/attachments/upload?filename=x.bin",
			wantNot: http.StatusNotFound,
		},
		{
			name:    "upload with rid",
			method:  http.MethodPost,
			path:    "/api/v2/ontologies/attachments/upload/" + attachment.NewAttachmentRID() + "?filename=x.bin",
			wantNot: http.StatusNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("hi"))
			req.Header.Set("Content-Type", "application/octet-stream")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code == tt.wantNot &&
				!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
				t.Errorf("%s %s: got chi default %d — route not registered",
					tt.method, tt.path, rec.Code)
			}
		})
	}
}

// TestUS033_UploadAndGetRoundTrip exercises the full round-trip:
// upload -> get metadata -> get content via the HTTP router.
func TestUS033_UploadAndGetRoundTrip(t *testing.T) {
	store := attachment.NewLocalStore(t.TempDir())
	deps := &ServerDeps{AttachmentStore: store}
	router := NewFullRouter(deps)

	body := "round-trip payload"

	// Upload
	uploadReq := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/attachments/upload?filename=rt.txt",
		strings.NewReader(body))
	uploadReq.Header.Set("Content-Type", "text/plain")
	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	// crude RID extract — the body is a JSON object with "rid" field
	ridStart := strings.Index(uploadRec.Body.String(), "\"rid\":\"") + len("\"rid\":\"")
	ridEnd := strings.Index(uploadRec.Body.String()[ridStart:], "\"")
	rid := uploadRec.Body.String()[ridStart : ridStart+ridEnd]
	if !strings.HasPrefix(rid, "ri.attachments.main.attachment.") {
		t.Fatalf("unexpected rid in upload response: %s", rid)
	}

	// Get metadata
	metaReq := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/attachments/"+rid, nil)
	metaRec := httptest.NewRecorder()
	router.ServeHTTP(metaRec, metaReq)
	if metaRec.Code != http.StatusOK {
		t.Fatalf("metadata status=%d body=%s", metaRec.Code, metaRec.Body.String())
	}

	// Get content
	contentReq := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/attachments/"+rid+"/content", nil)
	contentRec := httptest.NewRecorder()
	router.ServeHTTP(contentRec, contentReq)
	if contentRec.Code != http.StatusOK {
		t.Fatalf("content status=%d", contentRec.Code)
	}
	if got := contentRec.Body.String(); got != body {
		t.Errorf("content body = %q, want %q", got, body)
	}
	if ct := contentRec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("content-type = %q, want text/plain", ct)
	}
}

// TestUS033_NoRoutesWhenStoreMissing — when AttachmentStore is nil the
// routes are not mounted (dev/minimal harness).
func TestUS033_NoRoutesWhenStoreMissing(t *testing.T) {
	deps := &ServerDeps{}
	router := NewFullRouter(deps)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/attachments/upload?filename=x.bin",
		strings.NewReader("hi"))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 without AttachmentStore, got %d body=%s", rec.Code, rec.Body.String())
	}
}
