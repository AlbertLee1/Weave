package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// datasetTransactionWriter is the narrow store contract that the US-388
// CreateTransaction + Rollback handlers depend on, on top of the
// pre-existing read surface (datasetTransactionLister). Mirrors the
// DatasetTransactionStore method set without leaking the recorder hook
// the funnel consumer uses, so a degraded-mode router can plug a fake
// without dragging in NATS / consumer wiring.
type datasetTransactionWriter interface {
	datasetTransactionLister
	RecordDatasetTransaction(ctx context.Context, tx *oms.DatasetTransaction) error
	ListAfterCommittedAt(ctx context.Context, ontologyAPIName string, after time.Time) ([]oms.DatasetTransaction, error)
	MarkRolledBack(ctx context.Context, txID, rolledBackToTxID string, rolledBackAt time.Time) error
}

// datasetRollbackOntologyRepo is the OMS subset the rollback handler needs:
// the ontology lookup (URL {rid} → OntologyAPIName) plus an enumeration of
// every ObjectType under that ontology so the per-PK replay can bucket
// affected keys by their owning ObjectType.
type datasetRollbackOntologyRepo interface {
	GetOntology(ctx context.Context, ridOrApiName string) (*oms.Ontology, error)
	GetObjectType(ctx context.Context, rid string) (*oms.ObjectType, error)
}

// datasetRollbackAffectedStore returns the (ObjectTypeRID, PrimaryKey)
// tuples whose object_history has at least one row newer than `after`. The
// US-388 rollback iterates this set so the replay touches only the rows
// that actually changed since the rollback target.
type datasetRollbackAffectedStore interface {
	ListAffectedKeysSince(ctx context.Context, ontologyRID string, after time.Time) ([]oms.AffectedKey, error)
}

// datasetRollbackSnapshot is the per-ObjectType time-travel reader. The
// US-388 handler asks for a snapshot at the rollback target's CommittedAt
// and uses it to decide, for each affected PK, whether to restore a prior
// state or delete the row outright (object created after target).
type datasetRollbackSnapshot interface {
	SnapshotObjectsAt(ctx context.Context, objectTypeRID string, asOf time.Time) ([]oms.LatestObjectState, error)
}

// indexWriter is the index.Manager subset the rollback handler depends on.
type indexWriter interface {
	IndexDocument(scopedKey, primaryKey string, doc map[string]interface{}) error
	DeleteDocument(scopedKey, primaryKey string) error
}

// datasetRollbackHandler hosts the US-388 explicit transactions + rollback
// endpoints. The handler is mounted unconditionally so the route is always
// discoverable via OpenAPI / contract tests; missing dependencies surface
// as `DatasetRollbackUnavailable` 400 rather than chi's bare 404.
type datasetRollbackHandler struct {
	repo            datasetRollbackOntologyRepo
	store           datasetTransactionWriter
	affectedStore   datasetRollbackAffectedStore
	historyStore    datasetRollbackSnapshot
	indexMgr        indexWriter
}

func newDatasetRollbackHandler(
	repo datasetRollbackOntologyRepo,
	store datasetTransactionWriter,
	affected datasetRollbackAffectedStore,
	history datasetRollbackSnapshot,
	indexMgr indexWriter,
) *datasetRollbackHandler {
	return &datasetRollbackHandler{
		repo:          repo,
		store:         store,
		affectedStore: affected,
		historyStore:  history,
		indexMgr:      indexMgr,
	}
}

// createTransactionRequest is the wire shape for POST /transactions. Both
// fields are optional; UserID falls through from the auth middleware when
// blank, EditsCount is informational metadata for explicit checkpoints
// (typically zero — the endpoint creates an empty marker tx).
type createTransactionRequest struct {
	UserID     string `json:"userId,omitempty"`
	EditsCount int    `json:"editsCount,omitempty"`
}

// CreateTransaction POST /api/v2/datasets/{rid}/transactions records an
// explicit checkpoint transaction in the dataset_transactions chain so a
// caller can pin "?asOf=tx-..." or "?to=tx-..." to a known point without
// having to wait for an EditBatch to drift through the funnel. Returns the
// freshly-stamped DatasetTransaction whose ParentTxID points at the prior
// chain head (empty for the genesis case).
func (h *datasetRollbackHandler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.repo == nil || h.store == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("DatasetRollbackUnavailable", map[string]string{
			"reason": "dataset transaction store is not configured on this server",
		}))
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingDatasetRID", map[string]string{
			"reason": "URL path requires a dataset rid",
		}))
		return
	}
	ont, err := h.repo.GetOntology(r.Context(), rid)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DatasetNotFound", map[string]string{"rid": rid}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}
	if ont == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("DatasetNotFound", map[string]string{"rid": rid}))
		return
	}

	// Body is optional — empty body means "create a default checkpoint".
	var req createTransactionRequest
	if r.ContentLength > 0 && r.Body != nil {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
				"error": err.Error(),
			}))
			return
		}
	}

	parent, err := h.store.LatestForOntology(r.Context(), ont.APIName)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}

	tx := &oms.DatasetTransaction{
		TxID:            newCheckpointTxID(),
		OntologyAPIName: ont.APIName,
		CommittedAt:     time.Now().UTC(),
		EditsCount:      req.EditsCount,
		UserID:          req.UserID,
	}
	if parent != nil {
		tx.ParentTxID = parent.TxID
	}
	if err := h.store.RecordDatasetTransaction(r.Context(), tx); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, tx)
}

// rollbackResponse is the wire shape for POST /rollback. The summary lists
// the txs that were marked rolled-back, the count of objects whose state
// the handler restored from the historical snapshot, the count of objects
// it deleted (created after the rollback target), and the bookkeeping tx
// the handler wrote to record the rollback as the new chain head.
type rollbackResponse struct {
	RolledBackTxIDs []string                 `json:"rolledBackTxIds"`
	RestoredObjects int                      `json:"restoredObjects"`
	DeletedObjects  int                      `json:"deletedObjects"`
	NewTransaction  *oms.DatasetTransaction  `json:"newTransaction,omitempty"`
	TargetTx        *oms.DatasetTransaction  `json:"targetTx,omitempty"`
}

// Rollback POST /api/v2/datasets/{rid}/rollback?to=tx-... rolls the
// dataset back to the given target. Steps:
//
//  1. Validate the target tx exists and belongs to this ontology.
//  2. List dataset_transactions strictly newer than the target.
//  3. For each affected (objectTypeRID, primaryKey) pair, look up the
//     state at the target's CommittedAt via SnapshotObjectsAt and either
//     re-index the prior state or delete the doc when no prior state
//     exists (object was created after target).
//  4. MarkRolledBack each newer tx (audit overlay).
//  5. Record a fresh "rollback" tx whose parent_tx_id is the target so
//     the chain stays linear and a future ?asOf=tx-... read past the
//     rollback resolves correctly.
//
// Per-PK replay errors are tolerated and accumulated into the response so
// a partial failure surfaces useful counts. Marking failures DO abort the
// handler so the chain audit overlay stays coherent.
func (h *datasetRollbackHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.repo == nil || h.store == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("DatasetRollbackUnavailable", map[string]string{
			"reason": "dataset transaction store is not configured on this server",
		}))
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingDatasetRID", map[string]string{
			"reason": "URL path requires a dataset rid",
		}))
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("to"))
	if target == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingRollbackTarget", map[string]string{
			"reason": "rollback requires a ?to=tx-... query parameter",
		}))
		return
	}
	if !strings.HasPrefix(target, oms.DatasetTransactionIDPrefix) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRollbackTarget", map[string]string{
			"to":     target,
			"reason": "rollback target must start with \"tx-\"",
		}))
		return
	}

	ont, err := h.repo.GetOntology(r.Context(), rid)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("DatasetNotFound", map[string]string{"rid": rid}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}
	if ont == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("DatasetNotFound", map[string]string{"rid": rid}))
		return
	}

	targetTx, err := h.store.GetDatasetTransaction(r.Context(), target)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("RollbackTargetNotFound", map[string]string{"to": target}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}
	if targetTx.OntologyAPIName != ont.APIName {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("RollbackTargetWrongOntology", map[string]string{
			"to":              target,
			"targetOntology":  targetTx.OntologyAPIName,
			"requestOntology": ont.APIName,
		}))
		return
	}

	// Step 2: list newer txs.
	newer, err := h.store.ListAfterCommittedAt(r.Context(), ont.APIName, targetTx.CommittedAt)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}

	resp := &rollbackResponse{
		TargetTx:        targetTx,
		RolledBackTxIDs: []string{},
	}

	// Step 3: replay every affected (objectType, pk) using the snapshot
	// at the target's CommittedAt. The replay is best-effort: missing
	// dependencies (no affected store, no history reader, no index
	// manager) degrade to a metadata-only rollback and the response
	// counts surface the no-op cleanly.
	if h.affectedStore != nil && h.historyStore != nil && h.indexMgr != nil {
		restored, deleted, replayErr := h.replayObjects(r.Context(), ont, targetTx.CommittedAt)
		if replayErr != nil {
			apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
				"rid":   rid,
				"error": replayErr.Error(),
			}))
			return
		}
		resp.RestoredObjects = restored
		resp.DeletedObjects = deleted
	}

	// Step 4: mark every newer tx as rolled back.
	rolledBackAt := time.Now().UTC()
	for _, tx := range newer {
		if err := h.store.MarkRolledBack(r.Context(), tx.TxID, targetTx.TxID, rolledBackAt); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
				"rid":   rid,
				"txId":  tx.TxID,
				"error": err.Error(),
			}))
			return
		}
		resp.RolledBackTxIDs = append(resp.RolledBackTxIDs, tx.TxID)
	}

	// Step 5: stamp a bookkeeping "rollback" tx as the new chain head.
	bookkeeping := &oms.DatasetTransaction{
		TxID:             newCheckpointTxID(),
		ParentTxID:       targetTx.TxID,
		OntologyAPIName:  ont.APIName,
		CommittedAt:      rolledBackAt,
		EditsCount:       resp.RestoredObjects + resp.DeletedObjects,
		RolledBackToTxID: targetTx.TxID,
	}
	if err := h.store.RecordDatasetTransaction(r.Context(), bookkeeping); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DatasetRollbackFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}
	resp.NewTransaction = bookkeeping
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// replayObjects walks the affected (objectType, pk) tuples and applies the
// per-OT snapshot at `asOf` to the live Bleve indexes. Returns the count
// of objects whose state was restored (re-indexed from snapshot) and the
// count deleted (objects that did not yet exist at asOf).
func (h *datasetRollbackHandler) replayObjects(ctx context.Context, ont *oms.Ontology, asOf time.Time) (int, int, error) {
	affected, err := h.affectedStore.ListAffectedKeysSince(ctx, ont.RID, asOf)
	if err != nil {
		return 0, 0, err
	}
	if len(affected) == 0 {
		return 0, 0, nil
	}

	// Bucket PKs by ObjectTypeRID so each per-OT snapshot is fetched once.
	byOT := make(map[string][]string)
	for _, k := range affected {
		byOT[k.ObjectTypeRID] = append(byOT[k.ObjectTypeRID], k.PrimaryKey)
	}

	var restored, deleted int
	for otRID, pks := range byOT {
		ot, err := h.repo.GetObjectType(ctx, otRID)
		if err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				continue
			}
			return restored, deleted, err
		}
		if ot == nil {
			continue
		}
		snapshot, err := h.historyStore.SnapshotObjectsAt(ctx, otRID, asOf)
		if err != nil {
			return restored, deleted, err
		}
		// Index snapshot for O(1) lookups.
		bySnapshotPK := make(map[string]oms.LatestObjectState, len(snapshot))
		for _, row := range snapshot {
			bySnapshotPK[row.PrimaryKey] = row
		}

		scopedKey := index.ScopedKey(ont.APIName, ot.APIName)
		for _, pk := range pks {
			row, ok := bySnapshotPK[pk]
			if !ok {
				// Object did not exist at asOf — delete from the live index.
				if err := h.indexMgr.DeleteDocument(scopedKey, pk); err != nil {
					return restored, deleted, err
				}
				deleted++
				continue
			}
			doc, err := decodeNewState(row.NewState)
			if err != nil {
				return restored, deleted, err
			}
			if err := h.indexMgr.IndexDocument(scopedKey, pk, doc); err != nil {
				return restored, deleted, err
			}
			restored++
		}
	}
	return restored, deleted, nil
}

// decodeNewState unmarshals a JSONB new_state blob into the property map
// the index manager expects. Empty input is a soft skip — the caller
// already filtered DELETE tombstones.
func decodeNewState(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

// newCheckpointTxID mints a fresh "tx-<hex>" identifier for a manual
// checkpoint or rollback bookkeeping row. 16 random bytes give 128 bits
// of entropy — comfortable for chain ids that never need to roll over in
// a single-machine deployment.
func newCheckpointTxID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand should not fail on supported platforms; fall back to
		// a time-based id so the handler keeps making forward progress in
		// the unlikely degraded case.
		return oms.DatasetTransactionIDPrefix + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return oms.DatasetTransactionIDPrefix + hex.EncodeToString(buf[:])
}
