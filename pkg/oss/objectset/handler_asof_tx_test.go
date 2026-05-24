package objectset_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakeTxResolver is the in-memory test double for the US-379
// TransactionResolver hook. data maps a txID to the committedAt the
// resolver should return; missing keys surface as ErrTransactionNotFound
// so the handler can render a clean TransactionNotFound 400 envelope.
// err is set to short-circuit every call with the same error (used to
// drive the TimeTravelFailed branch).
type fakeTxResolver struct {
	data  map[string]time.Time
	err   error
	calls []string
}

func newFakeTxResolver() *fakeTxResolver {
	return &fakeTxResolver{data: map[string]time.Time{}}
}

func (f *fakeTxResolver) ResolveTransaction(_ context.Context, txID string) (time.Time, error) {
	f.calls = append(f.calls, txID)
	if f.err != nil {
		return time.Time{}, f.err
	}
	if ts, ok := f.data[txID]; ok {
		return ts, nil
	}
	return time.Time{}, objectset.ErrTransactionNotFound
}

// TestLoadObjects_AsOfTx_RoutesThroughResolver is the US-379 happy path:
// ?asOf=tx-<id> is resolved to the transaction's committed_at, then routed
// through the existing US-223 history-snapshot scan with the resolved
// timestamp. The provider receives the resolved time, not the raw tx-id
// string, so the rest of the time-travel pipeline stays oblivious to the
// new wire format.
func TestLoadObjects_AsOfTx_RoutesThroughResolver(t *testing.T) {
	prov := newFakeSnapshotProvider()
	wantTS, _ := time.Parse(time.RFC3339, "2026-01-15T00:00:00Z")
	prov.asOfData["2026-01-15T00:00:00Z"] = []objectset.ObjectSnapshot{
		{PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
	}
	tx := newFakeTxResolver()
	tx.data["tx-001"] = wantTS

	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)
	h.SetTransactionResolver(tx)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=tx-001",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if len(tx.calls) != 1 || tx.calls[0] != "tx-001" {
		t.Errorf("resolver calls = %v, want [tx-001]", tx.calls)
	}
	if len(prov.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(prov.calls))
	}
	if !prov.calls[0].AsOf.Equal(wantTS) {
		t.Errorf("provider asOf = %v, want %v", prov.calls[0].AsOf, wantTS)
	}
	resp := decodeJSON[struct {
		Data []map[string]interface{} `json:"data"`
	}](t, rr.Body.Bytes())
	if len(resp.Data) != 1 || resp.Data[0]["name"] != "Alice" {
		t.Errorf("data = %v, want one row with name=Alice", resp.Data)
	}
}

// TestLoadObjects_AsOfTx_NoResolverReturnsLookupUnavailable verifies the
// degraded-mode contract: when no resolver is wired (PG-less router) the
// tx-id branch returns a documented 400 instead of falling through to the
// RFC3339 parser, which would otherwise reject "tx-..." as invalid and
// leak the wrong error name.
func TestLoadObjects_AsOfTx_NoResolverReturnsLookupUnavailable(t *testing.T) {
	prov := newFakeSnapshotProvider()
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov) // history snapshot wired, but tx resolver is not

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=tx-anything",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName string `json:"errorName"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "TransactionLookupUnavailable" {
		t.Errorf("errorName = %q, want TransactionLookupUnavailable", apiErr.ErrorName)
	}
	if len(prov.calls) != 0 {
		t.Errorf("provider must not be called when resolver is missing; got %d", len(prov.calls))
	}
}

// TestLoadObjects_AsOfTx_UnknownTxReturnsTransactionNotFound covers the
// missing-row branch: a syntactically valid tx-id with no matching row
// surfaces as a clean TransactionNotFound 400 rather than the generic
// TimeTravelFailed envelope.
func TestLoadObjects_AsOfTx_UnknownTxReturnsTransactionNotFound(t *testing.T) {
	prov := newFakeSnapshotProvider()
	tx := newFakeTxResolver() // empty data → every lookup ErrTransactionNotFound
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)
	h.SetTransactionResolver(tx)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=tx-missing",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "TransactionNotFound" {
		t.Errorf("errorName = %q, want TransactionNotFound", apiErr.ErrorName)
	}
	if apiErr.Parameters["txId"] != "tx-missing" {
		t.Errorf("parameters.txId = %q, want tx-missing", apiErr.Parameters["txId"])
	}
}

// TestLoadObjects_AsOfTx_PropagatesResolverError forces the resolver to
// return an unrecognised error (not ErrTransactionNotFound). The handler
// surfaces it as the generic TimeTravelFailed envelope at HTTP 500
// INTERNAL — a downstream TransactionResolver failure is server-side,
// NOT bad user input. The sentinel ErrTransactionNotFound case keeps
// its dedicated 400 TransactionNotFound envelope so callers can still
// distinguish "you asked for a tx id that does not exist" from "the
// server can't reach its tx store".
func TestLoadObjects_AsOfTx_PropagatesResolverError(t *testing.T) {
	prov := newFakeSnapshotProvider()
	tx := newFakeTxResolver()
	tx.err = errors.New("resolver unreachable")
	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)
	h.SetTransactionResolver(tx)

	body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
	req := httptest.NewRequest("POST",
		"/api/v2/ontologies/test/objectSets/loadObjects?asOf=tx-001",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	newAsOfRouter(t, h).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rr.Code, rr.Body.String())
	}
	apiErr := decodeJSON[struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}](t, rr.Body.Bytes())
	if apiErr.ErrorName != "TimeTravelFailed" {
		t.Errorf("errorName = %q, want TimeTravelFailed", apiErr.ErrorName)
	}
	if !strings.Contains(apiErr.Parameters["error"], "resolver unreachable") {
		t.Errorf("parameters.error = %q, want it to mention resolver unreachable", apiErr.Parameters["error"])
	}
}

// TestLoadObjects_AsOfTx_TimeTravelEndToEnd is the PRD-specified
// "t1 写→t2 改→asOf=t1 返回原值" check: stage two snapshots representing
// pre-modification (tx-1) and post-modification (tx-2) states; verify
// asOf=tx-1 returns the original value while asOf=tx-2 returns the
// modified value. The fake provider keys snapshots by RFC3339 timestamp
// the resolver hands back, mirroring what the production [valid_from,
// valid_to) scan delivers.
func TestLoadObjects_AsOfTx_TimeTravelEndToEnd(t *testing.T) {
	prov := newFakeSnapshotProvider()
	t1, _ := time.Parse(time.RFC3339, "2026-01-15T00:00:00Z")
	t2, _ := time.Parse(time.RFC3339, "2026-01-15T01:00:00Z")
	prov.asOfData[t1.Format(time.RFC3339)] = []objectset.ObjectSnapshot{
		{PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
	}
	prov.asOfData[t2.Format(time.RFC3339)] = []objectset.ObjectSnapshot{
		{PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice2"}},
	}
	tx := newFakeTxResolver()
	tx.data["tx-1"] = t1
	tx.data["tx-2"] = t2

	store := objectset.NewStore(0)
	h := objectset.NewHandler(nil, nil, store)
	h.SetHistorySnapshotProvider(prov)
	h.SetTransactionResolver(tx)
	router := newAsOfRouter(t, h)

	loadAt := func(t *testing.T, asOf string) string {
		t.Helper()
		body := `{"objectSet":{"type":"base","objectType":"Employee"},"select":["name"]}`
		req := httptest.NewRequest("POST",
			"/api/v2/ontologies/test/objectSets/loadObjects?asOf="+asOf,
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
		}
		resp := decodeJSON[struct {
			Data []map[string]interface{} `json:"data"`
		}](t, rr.Body.Bytes())
		if len(resp.Data) != 1 {
			t.Fatalf("data len = %d, want 1", len(resp.Data))
		}
		name, _ := resp.Data[0]["name"].(string)
		return name
	}

	if name := loadAt(t, "tx-1"); name != "Alice" {
		t.Errorf("asOf=tx-1 returned name=%q, want Alice (the original value)", name)
	}
	if name := loadAt(t, "tx-2"); name != "Alice2" {
		t.Errorf("asOf=tx-2 returned name=%q, want Alice2 (the modified value)", name)
	}
}
