package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// datasetTransactionLister is the narrow read surface the
// /datasets/{rid}/history handler and the OSS asOf=tx- resolver depend on.
// Mirrors oms.DatasetTransactionStore minus the recorder method so a
// future degraded-mode router can plug a fake without dragging in the
// funnel consumer's hook.
type datasetTransactionLister interface {
	GetDatasetTransaction(ctx context.Context, txID string) (*oms.DatasetTransaction, error)
	LatestForOntology(ctx context.Context, ontologyAPIName string) (*oms.DatasetTransaction, error)
	ListByOntology(ctx context.Context, ontologyAPIName string, limit int) ([]oms.DatasetTransaction, error)
}

// datasetOntologyResolver is the OMS-side lookup the handler uses to
// translate the URL {rid} into the OntologyAPIName the dataset_transactions
// rows carry. Accepts either an api name or a RID via the existing
// GetOntology semantics.
type datasetOntologyResolver interface {
	GetOntology(ctx context.Context, ridOrApiName string) (*oms.Ontology, error)
}

// datasetTransactionResolverAdapter satisfies objectset.TransactionResolver
// by forwarding tx_id lookups to the dataset_transactions table. Returns
// objectset.ErrTransactionNotFound when the row is absent so the OSS
// handler can map the case to a clean TransactionNotFound 400 envelope.
type datasetTransactionResolverAdapter struct {
	store datasetTransactionLister
}

func newDatasetTransactionResolverAdapter(store datasetTransactionLister) *datasetTransactionResolverAdapter {
	return &datasetTransactionResolverAdapter{store: store}
}

func (a *datasetTransactionResolverAdapter) ResolveTransaction(ctx context.Context, txID string) (time.Time, error) {
	if a == nil || a.store == nil {
		return time.Time{}, errors.New("dataset transaction resolver not configured")
	}
	tx, err := a.store.GetDatasetTransaction(ctx, txID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return time.Time{}, objectset.ErrTransactionNotFound
		}
		return time.Time{}, err
	}
	return tx.CommittedAt, nil
}

// datasetHistoryHandler serves GET /api/v2/datasets/{rid}/history. {rid}
// is resolved to an OntologyAPIName via GetOntology so the URL accepts
// either the ontology RID or its api name — matching the rest of the
// /api/v2/ontologies/{ontologyApiName}/* surface. The response shape is
// {transactions: [...]} where each entry mirrors oms.DatasetTransaction's
// JSON tags; ordering is committed_at-DESC (newest first), the same order
// ListByOntology returns.
type datasetHistoryHandler struct {
	repo  datasetOntologyResolver
	store datasetTransactionLister
}

func newDatasetHistoryHandler(repo datasetOntologyResolver, store datasetTransactionLister) *datasetHistoryHandler {
	return &datasetHistoryHandler{repo: repo, store: store}
}

// historyResponse is the wire shape for GET /datasets/{rid}/history.
type historyResponse struct {
	Transactions []oms.DatasetTransaction `json:"transactions"`
}

// History serves the GET endpoint. Soft 404 when the rid resolves to no
// ontology; clean empty list when the ontology exists but has no
// transactions yet (the chain genesis case).
func (h *datasetHistoryHandler) History(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.repo == nil || h.store == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("DatasetHistoryUnavailable", map[string]string{
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
			apierror.WriteJSON(w, apierror.NewNotFound("DatasetNotFound", map[string]string{
				"rid": rid,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("DatasetHistoryFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}
	if ont == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("DatasetNotFound", map[string]string{"rid": rid}))
		return
	}

	// Cap the chain at 1000 rows for response-size sanity. Single-machine
	// deployments rarely accumulate more in practice; if pagination becomes
	// necessary we'll add a ?pageToken= parameter rather than uncapping.
	const historyCap = 1000
	rows, err := h.store.ListByOntology(r.Context(), ont.APIName, historyCap)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("DatasetHistoryFailed", map[string]string{
			"rid":   rid,
			"error": err.Error(),
		}))
		return
	}
	if rows == nil {
		rows = []oms.DatasetTransaction{}
	}
	httputil.WriteJSON(w, http.StatusOK, &historyResponse{Transactions: rows})
}
