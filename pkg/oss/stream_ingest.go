package oss

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

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

// IngestRateLimiter gates stream ingest requests per ontology. The Allow
// method returns true when the request may proceed. When false, the
// handler responds with 429 + Retry-After.
type IngestRateLimiter interface {
	Allow(ontology string) bool
}

// IndexReadinessChecker reports whether a Bleve index already exists for
// (ontologyAPIName, objectType). DOG-003: the funnel consumer's
// IndexDocument call silently fails with "index not found" when an ingest
// targets an ObjectType whose index was never bootstrapped — but the
// publisher has already returned 200 by then, so the operator sees an
// editCount success that later disappears. When wired, the handler checks
// readiness before publishing and rejects the batch with 409 IndexNotReady
// so the caller can retry after a rebuild rather than discovering the
// silent drop via a missing list/search row hours later.
type IndexReadinessChecker interface {
	IndexReady(ontologyAPIName, objectType string) bool
}

// PerOntologyRateLimiter maintains a token-bucket rate limiter per ontology.
// Limiters are lazily created on first access and share the same rate/burst.
type PerOntologyRateLimiter struct {
	rate    rate.Limit
	burst   int
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
}

// NewPerOntologyRateLimiter creates a rate limiter that allows ratePerSec
// requests per second per ontology with the given burst capacity.
func NewPerOntologyRateLimiter(ratePerSec float64, burst int) *PerOntologyRateLimiter {
	return &PerOntologyRateLimiter{
		rate:    rate.Limit(ratePerSec),
		burst:   burst,
		buckets: make(map[string]*rate.Limiter),
	}
}

// Allow returns true when the ontology's token bucket has capacity. Non-blocking.
func (l *PerOntologyRateLimiter) Allow(ontology string) bool {
	l.mu.Lock()
	lim, ok := l.buckets[ontology]
	if !ok {
		lim = rate.NewLimiter(l.rate, l.burst)
		l.buckets[ontology] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// StreamIngestHandler handles POST .../streams/{objectType}/ingest requests
// for bulk-importing edits that bypass Action rules.
type StreamIngestHandler struct {
	publisher       IngestPublisher
	policyChecker   IngestPolicyChecker
	rateLimiter     IngestRateLimiter
	indexReadiness  IndexReadinessChecker
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

// SetRateLimiter wires the optional per-ontology rate limiter. When set,
// ServeHTTP checks the limiter before processing. When nil, no rate limiting
// is applied (backwards compatible with pre-US-063 deployments).
func (h *StreamIngestHandler) SetRateLimiter(l IngestRateLimiter) {
	h.rateLimiter = l
}

// SetIndexReadinessChecker wires the DOG-003 fail-fast hook. When set,
// ServeHTTP rejects ingest batches whose ObjectType has no open Bleve
// index with 409 IndexNotReady, preventing the silent
// edits-accepted-but-never-visible failure mode the dogfood report
// captured. A nil checker disables the guard (pre-DOG-003 behaviour).
func (h *StreamIngestHandler) SetIndexReadinessChecker(c IndexReadinessChecker) {
	h.indexReadiness = c
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

	// US-063: per-ontology token-bucket rate limiting. Check before any
	// expensive work (policy evaluation, JSON parsing). When the bucket is
	// exhausted respond with 429 + Retry-After so the client can back off.
	if h.rateLimiter != nil && !h.rateLimiter.Allow(ontology) {
		retryAfter := fmt.Sprintf("%d", int(math.Ceil(1.0)))
		w.Header().Set("Retry-After", retryAfter)
		apierror.WriteJSON(w, apierror.NewTooManyRequests("IngestRateLimitExceeded",
			map[string]string{
				"ontology": ontology,
				"reason":   "ingest rate limit exceeded for this ontology",
			}))
		return
	}

	// DOG-003: fail fast if the target Bleve index does not exist. Without
	// this guard a NATS publish would 200-OK the caller but the consumer's
	// IndexDocument would then drop every edit with "index not found",
	// producing a silent data-loss surface that misled the dogfood operator
	// into thinking ingest had succeeded.
	if h.indexReadiness != nil && !h.indexReadiness.IndexReady(ontology, objectType) {
		apierror.WriteJSON(w, apierror.NewConflict("IndexNotReady",
			map[string]string{
				"ontology":   ontology,
				"objectType": objectType,
				"reason":     "object index has not been bootstrapped; rebuild via /api/admin/indexes/rebuild and retry",
			}))
		return
	}

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
