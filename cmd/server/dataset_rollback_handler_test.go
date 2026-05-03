package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
)

// rollbackOntologyResolver is the OMS surface the rollback handler
// depends on for both URL {rid} → ontology lookup AND ObjectType resolution
// during the per-PK replay. The test fake stocks both maps so a single
// fixture covers both lookup shapes.
type rollbackOntologyResolver struct {
	byOntologyInput map[string]*oms.Ontology
	byObjectTypeRID map[string]*oms.ObjectType
}

func (f *rollbackOntologyResolver) GetOntology(_ context.Context, ridOrApiName string) (*oms.Ontology, error) {
	o, ok := f.byOntologyInput[ridOrApiName]
	if !ok {
		return nil, oms.ErrNotFound
	}
	return o, nil
}

func (f *rollbackOntologyResolver) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	ot, ok := f.byObjectTypeRID[rid]
	if !ok {
		return nil, oms.ErrNotFound
	}
	return ot, nil
}

// fakeDatasetTxWriter satisfies datasetTransactionWriter end-to-end —
// chains, latest, ListAfterCommittedAt, MarkRolledBack, and the recorder
// hook. records: chronological insertion order so RecordDatasetTransaction
// preserves the chain head computation.
type fakeDatasetTxWriter struct {
	byID         map[string]*oms.DatasetTransaction
	byOntology   map[string][]*oms.DatasetTransaction // newest-first
	recordCalls  int
	markCalls    map[string]markCall
	failOnRecord bool
}

type markCall struct {
	rolledBackToTxID string
	rolledBackAt     time.Time
}

func newFakeDatasetTxWriter() *fakeDatasetTxWriter {
	return &fakeDatasetTxWriter{
		byID:       map[string]*oms.DatasetTransaction{},
		byOntology: map[string][]*oms.DatasetTransaction{},
		markCalls:  map[string]markCall{},
	}
}

func (s *fakeDatasetTxWriter) seed(tx *oms.DatasetTransaction) {
	clone := *tx
	s.byID[tx.TxID] = &clone
	s.byOntology[tx.OntologyAPIName] = append(s.byOntology[tx.OntologyAPIName], &clone)
	sort.SliceStable(s.byOntology[tx.OntologyAPIName], func(i, j int) bool {
		return s.byOntology[tx.OntologyAPIName][i].CommittedAt.After(s.byOntology[tx.OntologyAPIName][j].CommittedAt)
	})
}

func (s *fakeDatasetTxWriter) GetDatasetTransaction(_ context.Context, txID string) (*oms.DatasetTransaction, error) {
	tx, ok := s.byID[txID]
	if !ok {
		return nil, oms.ErrNotFound
	}
	return tx, nil
}

func (s *fakeDatasetTxWriter) LatestForOntology(_ context.Context, ontologyAPIName string) (*oms.DatasetTransaction, error) {
	rows := s.byOntology[ontologyAPIName]
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *fakeDatasetTxWriter) ListByOntology(_ context.Context, ontologyAPIName string, limit int) ([]oms.DatasetTransaction, error) {
	rows := s.byOntology[ontologyAPIName]
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]oms.DatasetTransaction, len(rows))
	for i, p := range rows {
		out[i] = *p
	}
	return out, nil
}

func (s *fakeDatasetTxWriter) RecordDatasetTransaction(_ context.Context, tx *oms.DatasetTransaction) error {
	if s.failOnRecord {
		return errors.New("record failed")
	}
	s.recordCalls++
	clone := *tx
	s.byID[tx.TxID] = &clone
	s.byOntology[tx.OntologyAPIName] = append([]*oms.DatasetTransaction{&clone}, s.byOntology[tx.OntologyAPIName]...)
	return nil
}

func (s *fakeDatasetTxWriter) ListAfterCommittedAt(_ context.Context, ontologyAPIName string, after time.Time) ([]oms.DatasetTransaction, error) {
	rows := s.byOntology[ontologyAPIName]
	out := []oms.DatasetTransaction{}
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].CommittedAt.After(after) {
			out = append(out, *rows[i])
		}
	}
	return out, nil
}

func (s *fakeDatasetTxWriter) MarkRolledBack(_ context.Context, txID, rolledBackToTxID string, at time.Time) error {
	tx, ok := s.byID[txID]
	if !ok {
		return oms.ErrNotFound
	}
	tx.RolledBackAt = at
	tx.RolledBackToTxID = rolledBackToTxID
	s.markCalls[txID] = markCall{rolledBackToTxID: rolledBackToTxID, rolledBackAt: at}
	return nil
}

// fakeAffectedStore returns hard-coded (objectType, pk) tuples so each
// rollback test can isolate exactly which docs the replay touches.
type fakeAffectedStore struct {
	byOntologyAfter map[string][]oms.AffectedKey
	err             error
}

func (s *fakeAffectedStore) ListAffectedKeysSince(_ context.Context, ontologyRID string, _ time.Time) ([]oms.AffectedKey, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.byOntologyAfter[ontologyRID], nil
}

// fakeHistorySnapshot serves a per-ObjectType snapshot map. Missing PKs
// surface as "object did not exist at asOf" — the rollback handler should
// delete those from the live index.
type fakeHistorySnapshot struct {
	byObjectType map[string][]oms.LatestObjectState
	err          error
}

func (s *fakeHistorySnapshot) SnapshotObjectsAt(_ context.Context, objectTypeRID string, _ time.Time) ([]oms.LatestObjectState, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.byObjectType[objectTypeRID], nil
}

// recordingIndex captures Index/DeleteDocument calls so tests can assert
// exactly what the rollback wrote. Keyed by scopedKey + pk.
type recordingIndex struct {
	indexed map[string]map[string]map[string]interface{}
	deleted map[string]map[string]bool
}

func newRecordingIndex() *recordingIndex {
	return &recordingIndex{
		indexed: map[string]map[string]map[string]interface{}{},
		deleted: map[string]map[string]bool{},
	}
}

func (r *recordingIndex) IndexDocument(scopedKey, pk string, doc map[string]interface{}) error {
	if r.indexed[scopedKey] == nil {
		r.indexed[scopedKey] = map[string]map[string]interface{}{}
	}
	r.indexed[scopedKey][pk] = doc
	return nil
}

func (r *recordingIndex) DeleteDocument(scopedKey, pk string) error {
	if r.deleted[scopedKey] == nil {
		r.deleted[scopedKey] = map[string]bool{}
	}
	r.deleted[scopedKey][pk] = true
	return nil
}

func newDatasetRollbackRouter(
	repo datasetRollbackOntologyRepo,
	store datasetTransactionWriter,
	affected datasetRollbackAffectedStore,
	history datasetRollbackSnapshot,
	indexMgr indexWriter,
) http.Handler {
	r := chi.NewRouter()
	h := newDatasetRollbackHandler(repo, store, affected, history, indexMgr)
	r.Post("/api/v2/datasets/{rid}/transactions", h.CreateTransaction)
	r.Post("/api/v2/datasets/{rid}/rollback", h.Rollback)
	return r
}

// TestCreateTransaction_StampsParentFromChainHead verifies the new
// checkpoint tx points at the prior chain head and is returned as 201
// JSON.
func TestCreateTransaction_StampsParentFromChainHead(t *testing.T) {
	store := newFakeDatasetTxWriter()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.seed(&oms.DatasetTransaction{
		TxID:            "tx-existing-head",
		OntologyAPIName: "test",
		CommittedAt:     t1,
		EditsCount:      5,
	})
	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}

	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"userId":"alice","editsCount":0}`)
	req := httptest.NewRequest("POST", "/api/v2/datasets/test/transactions", body)
	req.Header.Set("Content-Type", "application/json")
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rr.Code, rr.Body.String())
	}
	var tx oms.DatasetTransaction
	if err := json.Unmarshal(rr.Body.Bytes(), &tx); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rr.Body.String())
	}
	if !strings.HasPrefix(tx.TxID, "tx-") {
		t.Errorf("TxID = %q, want tx- prefix", tx.TxID)
	}
	if tx.ParentTxID != "tx-existing-head" {
		t.Errorf("ParentTxID = %q, want tx-existing-head", tx.ParentTxID)
	}
	if tx.OntologyAPIName != "test" {
		t.Errorf("OntologyAPIName = %q, want test", tx.OntologyAPIName)
	}
	if tx.UserID != "alice" {
		t.Errorf("UserID = %q, want alice", tx.UserID)
	}
	if store.recordCalls != 1 {
		t.Errorf("RecordDatasetTransaction calls = %d, want 1", store.recordCalls)
	}
}

// TestCreateTransaction_GenesisHasEmptyParent verifies the first
// checkpoint on an empty chain has no parent.
func TestCreateTransaction_GenesisHasEmptyParent(t *testing.T) {
	store := newFakeDatasetTxWriter()
	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/test/transactions", nil)
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rr.Code, rr.Body.String())
	}
	var tx oms.DatasetTransaction
	if err := json.Unmarshal(rr.Body.Bytes(), &tx); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tx.ParentTxID != "" {
		t.Errorf("ParentTxID = %q, want empty", tx.ParentTxID)
	}
}

// TestCreateTransaction_DegradedReturns400 verifies an unwired store
// surfaces DatasetRollbackUnavailable rather than a chi 500.
func TestCreateTransaction_DegradedReturns400(t *testing.T) {
	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/test/transactions", nil)
	newDatasetRollbackRouter(repo, nil, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "DatasetRollbackUnavailable" {
		t.Errorf("errorName = %q, want DatasetRollbackUnavailable", apiErr.ErrorName)
	}
}

// TestCreateTransaction_DatasetNotFound 404 envelope.
func TestCreateTransaction_DatasetNotFound(t *testing.T) {
	store := newFakeDatasetTxWriter()
	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/missing/transactions", nil)
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
	}
}

// TestRollback_MissingTargetParam returns 400 MissingRollbackTarget when
// `?to=` is omitted.
func TestRollback_MissingTargetParam(t *testing.T) {
	store := newFakeDatasetTxWriter()
	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/test/rollback", nil)
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "MissingRollbackTarget" {
		t.Errorf("errorName = %q, want MissingRollbackTarget", apiErr.ErrorName)
	}
}

// TestRollback_TargetNotFound returns 404 RollbackTargetNotFound.
func TestRollback_TargetNotFound(t *testing.T) {
	store := newFakeDatasetTxWriter()
	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/test/rollback?to=tx-missing", nil)
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
	}
}

// TestRollback_TargetWrongOntology surfaces 400 with the mismatch params.
func TestRollback_TargetWrongOntology(t *testing.T) {
	store := newFakeDatasetTxWriter()
	store.seed(&oms.DatasetTransaction{
		TxID:            "tx-other",
		OntologyAPIName: "other",
		CommittedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"test":  {RID: "ri.ontology.main.ontology.test", APIName: "test"},
		"other": {RID: "ri.ontology.main.ontology.other", APIName: "other"},
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/test/rollback?to=tx-other", nil)
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "RollbackTargetWrongOntology" {
		t.Errorf("errorName = %q, want RollbackTargetWrongOntology", apiErr.ErrorName)
	}
	if apiErr.Parameters["targetOntology"] != "other" {
		t.Errorf("parameters.targetOntology = %q, want other", apiErr.Parameters["targetOntology"])
	}
}

// TestRollback_InvalidTargetPrefix rejects targets that don't start with `tx-`.
func TestRollback_InvalidTargetPrefix(t *testing.T) {
	store := newFakeDatasetTxWriter()
	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"test": {RID: "ri.ontology.main.ontology.test", APIName: "test"},
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/test/rollback?to=garbage", nil)
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	var apiErr struct {
		ErrorName string `json:"errorName"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &apiErr)
	if apiErr.ErrorName != "InvalidRollbackTarget" {
		t.Errorf("errorName = %q, want InvalidRollbackTarget", apiErr.ErrorName)
	}
}

// TestRollback_FullReplay_RestoresAndDeletes covers the core acceptance
// criterion: t1 write → t2 modify+create → rollback to t1. The replay
// must restore objects that existed at t1 (re-index prior state) and
// delete objects created after t1 (no snapshot row → live index purge).
// Bookkeeping: rolled-back tx ids are returned and a fresh chain head is
// recorded with parent_tx_id pointing at t1.
func TestRollback_FullReplay_RestoresAndDeletes(t *testing.T) {
	store := newFakeDatasetTxWriter()

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	store.seed(&oms.DatasetTransaction{TxID: "tx-1", OntologyAPIName: "shop", CommittedAt: t1, EditsCount: 1})
	store.seed(&oms.DatasetTransaction{TxID: "tx-2", ParentTxID: "tx-1", OntologyAPIName: "shop", CommittedAt: t2, EditsCount: 2})
	store.seed(&oms.DatasetTransaction{TxID: "tx-3", ParentTxID: "tx-2", OntologyAPIName: "shop", CommittedAt: t3, EditsCount: 1})

	repo := &rollbackOntologyResolver{
		byOntologyInput: map[string]*oms.Ontology{
			"shop": {RID: "ri.ontology.main.ontology.shop", APIName: "shop"},
		},
		byObjectTypeRID: map[string]*oms.ObjectType{
			"ri.objecttype.main.objecttype.product": {RID: "ri.objecttype.main.objecttype.product", APIName: "Product"},
		},
	}

	// Affected keys: pk=A existed before t1 + was modified after t1; pk=B
	// was newly created after t1 (no snapshot row at t1).
	affected := &fakeAffectedStore{
		byOntologyAfter: map[string][]oms.AffectedKey{
			"ri.ontology.main.ontology.shop": {
				{ObjectTypeRID: "ri.objecttype.main.objecttype.product", PrimaryKey: "A"},
				{ObjectTypeRID: "ri.objecttype.main.objecttype.product", PrimaryKey: "B"},
			},
		},
	}

	// Snapshot at t1: pk=A had {price:100}; pk=B did NOT exist yet.
	history := &fakeHistorySnapshot{
		byObjectType: map[string][]oms.LatestObjectState{
			"ri.objecttype.main.objecttype.product": {
				{PrimaryKey: "A", NewState: json.RawMessage(`{"price":100}`)},
			},
		},
	}

	idx := newRecordingIndex()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/shop/rollback?to=tx-1", nil)
	newDatasetRollbackRouter(repo, store, affected, history, idx).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp rollbackResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body = %s", err, rr.Body.String())
	}

	if resp.RestoredObjects != 1 {
		t.Errorf("RestoredObjects = %d, want 1", resp.RestoredObjects)
	}
	if resp.DeletedObjects != 1 {
		t.Errorf("DeletedObjects = %d, want 1", resp.DeletedObjects)
	}

	// Two newer txs should be marked rolled back: tx-2 + tx-3.
	if len(resp.RolledBackTxIDs) != 2 {
		t.Errorf("RolledBackTxIDs = %v, want 2 entries", resp.RolledBackTxIDs)
	}
	wantMarked := map[string]bool{"tx-2": true, "tx-3": true}
	for _, id := range resp.RolledBackTxIDs {
		if !wantMarked[id] {
			t.Errorf("unexpected rolled-back tx %q", id)
		}
	}
	if len(store.markCalls) != 2 {
		t.Errorf("MarkRolledBack calls = %d, want 2", len(store.markCalls))
	}
	if store.markCalls["tx-2"].rolledBackToTxID != "tx-1" {
		t.Errorf("tx-2 RolledBackToTxID = %q, want tx-1", store.markCalls["tx-2"].rolledBackToTxID)
	}

	// New bookkeeping head: parent should be tx-1, RolledBackToTxID self-points to target.
	if resp.NewTransaction == nil {
		t.Fatalf("NewTransaction is nil, want bookkeeping row")
	}
	if resp.NewTransaction.ParentTxID != "tx-1" {
		t.Errorf("NewTransaction.ParentTxID = %q, want tx-1", resp.NewTransaction.ParentTxID)
	}
	if resp.NewTransaction.RolledBackToTxID != "tx-1" {
		t.Errorf("NewTransaction.RolledBackToTxID = %q, want tx-1", resp.NewTransaction.RolledBackToTxID)
	}

	// Index assertions: pk=A must be re-indexed to {price:100}; pk=B deleted.
	scoped := "shop__Product"
	if doc, ok := idx.indexed[scoped]["A"]; !ok {
		t.Errorf("pk=A was not re-indexed under scoped key %q", scoped)
	} else if v, _ := doc["price"].(float64); v != 100 {
		t.Errorf("pk=A re-indexed price = %v, want 100", doc["price"])
	}
	if !idx.deleted[scoped]["B"] {
		t.Errorf("pk=B was not deleted from live index")
	}

	// Target tx echoed in response for client convenience.
	if resp.TargetTx == nil || resp.TargetTx.TxID != "tx-1" {
		t.Errorf("TargetTx echo missing or wrong: %+v", resp.TargetTx)
	}
}

// TestRollback_DegradedNoReplayInfra still flips the audit overlay and
// records the bookkeeping row even when affected/history/index are nil.
// AC: a metadata-only rollback is the canonical degraded behaviour — the
// chain stays coherent so a future ?asOf=tx- read past the rollback
// resolves.
func TestRollback_DegradedNoReplayInfra(t *testing.T) {
	store := newFakeDatasetTxWriter()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	store.seed(&oms.DatasetTransaction{TxID: "tx-1", OntologyAPIName: "shop", CommittedAt: t1})
	store.seed(&oms.DatasetTransaction{TxID: "tx-2", ParentTxID: "tx-1", OntologyAPIName: "shop", CommittedAt: t2})

	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"shop": {RID: "ri.ontology.main.ontology.shop", APIName: "shop"},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/shop/rollback?to=tx-1", nil)
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp rollbackResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.RestoredObjects != 0 || resp.DeletedObjects != 0 {
		t.Errorf("expected metadata-only rollback to leave object counts at 0; got restored=%d deleted=%d", resp.RestoredObjects, resp.DeletedObjects)
	}
	if len(resp.RolledBackTxIDs) != 1 || resp.RolledBackTxIDs[0] != "tx-2" {
		t.Errorf("RolledBackTxIDs = %v, want [tx-2]", resp.RolledBackTxIDs)
	}
	if resp.NewTransaction == nil || resp.NewTransaction.ParentTxID != "tx-1" {
		t.Errorf("bookkeeping row not stamped at chain head; got %+v", resp.NewTransaction)
	}
}

// TestRollback_NoNewerTxs_NoOp covers the boundary case: the target IS the
// current head. The handler short-circuits with no marks, no replay, but
// still stamps a fresh bookkeeping row so audit explicitly records the
// "rollback to head" event.
func TestRollback_NoNewerTxs_NoOp(t *testing.T) {
	store := newFakeDatasetTxWriter()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.seed(&oms.DatasetTransaction{TxID: "tx-1", OntologyAPIName: "shop", CommittedAt: t1})

	repo := &rollbackOntologyResolver{byOntologyInput: map[string]*oms.Ontology{
		"shop": {RID: "ri.ontology.main.ontology.shop", APIName: "shop"},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v2/datasets/shop/rollback?to=tx-1", nil)
	newDatasetRollbackRouter(repo, store, nil, nil, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp rollbackResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.RolledBackTxIDs) != 0 {
		t.Errorf("RolledBackTxIDs = %v, want empty", resp.RolledBackTxIDs)
	}
	if resp.NewTransaction == nil {
		t.Errorf("expected bookkeeping row even on no-op rollback")
	}
}

// TestNewCheckpointTxID_HasCorrectShape unit-tests the id minting helper —
// must start with `tx-` and be sufficiently unique.
func TestNewCheckpointTxID_HasCorrectShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newCheckpointTxID()
		if !strings.HasPrefix(id, oms.DatasetTransactionIDPrefix) {
			t.Fatalf("id = %q does not start with %q", id, oms.DatasetTransactionIDPrefix)
		}
		if seen[id] {
			t.Fatalf("collision on id %q in 100 mints", id)
		}
		seen[id] = true
	}
}
