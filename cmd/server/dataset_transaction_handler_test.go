package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakeOntologyResolver satisfies datasetOntologyResolver for handler tests.
// rid → api name mapping is keyed off the RID-or-api-name input so a single
// fake covers both lookup shapes.
type fakeOntologyResolver struct {
	byInput map[string]*oms.Ontology
}

func (f *fakeOntologyResolver) GetOntology(_ context.Context, ridOrApiName string) (*oms.Ontology, error) {
	o, ok := f.byInput[ridOrApiName]
	if !ok {
		return nil, oms.ErrNotFound
	}
	return o, nil
}

// fakeDatasetTxStore satisfies datasetTransactionLister with in-memory rows.
type fakeDatasetTxStore struct {
	byID  map[string]*oms.DatasetTransaction
	byOnt map[string][]oms.DatasetTransaction
}

func newFakeDatasetTxStore() *fakeDatasetTxStore {
	return &fakeDatasetTxStore{
		byID:  map[string]*oms.DatasetTransaction{},
		byOnt: map[string][]oms.DatasetTransaction{},
	}
}

func (s *fakeDatasetTxStore) GetDatasetTransaction(_ context.Context, txID string) (*oms.DatasetTransaction, error) {
	tx, ok := s.byID[txID]
	if !ok {
		return nil, oms.ErrNotFound
	}
	return tx, nil
}

func (s *fakeDatasetTxStore) LatestForOntology(_ context.Context, ontologyAPIName string) (*oms.DatasetTransaction, error) {
	rows := s.byOnt[ontologyAPIName]
	if len(rows) == 0 {
		return nil, nil
	}
	out := rows[0]
	return &out, nil
}

func (s *fakeDatasetTxStore) ListByOntology(_ context.Context, ontologyAPIName string, limit int) ([]oms.DatasetTransaction, error) {
	rows := s.byOnt[ontologyAPIName]
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]oms.DatasetTransaction, len(rows))
	copy(out, rows)
	return out, nil
}

func newDatasetHistoryRouter(repo datasetOntologyResolver, store datasetTransactionLister) http.Handler {
	r := chi.NewRouter()
	h := newDatasetHistoryHandler(repo, store)
	r.Get("/api/v2/datasets/{rid}/history", h.History)
	return r
}

// TestDatasetHistory_ReturnsChainOrderedByCommittedAtDesc covers the happy
// path: a multi-row chain returns newest-first, mirroring ListByOntology's
// committed_at-DESC ordering.
func TestDatasetHistory_ReturnsChainOrderedByCommittedAtDesc(t *testing.T) {
	t1 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	store := newFakeDatasetTxStore()
	store.byOnt["test"] = []oms.DatasetTransaction{
		{TxID: "tx-2", ParentTxID: "tx-1", OntologyAPIName: "test", CommittedAt: t2, EditsCount: 3, UserID: "alice"},
		{TxID: "tx-1", OntologyAPIName: "test", CommittedAt: t1, EditsCount: 1, UserID: "alice"},
	}
	repo := &fakeOntologyResolver{byInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v2/datasets/test/history", nil)
	newDatasetHistoryRouter(repo, store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp historyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rr.Body.String())
	}
	if len(resp.Transactions) != 2 {
		t.Fatalf("transactions = %d, want 2", len(resp.Transactions))
	}
	if resp.Transactions[0].TxID != "tx-2" {
		t.Errorf("transactions[0].TxID = %q, want tx-2 (newest first)", resp.Transactions[0].TxID)
	}
	if resp.Transactions[1].ParentTxID != "" {
		t.Errorf("transactions[1].ParentTxID = %q, want empty (genesis)", resp.Transactions[1].ParentTxID)
	}
	if resp.Transactions[0].EditsCount != 3 {
		t.Errorf("transactions[0].EditsCount = %d, want 3", resp.Transactions[0].EditsCount)
	}
}

// TestDatasetHistory_NotFound returns a 404 envelope when {rid} resolves
// to no ontology.
func TestDatasetHistory_NotFound(t *testing.T) {
	store := newFakeDatasetTxStore()
	repo := &fakeOntologyResolver{byInput: map[string]*oms.Ontology{}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v2/datasets/missing/history", nil)
	newDatasetHistoryRouter(repo, store).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.ErrorName != "DatasetNotFound" {
		t.Errorf("errorName = %q, want DatasetNotFound", apiErr.ErrorName)
	}
}

// TestDatasetHistory_DegradedMode returns DatasetHistoryUnavailable 400
// when the store is unwired (degraded-mode router without PG).
func TestDatasetHistory_DegradedMode(t *testing.T) {
	repo := &fakeOntologyResolver{byInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v2/datasets/test/history", nil)
	newDatasetHistoryRouter(repo, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if apiErr.ErrorName != "DatasetHistoryUnavailable" {
		t.Errorf("errorName = %q, want DatasetHistoryUnavailable", apiErr.ErrorName)
	}
}

// TestDatasetHistory_EmptyChainReturnsEmptyList verifies a known ontology
// with no transactions yet renders as an empty list (not an error). This
// is the chain-genesis case and matches what the funnel consumer produces
// before the first batch is applied.
func TestDatasetHistory_EmptyChainReturnsEmptyList(t *testing.T) {
	store := newFakeDatasetTxStore()
	repo := &fakeOntologyResolver{byInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v2/datasets/test/history", nil)
	newDatasetHistoryRouter(repo, store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp historyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Transactions == nil {
		t.Errorf("transactions field must be non-nil empty slice, got nil")
	}
	if len(resp.Transactions) != 0 {
		t.Errorf("transactions len = %d, want 0", len(resp.Transactions))
	}
}

// TestDatasetHistory_CappedResponseReportsTruncated covers the operator-facing
// Time Travel contract: a capped chain must say it is partial instead of
// letting clients mistake the newest 1000 rows for the full audit history.
func TestDatasetHistory_CappedResponseReportsTruncated(t *testing.T) {
	store := newFakeDatasetTxStore()
	base := time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 1001; i++ {
		store.byOnt["test"] = append(store.byOnt["test"], oms.DatasetTransaction{
			TxID:            fmt.Sprintf("tx-%04d", 1001-i),
			OntologyAPIName: "test",
			CommittedAt:     base.Add(-time.Duration(i) * time.Minute),
			EditsCount:      1,
		})
	}
	repo := &fakeOntologyResolver{byInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v2/datasets/test/history", nil)
	newDatasetHistoryRouter(repo, store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Transactions []oms.DatasetTransaction `json:"transactions"`
		Truncated    bool                     `json:"truncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rr.Body.String())
	}
	if !resp.Truncated {
		t.Fatalf("truncated = false, want true; body = %s", rr.Body.String())
	}
	if len(resp.Transactions) != 1000 {
		t.Fatalf("transactions = %d, want 1000", len(resp.Transactions))
	}
	if resp.Transactions[0].TxID != "tx-1001" {
		t.Errorf("first tx = %q, want newest tx-1001", resp.Transactions[0].TxID)
	}
	if resp.Transactions[999].TxID != "tx-0002" {
		t.Errorf("last tx = %q, want tx-0002 so tx-0001 is hidden by cap", resp.Transactions[999].TxID)
	}
}

func TestDatasetHistory_UncappedResponseReportsComplete(t *testing.T) {
	store := newFakeDatasetTxStore()
	store.byOnt["test"] = []oms.DatasetTransaction{
		{TxID: "tx-2", OntologyAPIName: "test", CommittedAt: time.Date(2026, 5, 19, 8, 1, 0, 0, time.UTC), EditsCount: 1},
		{TxID: "tx-1", OntologyAPIName: "test", CommittedAt: time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC), EditsCount: 1},
	}
	repo := &fakeOntologyResolver{byInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v2/datasets/test/history", nil)
	newDatasetHistoryRouter(repo, store).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Transactions []oms.DatasetTransaction `json:"transactions"`
		Truncated    bool                     `json:"truncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rr.Body.String())
	}
	if resp.Truncated {
		t.Fatalf("truncated = true, want false; body = %s", rr.Body.String())
	}
	if len(resp.Transactions) != 2 {
		t.Fatalf("transactions = %d, want 2", len(resp.Transactions))
	}
}

// TestDatasetTransactionResolver_RoutesNotFoundSentinel verifies the
// adapter maps oms.ErrNotFound onto objectset.ErrTransactionNotFound so
// the OSS handler can render a clean TransactionNotFound 400 envelope
// (rather than the generic TimeTravelFailed wrapper).
func TestDatasetTransactionResolver_RoutesNotFoundSentinel(t *testing.T) {
	store := newFakeDatasetTxStore()
	resolver := newDatasetTransactionResolverAdapter(store)

	_, err := resolver.ResolveTransaction(context.Background(), "tx-missing")
	if !errors.Is(err, objectset.ErrTransactionNotFound) {
		t.Fatalf("error = %v, want ErrTransactionNotFound", err)
	}
}

// TestDatasetTransactionResolver_ReturnsCommittedAt covers the happy path:
// the adapter unwraps the dataset_transactions row and returns its
// committed_at directly so the OSS handler can route it through the
// existing US-223 history-snapshot scan.
func TestDatasetTransactionResolver_ReturnsCommittedAt(t *testing.T) {
	want := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	store := newFakeDatasetTxStore()
	store.byID["tx-001"] = &oms.DatasetTransaction{
		TxID: "tx-001", OntologyAPIName: "test", CommittedAt: want, EditsCount: 1,
	}
	resolver := newDatasetTransactionResolverAdapter(store)

	got, err := resolver.ResolveTransaction(context.Background(), "tx-001")
	if err != nil {
		t.Fatalf("resolver error: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("got = %v, want %v", got, want)
	}
}
