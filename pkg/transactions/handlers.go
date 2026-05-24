package transactions

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/httputil"
)

// Handler serves the Foundry OSv2 OntologyTransaction experimental
// endpoints. Only "append edits" is exposed today; see package doc.
type Handler struct {
	store Store
}

// NewHandler constructs a Handler backed by the given Store.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes mounts the OntologyTransaction routes on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v2/ontologies/{ontologyApiName}/transactions/{transactionId}/edits", h.ApplyEdits)
	r.Get("/api/v2/ontologies/{ontologyApiName}/transactions/{transactionId}", h.GetTransaction)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/transactions/{transactionId}", h.AbortTransaction)
}

// applyEditsRequest is the wire body for POST .../transactions/{id}/edits.
type applyEditsRequest struct {
	Edits []funnel.Edit `json:"edits"`
}

// applyEditsResponse is the wire response envelope.
type applyEditsResponse struct {
	TransactionID string `json:"transactionId"`
	AppendedEdits int    `json:"appendedEdits"`
	TotalEdits    int    `json:"totalEdits"`
}

// getTransactionResponse is the wire shape for GET .../transactions/{id}.
// Edits is the full append-ordered list; empty (non-nil) array for an
// unknown transaction — matches the POST endpoint's "auto-created on
// first use" semantic so callers don't have to special-case 404.
type getTransactionResponse struct {
	TransactionID string        `json:"transactionId"`
	TotalEdits    int           `json:"totalEdits"`
	Edits         []funnel.Edit `json:"edits"`
}

// ApplyEdits handles POST /api/v2/ontologies/{o}/transactions/{transactionId}/edits.
// The endpoint is marked experimental and requires ?preview=true.
func (h *Handler) ApplyEdits(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("preview") != "true" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PreviewRequired", map[string]string{
			"reason": "OntologyTransaction edits endpoint is experimental and requires ?preview=true",
		}))
		return
	}

	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("TransactionStoreNotConfigured", nil))
		return
	}

	ontology := chi.URLParam(r, "ontologyApiName")
	txnID := chi.URLParam(r, "transactionId")

	var body applyEditsRequest
	if err := httputil.ReadJSON(r, &body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if len(body.Edits) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("NoEditsProvided", map[string]string{
			"reason": "edits must be a non-empty array",
		}))
		return
	}

	for i, e := range body.Edits {
		switch e.Type {
		case funnel.EditTypeCreate, funnel.EditTypeModify, funnel.EditTypeDelete:
			// ok
		default:
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidEditType", map[string]string{
				"index":  strconv.Itoa(i),
				"type":   string(e.Type),
				"reason": "edit.type must be CREATE, MODIFY, or DELETE",
			}))
			return
		}
		if e.ObjectType == "" || e.PrimaryKey == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidEdit", map[string]string{
				"index":  strconv.Itoa(i),
				"reason": "edit.objectType and edit.primaryKey are required",
			}))
			return
		}
	}

	key := Key{Ontology: ontology, TransactionID: txnID}
	if err := h.store.AppendEdits(r.Context(), key, body.Edits); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("AppendEditsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	all, err := h.store.ListEdits(r.Context(), key)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListEditsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, applyEditsResponse{
		TransactionID: txnID,
		AppendedEdits: len(body.Edits),
		TotalEdits:    len(all),
	})
}

// GetTransaction handles GET /api/v2/ontologies/{o}/transactions/{transactionId}.
// Round 59 added it so SDK clients can read back the edits a previous
// POST appended without keeping a client-side mirror. Same ?preview=true
// gate as ApplyEdits and the same "unknown txn → empty buffer" semantic
// (the store auto-creates a new transaction on first use, so absence is
// just zero appended edits).
func (h *Handler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("preview") != "true" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PreviewRequired", map[string]string{
			"reason": "OntologyTransaction get endpoint is experimental and requires ?preview=true",
		}))
		return
	}
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("TransactionStoreNotConfigured", nil))
		return
	}
	ontology := chi.URLParam(r, "ontologyApiName")
	txnID := chi.URLParam(r, "transactionId")

	edits, err := h.store.ListEdits(r.Context(), Key{Ontology: ontology, TransactionID: txnID})
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListEditsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	// Guarantee non-nil so the JSON serialiser emits `[]` rather than
	// `null` — SDK clients iterate without nil-checks.
	if edits == nil {
		edits = []funnel.Edit{}
	}
	httputil.WriteJSON(w, http.StatusOK, getTransactionResponse{
		TransactionID: txnID,
		TotalEdits:    len(edits),
		Edits:         edits,
	})
}

// AbortTransaction handles DELETE /api/v2/ontologies/{o}/transactions/{transactionId}.
// Round 59 added it so SDK clients can discard an in-progress
// transaction without restarting the server. Idempotent — deleting an
// unknown transaction returns 204 to keep retries safe.
func (h *Handler) AbortTransaction(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("preview") != "true" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PreviewRequired", map[string]string{
			"reason": "OntologyTransaction abort endpoint is experimental and requires ?preview=true",
		}))
		return
	}
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewInternal("TransactionStoreNotConfigured", nil))
		return
	}
	ontology := chi.URLParam(r, "ontologyApiName")
	txnID := chi.URLParam(r, "transactionId")

	if err := h.store.DeleteTransaction(r.Context(), Key{Ontology: ontology, TransactionID: txnID}); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("DeleteTransactionFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

