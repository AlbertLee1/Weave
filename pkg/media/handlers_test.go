package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

// memCatalog is a minimal in-memory MediaAssetStore that lets handler tests
// run without a PostgreSQL pool. Only the methods the HTTP handlers depend on
// are implemented; the unused ListByCreatedBy returns nil.
type memCatalog struct {
	mu   sync.Mutex
	rows map[string]*oms.MediaAsset
}

func newMemCatalog() *memCatalog { return &memCatalog{rows: map[string]*oms.MediaAsset{}} }

func (c *memCatalog) CreateMediaAsset(_ context.Context, a *oms.MediaAsset) error {
	if err := a.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.rows[a.RID]; ok {
		return fmt.Errorf("duplicate rid")
	}
	cp := *a
	c.rows[a.RID] = &cp
	return nil
}

func (c *memCatalog) GetMediaAsset(_ context.Context, rid string) (*oms.MediaAsset, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.rows[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (c *memCatalog) DeleteMediaAsset(_ context.Context, rid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.rows[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(c.rows, rid)
	return nil
}

func (c *memCatalog) CountBySHA256(_ context.Context, realm, sum string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, a := range c.rows {
		if a.Realm == realm && a.SHA256 == sum {
			n++
		}
	}
	return n, nil
}

func (c *memCatalog) ListByCreatedBy(_ context.Context, _ string, _ int) ([]oms.MediaAsset, error) {
	return nil, nil
}

func newTestRouter(t *testing.T) (*chi.Mux, *Store, *memCatalog, *Handler) {
	t.Helper()
	store := NewStore(t.TempDir())
	cat := newMemCatalog()
	h := NewHandler(store, cat)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, store, cat, h
}

// buildMultipart wraps bytes in a multipart/form-data body with a single file
// field plus optional realm form value.
func buildMultipart(t *testing.T, fieldName, filename, contentType string, body []byte, formFields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	buf := new(bytes.Buffer)
	mw := multipart.NewWriter(buf)
	for k, v := range formFields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name=%q; filename=%q`, fieldName, filename)}
	if contentType != "" {
		hdr["Content-Type"] = []string{contentType}
	}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatalf("part.Write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("mw.Close: %v", err)
	}
	return buf, mw.FormDataContentType()
}

func decodeMediaAsset(t *testing.T, body io.Reader) *oms.MediaAsset {
	t.Helper()
	var a oms.MediaAsset
	if err := json.NewDecoder(body).Decode(&a); err != nil {
		t.Fatalf("decode media asset: %v", err)
	}
	return &a
}

func TestHandler_Upload_HappyPath(t *testing.T) {
	r, store, cat, _ := newTestRouter(t)

	body := []byte("hello media")
	buf, ct := buildMultipart(t, "file", "hello.txt", "text/plain", body, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/media", buf)
	req.Header.Set("Content-Type", ct)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := decodeMediaAsset(t, rec.Body)
	if !strings.HasPrefix(got.RID, MediaRIDPrefix) {
		t.Errorf("rid = %q, want %s* prefix", got.RID, MediaRIDPrefix)
	}
	if got.Filename != "hello.txt" {
		t.Errorf("filename = %q", got.Filename)
	}
	if got.SizeBytes != int64(len(body)) {
		t.Errorf("sizeBytes = %d", got.SizeBytes)
	}
	if got.MIME != "text/plain" {
		t.Errorf("mime = %q", got.MIME)
	}
	if got.Realm != "main" {
		t.Errorf("realm = %q, want default 'main'", got.Realm)
	}
	row, err := cat.GetMediaAsset(context.Background(), got.RID)
	if err != nil {
		t.Fatalf("catalog GetMediaAsset: %v", err)
	}
	if row.SHA256 != got.SHA256 || row.Path != got.Path {
		t.Errorf("catalog row diverged from response")
	}
	exists, err := store.Exists(got.Path)
	if err != nil || !exists {
		t.Errorf("blob not on disk: exists=%v err=%v", exists, err)
	}
}

func TestHandler_Upload_CustomRealm(t *testing.T) {
	r, _, _, _ := newTestRouter(t)

	buf, ct := buildMultipart(t, "file", "x.bin", "", []byte("x"), map[string]string{"realm": "tenant-a"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/media", buf)
	req.Header.Set("Content-Type", ct)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := decodeMediaAsset(t, rec.Body)
	if got.Realm != "tenant-a" {
		t.Errorf("realm = %q", got.Realm)
	}
}

func TestHandler_Upload_InvalidRealm(t *testing.T) {
	r, _, _, _ := newTestRouter(t)
	buf, ct := buildMultipart(t, "file", "x.bin", "", []byte("x"), map[string]string{"realm": "../escape"})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/media", buf)
	req.Header.Set("Content-Type", ct)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_Upload_MissingFile(t *testing.T) {
	r, _, _, _ := newTestRouter(t)
	buf := new(bytes.Buffer)
	mw := multipart.NewWriter(buf)
	_ = mw.WriteField("realm", "main")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v2/media", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_Upload_TooLarge(t *testing.T) {
	r, _, _, h := newTestRouter(t)
	h.SetMaxUploadBytes(64) // tiny cap so we don't allocate 10MB in tests

	body := bytes.Repeat([]byte{0x01}, 1024)
	buf, ct := buildMultipart(t, "file", "big.bin", "application/octet-stream", body, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/media", buf)
	req.Header.Set("Content-Type", ct)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandler_Upload_DefaultMaxIs10MB(t *testing.T) {
	_, _, _, h := newTestRouter(t)
	if h.maxUploadBytes != DefaultMaxUploadBytes {
		t.Errorf("default max = %d, want %d", h.maxUploadBytes, DefaultMaxUploadBytes)
	}
	if DefaultMaxUploadBytes != 10*1024*1024 {
		t.Errorf("DefaultMaxUploadBytes = %d, want 10MB", DefaultMaxUploadBytes)
	}
}

func TestHandler_Download_HappyPath(t *testing.T) {
	r, _, _, _ := newTestRouter(t)

	body := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff}
	buf, ct := buildMultipart(t, "file", "logo.png", "image/png", body, nil)
	upReq := httptest.NewRequest(http.MethodPost, "/api/v2/media", buf)
	upReq.Header.Set("Content-Type", ct)
	upRec := httptest.NewRecorder()
	r.ServeHTTP(upRec, upReq)
	if upRec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", upRec.Code, upRec.Body.String())
	}
	asset := decodeMediaAsset(t, upRec.Body)

	dlReq := httptest.NewRequest(http.MethodGet, "/api/v2/media/"+asset.RID, nil)
	dlRec := httptest.NewRecorder()
	r.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusOK {
		t.Fatalf("download status = %d, body=%x", dlRec.Code, dlRec.Body.Bytes())
	}
	if ct := dlRec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	cd := dlRec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "logo.png") {
		t.Errorf("Content-Disposition = %q, want attachment + filename", cd)
	}
	if !bytes.Equal(dlRec.Body.Bytes(), body) {
		t.Errorf("body bytes mismatch: got=%x want=%x", dlRec.Body.Bytes(), body)
	}
}

func TestHandler_Download_NotFound(t *testing.T) {
	r, _, _, _ := newTestRouter(t)
	rid := NewMediaRID()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/media/"+rid, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_Delete_LastReferenceReclaimsBlob(t *testing.T) {
	r, store, _, _ := newTestRouter(t)

	body := []byte("dedup me")
	buf, ct := buildMultipart(t, "file", "a.txt", "text/plain", body, nil)
	upReq := httptest.NewRequest(http.MethodPost, "/api/v2/media", buf)
	upReq.Header.Set("Content-Type", ct)
	upRec := httptest.NewRecorder()
	r.ServeHTTP(upRec, upReq)
	if upRec.Code != http.StatusOK {
		t.Fatalf("upload: %s", upRec.Body.String())
	}
	asset := decodeMediaAsset(t, upRec.Body)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v2/media/"+asset.RID, nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%s", delRec.Code, delRec.Body.String())
	}
	exists, err := store.Exists(asset.Path)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Errorf("blob still on disk after last-reference delete")
	}
}

func TestHandler_Delete_DedupedBlobKept(t *testing.T) {
	r, store, _, _ := newTestRouter(t)

	body := []byte("share me")
	upload := func() *oms.MediaAsset {
		buf, ct := buildMultipart(t, "file", "shared.txt", "text/plain", body, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/v2/media", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("upload: %s", rec.Body.String())
		}
		return decodeMediaAsset(t, rec.Body)
	}

	a := upload()
	b := upload()
	if a.RID == b.RID {
		t.Fatalf("duplicate uploads should produce distinct RIDs")
	}
	if a.Path != b.Path {
		t.Fatalf("duplicate uploads should share the content-addressed path")
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v2/media/"+a.RID, nil)
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete first: status=%d", delRec.Code)
	}
	exists, err := store.Exists(b.Path)
	if err != nil || !exists {
		t.Errorf("blob removed too early; exists=%v err=%v", exists, err)
	}

	delReq2 := httptest.NewRequest(http.MethodDelete, "/api/v2/media/"+b.RID, nil)
	delRec2 := httptest.NewRecorder()
	r.ServeHTTP(delRec2, delReq2)
	if delRec2.Code != http.StatusNoContent {
		t.Fatalf("delete second: status=%d", delRec2.Code)
	}
	exists, err = store.Exists(b.Path)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Errorf("blob still present after final reference deleted")
	}
}

func TestHandler_Delete_NotFound(t *testing.T) {
	r, _, _, _ := newTestRouter(t)
	rid := NewMediaRID()
	req := httptest.NewRequest(http.MethodDelete, "/api/v2/media/"+rid, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
