package oss

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/httputil"
)

// MaxIngestEdits is the maximum number of edits allowed per ingest request.
const MaxIngestEdits = 1000

// IngestPublisher is the narrow interface the stream ingest handler needs
// to publish edit batches. The concrete funnel.Publisher satisfies it.
type IngestPublisher interface {
	Publish(batch *funnel.EditBatch) (uint64, error)
}

// IngestPolicyChecker decides whether the caller is permitted to ingest data
// into the given objectType under the given ontology. The implementation
// resolves the API names to OMS types and delegates to the security Engine's
// AllowedForIngest method. A nil checker skips the policy check (backwards
// compatible with pre-US-062 deployments where RBAC alone guards the route).
type IngestPolicyChecker interface {
	AllowedForIngest(ctx context.Context, ontologyAPIName, objectType string) (bool, error)
}

// StreamIngestHandler handles POST .../streams/{objectType}/ingest requests
// for bulk-importing edits that bypass Action rules.
type StreamIngestHandler struct {
	publisher     IngestPublisher
	policyChecker IngestPolicyChecker
}

// NewStreamIngestHandler creates a new stream ingest handler.
func NewStreamIngestHandler(pub IngestPublisher) *StreamIngestHandler {
	return &StreamIngestHandler{publisher: pub}
}

// SetPolicyChecker wires the optional policy checker. When set, ServeHTTP
// evaluates the caller's attributes against OBJECT-scoped policies before
// publishing. When nil, the handler relies solely on RBAC middleware.
func (h *StreamIngestHandler) SetPolicyChecker(c IngestPolicyChecker) {
	h.policyChecker = c
}

// streamIngestRequest is the JSON request body for the ingest endpoint.
type streamIngestRequest struct {
	Edits []funnel.Edit `json:"edits"`
}

// StreamIngestResponse is the JSON response body for a successful ingest.
type StreamIngestResponse struct {
	BatchID   string `json:"batchId"`
	EditCount int    `json:"editCount"`
}

// ServeHTTP handles a stream ingest POST request.
func (h *StreamIngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ontology := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")

	// US-062: policy engine enforcement — when a checker is wired, evaluate
	// whether the caller's user attributes satisfy the OBJECT-scoped policies
	// for this ObjectType. Deny with 403 IngestNotAllowed on failure.
	if h.policyChecker != nil {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			apierror.WriteJSON(w, apierror.NewPermissionDenied("IngestNotAllowed",
				map[string]string{"reason": "no authenticated user"}))
			return
		}
		allowed, err := h.policyChecker.AllowedForIngest(r.Context(), ontology, objectType)
		if err != nil || !allowed {
			apierror.WriteJSON(w, apierror.NewPermissionDenied("IngestNotAllowed",
				map[string]string{"reason": "policy denied"}))
			return
		}
	}

	var req streamIngestRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody",
			map[string]string{"error": err.Error()}))
		return
	}

	if len(req.Edits) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("EmptyEdits",
			map[string]string{"reason": "edits array must not be empty"}))
		return
	}
	if len(req.Edits) > MaxIngestEdits {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TooManyEdits",
			map[string]string{"reason": "edits array must not exceed 1000"}))
		return
	}

	// Tag every edit with source=ingest and enforce the URL objectType.
	for i := range req.Edits {
		req.Edits[i].Source = funnel.EditSourceIngest
		req.Edits[i].ObjectType = objectType
	}

	batch := &funnel.EditBatch{
		ID:              funnel.GenerateBatchID(),
		OntologyAPIName: ontology,
		Edits:           req.Edits,
		Timestamp:       time.Now(),
	}

	if _, err := h.publisher.Publish(batch); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PublishFailed",
			map[string]string{"error": err.Error()}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, StreamIngestResponse{
		BatchID:   batch.ID,
		EditCount: len(batch.Edits),
	})
}
