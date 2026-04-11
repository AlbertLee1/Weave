package transactions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/transactions"
)

func newTestRouter(store transactions.Store) *chi.Mux {
	r := chi.NewRouter()
	h := transactions.NewHandler(store)
	h.RegisterRoutes(r)
	return r
}

func doPost(t *testing.T, r http.Handler, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestApplyTransactionEdits_Success(t *testing.T) {
	store := transactions.NewMemoryStore()
	r := newTestRouter(store)

	body := map[string]interface{}{
		"edits": []map[string]interface{}{
			{"type": "CREATE", "objectType": "User", "primaryKey": "u1", "properties": map[string]interface{}{"name": "alice"}},
			{"type": "MODIFY", "objectType": "User", "primaryKey": "u1", "properties": map[string]interface{}{"name": "alice2"}},
		},
	}
	rec := doPost(t, r, "/api/v2/ontologies/onto/transactions/tx-1/edits?preview=true", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		TransactionID string `json:"transactionId"`
		AppendedEdits int    `json:"appendedEdits"`
		TotalEdits    int    `json:"totalEdits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.TransactionID != "tx-1" || resp.AppendedEdits != 2 || resp.TotalEdits != 2 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestApplyTransactionEdits_RequiresPreview(t *testing.T) {
	store := transactions.NewMemoryStore()
	r := newTestRouter(store)

	body := map[string]interface{}{
		"edits": []map[string]interface{}{
			{"type": "CREATE", "objectType": "User", "primaryKey": "u1"},
		},
	}
	rec := doPost(t, r, "/api/v2/ontologies/onto/transactions/tx-1/edits", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("PreviewRequired")) {
		t.Fatalf("expected PreviewRequired error, got %s", rec.Body.String())
	}
}

func TestApplyTransactionEdits_EmptyEdits(t *testing.T) {
	store := transactions.NewMemoryStore()
	r := newTestRouter(store)

	rec := doPost(t, r, "/api/v2/ontologies/onto/transactions/tx-1/edits?preview=true",
		map[string]interface{}{"edits": []interface{}{}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("NoEditsProvided")) {
		t.Fatalf("expected NoEditsProvided error, got %s", rec.Body.String())
	}
}

func TestApplyTransactionEdits_InvalidEditType(t *testing.T) {
	store := transactions.NewMemoryStore()
	r := newTestRouter(store)

	body := map[string]interface{}{
		"edits": []map[string]interface{}{
			{"type": "UPSERT", "objectType": "User", "primaryKey": "u1"},
		},
	}
	rec := doPost(t, r, "/api/v2/ontologies/onto/transactions/tx-1/edits?preview=true", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("InvalidEditType")) {
		t.Fatalf("expected InvalidEditType, got %s", rec.Body.String())
	}
}

func TestApplyTransactionEdits_MultipleAppends(t *testing.T) {
	store := transactions.NewMemoryStore()
	r := newTestRouter(store)

	first := map[string]interface{}{
		"edits": []map[string]interface{}{
			{"type": "CREATE", "objectType": "User", "primaryKey": "u1"},
		},
	}
	rec := doPost(t, r, "/api/v2/ontologies/onto/transactions/tx-multi/edits?preview=true", first)
	if rec.Code != http.StatusOK {
		t.Fatalf("first POST: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	second := map[string]interface{}{
		"edits": []map[string]interface{}{
			{"type": "CREATE", "objectType": "User", "primaryKey": "u2"},
			{"type": "CREATE", "objectType": "User", "primaryKey": "u3"},
		},
	}
	rec = doPost(t, r, "/api/v2/ontologies/onto/transactions/tx-multi/edits?preview=true", second)
	if rec.Code != http.StatusOK {
		t.Fatalf("second POST: want 200, got %d", rec.Code)
	}

	var resp struct {
		AppendedEdits int `json:"appendedEdits"`
		TotalEdits    int `json:"totalEdits"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.AppendedEdits != 2 || resp.TotalEdits != 3 {
		t.Fatalf("append accumulation broken: %+v", resp)
	}
}
