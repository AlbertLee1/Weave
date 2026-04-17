package developer

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/metrics"
)

// UsageResponse wraps the per-window summaries returned by the /usage
// endpoint in a single envelope so future metadata (rate limit info,
// cursor, etc.) has a stable place to land.
type UsageResponse struct {
	ApplicationID string                  `json:"applicationId"`
	ClientID      string                  `json:"clientId"`
	Windows       []metrics.UsageSummary  `json:"windows"`
}

// UsageHandler serves GET /api/v2/developer/applications/{id}/usage. It
// reuses the ApplicationRepository for ownership enforcement and reads
// from the metrics.UsageSampleStore that UsageMiddleware writes into.
type UsageHandler struct {
	repo  ApplicationRepository
	store *metrics.UsageSampleStore
	now   func() time.Time
}

// NewUsageHandler constructs a UsageHandler. Both dependencies are
// required; callers guard the wire-up with nil-checks on the server-deps
// struct before registering the route.
func NewUsageHandler(repo ApplicationRepository, store *metrics.UsageSampleStore) *UsageHandler {
	return &UsageHandler{repo: repo, store: store, now: time.Now}
}

// Get handles GET /api/v2/developer/applications/{id}/usage.
func (h *UsageHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.GetFor(w, r, chi.URLParam(r, "id"))
}

// GetFor is the chi-independent variant used by tests to drive the handler
// without going through a router.
func (h *UsageHandler) GetFor(w http.ResponseWriter, r *http.Request, id string) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", nil))
		return
	}
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingApplicationID", map[string]string{
			"reason": "id path parameter is required",
		}))
		return
	}
	app, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrApplicationNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ApplicationNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ApplicationLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if app.CreatedBy != u.ID {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("ApplicationNotOwned", map[string]string{
			"reason": "callers can only view usage for their own applications",
		}))
		return
	}

	now := h.now()
	// Pull the widest window once and reuse the slice across windows: this
	// keeps the sample store's read lock held for a single scan regardless
	// of how many windows we return.
	widest := time.Duration(0)
	for _, w := range metrics.UsageWindows {
		if w.Duration > widest {
			widest = w.Duration
		}
	}
	var samples []metrics.UsageSample
	if h.store != nil {
		samples = h.store.Snapshot(app.ClientID, widest)
	}
	windows := metrics.SummarizeAll(samples, now)

	resp := UsageResponse{
		ApplicationID: app.ID,
		ClientID:      app.ClientID,
		Windows:       windows,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// RegisterRoutes wires the /usage endpoint onto a chi router. Mirrors the
// pattern in application_handlers.go so cmd/server/main.go can register
// the endpoint inside the auth group.
func (h *UsageHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/developer/applications/{id}/usage", h.Get)
}
