package attachment

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// newTestRouter wires the 4 Foundry attachment endpoints onto a fresh
// chi router backed by a LocalStore in a temporary directory.
func newTestRouter(t *testing.T) (*chi.Mux, *LocalStore) {
	t.Helper()
	store := NewLocalStore(t.TempDir())
	h := NewHandler(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, store
}

func decodeAttachment(t *testing.T, body io.Reader) *Attachment {
	t.Helper()
	var att Attachment
	if err := json.NewDecoder(body).Decode(&att); err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	return &att
}

// TestHandler_Upload_GeneratedRID exercises the upload endpoint that
// allocates a fresh RID on the server side.
func TestHandler_Upload_GeneratedRID(t *testing.T) {
	r, _ := newTestRouter(t)

	body := "hello world"
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/attachments/upload?filename=hello.txt",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	att := decodeAttachment(t, rec.Body)
	if !strings.HasPrefix(att.RID, "ri.attachments.main.attachment.") {
		t.Errorf("rid = %q, want ri.attachments.main.attachment. prefix", att.RID)
	}
	if att.Filename != "hello.txt" {
		t.Errorf("filename = %q", att.Filename)
	}
	if att.SizeBytes != int64(len(body)) {
		t.Errorf("sizeBytes = %d", att.SizeBytes)
	}
	if att.MediaType != "text/plain" {
		t.Errorf("mediaType = %q", att.MediaType)
	}
}

// TestHandler_Upload_MissingFilename rejects requests that omit the
// required ?filename query parameter.
func TestHandler_Upload_MissingFilename(t *testing.T) {
	r, _ := newTestRouter(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/attachments/upload",
		strings.NewReader("x"))
	req.Header.Set("Content-Type", "application/octet-stream")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandler_Upload_WithRID lets the caller pre-generate the RID.
func TestHandler_Upload_WithRID(t *testing.T) {
	r, _ := newTestRouter(t)

	rid := NewAttachmentRID()
	body := "payload"
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/attachments/upload/"+rid+"?filename=p.bin",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	att := decodeAttachment(t, rec.Body)
	if att.RID != rid {
		t.Errorf("rid = %q, want %q", att.RID, rid)
	}
	if att.Filename != "p.bin" {
		t.Errorf("filename = %q", att.Filename)
	}
	if att.SizeBytes != int64(len(body)) {
		t.Errorf("sizeBytes = %d", att.SizeBytes)
	}
}

// TestHandler_Upload_WithRID_Conflict — pre-generated RID already used.
func TestHandler_Upload_WithRID_Conflict(t *testing.T) {
	r, _ := newTestRouter(t)

	rid := NewAttachmentRID()
	path := "/api/v2/ontologies/attachments/upload/" + rid + "?filename=p.bin"

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("x"))
		req.Header.Set("Content-Type", "application/octet-stream")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if i == 0 {
			if rec.Code != http.StatusOK {
				t.Fatalf("first upload: status=%d body=%s", rec.Code, rec.Body.String())
			}
		} else {
			if rec.Code != http.StatusConflict {
				t.Errorf("second upload: status=%d, want 409, body=%s", rec.Code, rec.Body.String())
			}
		}
	}
}

// TestHandler_Upload_WithRID_InvalidRID — a RID that doesn't match the
// attachment prefix must be rejected with 400.
func TestHandler_Upload_WithRID_InvalidRID(t *testing.T) {
	r, _ := newTestRouter(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/attachments/upload/ri.foo.bar.baz?filename=p.bin",
		strings.NewReader("x"))
	req.Header.Set("Content-Type", "application/octet-stream")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandler_GetMetadata returns the JSON metadata record.
func TestHandler_GetMetadata(t *testing.T) {
	r, store := newTestRouter(t)
	att, err := store.Put(nil, BlobMeta{Filename: "m.txt", MediaType: "text/plain"}, strings.NewReader("meta-body"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/attachments/"+att.RID, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q, want json", ct)
	}
	got := decodeAttachment(t, rec.Body)
	if got.RID != att.RID {
		t.Errorf("rid mismatch")
	}
	if got.Filename != "m.txt" {
		t.Errorf("filename = %q", got.Filename)
	}
	if got.SizeBytes != int64(len("meta-body")) {
		t.Errorf("sizeBytes = %d", got.SizeBytes)
	}
}

// TestHandler_GetMetadata_NotFound — unknown RID returns 404.
func TestHandler_GetMetadata_NotFound(t *testing.T) {
	r, _ := newTestRouter(t)
	rid := NewAttachmentRID() // never uploaded

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/attachments/"+rid, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestHandler_GetContent returns the raw blob with the stored media type.
func TestHandler_GetContent(t *testing.T) {
	r, store := newTestRouter(t)
	body := []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0xFF}
	att, err := store.Put(nil, BlobMeta{Filename: "p.png", MediaType: "image/png"}, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/attachments/"+att.RID+"/content", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%x", rec.Code, rec.Body.Bytes())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Errorf("body bytes mismatch")
	}
}

// TestHandler_GetContent_NotFound — unknown RID returns 404.
func TestHandler_GetContent_NotFound(t *testing.T) {
	r, _ := newTestRouter(t)
	rid := NewAttachmentRID()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/attachments/"+rid+"/content", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
