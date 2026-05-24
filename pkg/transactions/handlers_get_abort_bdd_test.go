package transactions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/transactions"
)

// TestBDD_Transactions_GetAndAbort covers the round-59 fix for the
// PRD-V2 Transaction (preview) 55% gap. Before this round the
// OntologyTransaction surface exposed ONLY POST .../edits — callers
// could append edits but had no way to read them back without
// keeping a client-side mirror, and no way to abort an experiment
// short of restarting the server (transactions live in-memory and
// have no TTL today). The Foundry-side preview API ships at minimum
// {append, get, abort, commit}; commit needs full Funnel/Action
// integration so it's left for a future round, but get + abort are
// pure read/lifecycle operations that can land standalone.
//
// Wire shape (gated on the existing ?preview=true flag):
//
//   GET    /api/v2/ontologies/{o}/transactions/{tx}?preview=true
//     200 + {transactionId, totalEdits, edits} (edits in append order;
//                                                empty array for
//                                                unknown txn — same
//                                                "auto-created on first
//                                                use" semantic as POST)
//
//   DELETE /api/v2/ontologies/{o}/transactions/{tx}?preview=true
//     204 (idempotent; deleting an unknown txn is not an error so
//          retries are safe)
//
// Both endpoints reject missing ?preview=true with 400 PreviewRequired
// to match the POST contract.
func TestBDD_Transactions_GetAndAbort(t *testing.T) {
	const ontology = "onto"
	const txID = "tx-bdd-1"

	appendEdits := func(t *testing.T, store transactions.Store, edits []funnel.Edit) {
		t.Helper()
		err := store.AppendEdits(nil, transactions.Key{Ontology: ontology, TransactionID: txID}, edits)
		if err != nil {
			t.Fatalf("AppendEdits: %v", err)
		}
	}

	t.Run("GET returns appended edits in order", func(t *testing.T) {
		store := transactions.NewMemoryStore()
		appendEdits(t, store, []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "User", PrimaryKey: "u1"},
			{Type: funnel.EditTypeModify, ObjectType: "User", PrimaryKey: "u1"},
			{Type: funnel.EditTypeDelete, ObjectType: "User", PrimaryKey: "u2"},
		})

		r := newTestRouter(store)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontology+"/transactions/"+txID+"?preview=true", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			TransactionID string        `json:"transactionId"`
			TotalEdits    int           `json:"totalEdits"`
			Edits         []funnel.Edit `json:"edits"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.TransactionID != txID {
			t.Errorf("transactionId=%q, want %q", resp.TransactionID, txID)
		}
		if resp.TotalEdits != 3 || len(resp.Edits) != 3 {
			t.Fatalf("totalEdits=%d edits.len=%d, want 3/3", resp.TotalEdits, len(resp.Edits))
		}
		// Order preservation.
		if resp.Edits[0].Type != funnel.EditTypeCreate ||
			resp.Edits[1].Type != funnel.EditTypeModify ||
			resp.Edits[2].Type != funnel.EditTypeDelete {
			t.Errorf("order wrong: %v", resp.Edits)
		}
		if resp.Edits[2].PrimaryKey != "u2" {
			t.Errorf("edits[2].primaryKey=%q, want u2", resp.Edits[2].PrimaryKey)
		}
	})

	t.Run("GET for unknown txn returns {totalEdits:0, edits:[]}", func(t *testing.T) {
		// Auto-created-on-first-use semantic matches POST's behaviour.
		store := transactions.NewMemoryStore()
		r := newTestRouter(store)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontology+"/transactions/never-existed?preview=true", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			TotalEdits int           `json:"totalEdits"`
			Edits      []funnel.Edit `json:"edits"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp.TotalEdits != 0 {
			t.Errorf("totalEdits=%d, want 0", resp.TotalEdits)
		}
		// Empty array, not null — SDK clients iterate without nil-checks.
		if resp.Edits == nil {
			t.Errorf("edits is nil, want empty array")
		}
	})

	t.Run("DELETE removes the transaction; subsequent GET is empty", func(t *testing.T) {
		store := transactions.NewMemoryStore()
		appendEdits(t, store, []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "User", PrimaryKey: "u1"},
		})

		r := newTestRouter(store)
		// Sanity: GET shows the edit first.
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontology+"/transactions/"+txID+"?preview=true", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var pre struct{ TotalEdits int }
		_ = json.NewDecoder(rec.Body).Decode(&pre)
		if pre.TotalEdits != 1 {
			t.Fatalf("setup: totalEdits=%d, want 1", pre.TotalEdits)
		}

		// DELETE.
		delReq := httptest.NewRequest(http.MethodDelete,
			"/api/v2/ontologies/"+ontology+"/transactions/"+txID+"?preview=true", nil)
		delRec := httptest.NewRecorder()
		r.ServeHTTP(delRec, delReq)
		if delRec.Code != http.StatusNoContent {
			t.Fatalf("delete status=%d, want 204; body=%s", delRec.Code, delRec.Body.String())
		}

		// GET now shows zero edits.
		getReq := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontology+"/transactions/"+txID+"?preview=true", nil)
		getRec := httptest.NewRecorder()
		r.ServeHTTP(getRec, getReq)
		var post struct{ TotalEdits int }
		_ = json.NewDecoder(getRec.Body).Decode(&post)
		if post.TotalEdits != 0 {
			t.Errorf("after delete totalEdits=%d, want 0", post.TotalEdits)
		}
	})

	t.Run("DELETE on unknown transaction is idempotent (204, no error)", func(t *testing.T) {
		store := transactions.NewMemoryStore()
		r := newTestRouter(store)
		req := httptest.NewRequest(http.MethodDelete,
			"/api/v2/ontologies/"+ontology+"/transactions/never-existed?preview=true", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("status=%d, want 204 (idempotent on unknown txn)", rec.Code)
		}
	})

	t.Run("GET without preview=true returns 400 PreviewRequired", func(t *testing.T) {
		store := transactions.NewMemoryStore()
		r := newTestRouter(store)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+ontology+"/transactions/"+txID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]interface{}
		_ = json.NewDecoder(rec.Body).Decode(&body)
		if body["errorName"] != "PreviewRequired" {
			t.Errorf("errorName=%v, want PreviewRequired", body["errorName"])
		}
	})

	t.Run("DELETE without preview=true returns 400 PreviewRequired", func(t *testing.T) {
		store := transactions.NewMemoryStore()
		r := newTestRouter(store)
		req := httptest.NewRequest(http.MethodDelete,
			"/api/v2/ontologies/"+ontology+"/transactions/"+txID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Transactions in different ontologies are isolated", func(t *testing.T) {
		store := transactions.NewMemoryStore()
		_ = store.AppendEdits(nil, transactions.Key{Ontology: "ont-a", TransactionID: "shared"}, []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "User", PrimaryKey: "u1"},
		})
		_ = store.AppendEdits(nil, transactions.Key{Ontology: "ont-b", TransactionID: "shared"}, []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "User", PrimaryKey: "u2"},
			{Type: funnel.EditTypeCreate, ObjectType: "User", PrimaryKey: "u3"},
		})
		r := newTestRouter(store)

		check := func(ont string, want int) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/v2/ontologies/"+ont+"/transactions/shared?preview=true", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			var resp struct{ TotalEdits int }
			_ = json.NewDecoder(rec.Body).Decode(&resp)
			if resp.TotalEdits != want {
				t.Errorf("ontology %s totalEdits=%d, want %d", ont, resp.TotalEdits, want)
			}
		}
		check("ont-a", 1)
		check("ont-b", 2)
	})
}
