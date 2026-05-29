package developer

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
)

// CreateApplicationRequest is the JSON body of POST /api/v2/developer/applications.
type CreateApplicationRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	RedirectURIs []string `json:"redirectUris,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// CreateApplicationResponse is returned from Create. ClientSecret is the
// only time the plaintext secret is ever visible to the caller; subsequent
// reads return ApplicationSummary which omits the field entirely.
type CreateApplicationResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	ClientID     string    `json:"clientId"`
	ClientSecret string    `json:"clientSecret"`
	RedirectURIs []string  `json:"redirectUris"`
	Scopes       []string  `json:"scopes"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ApplicationSummary is the redacted shape returned from List and Get: the
// plaintext client_secret is intentionally absent and the hash is never
// serialised either.
type ApplicationSummary struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	ClientID     string    `json:"clientId"`
	RedirectURIs []string  `json:"redirectUris"`
	Scopes       []string  `json:"scopes"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ListApplicationsResponse wraps the redacted list in a single
// `applications` envelope to leave room for future pagination metadata.
type ListApplicationsResponse struct {
	Applications []ApplicationSummary `json:"applications"`
}

// ApplicationHandler implements the /api/v2/developer/applications REST
// surface. The actor is taken from the request context (populated upstream
// by the auth middleware), so the handler itself does not depend on any
// particular role gating — the router enforces that.
type ApplicationHandler struct {
	repo ApplicationRepository
}

// NewApplicationHandler constructs a handler around an ApplicationRepository.
func NewApplicationHandler(repo ApplicationRepository) *ApplicationHandler {
	return &ApplicationHandler{repo: repo}
}

// Create handles POST /api/v2/developer/applications. It mints a fresh
// client_id / client_secret, persists only the hash, and returns the secret
// in the response EXACTLY once.
func (h *ApplicationHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}

	var req CreateApplicationRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidApplicationRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingName", map[string]string{
			"reason": "name is required",
		}))
		return
	}

	clientID, err := GenerateClientID()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ClientIDGenerationFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	clientSecret, err := GenerateClientSecret()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ClientSecretGenerationFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	redirects := req.RedirectURIs
	if redirects == nil {
		redirects = []string{}
	}
	scopes := req.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	app := &Application{
		Name:             req.Name,
		Description:      req.Description,
		ClientID:         clientID,
		ClientSecretHash: HashClientSecret(clientSecret),
		RedirectURIs:     redirects,
		Scopes:           scopes,
		CreatedBy:        u.ID,
	}
	if err := h.repo.Create(r.Context(), app); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ApplicationCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	resp := CreateApplicationResponse{
		ID:           app.ID,
		Name:         app.Name,
		Description:  app.Description,
		ClientID:     app.ClientID,
		ClientSecret: clientSecret,
		RedirectURIs: app.RedirectURIs,
		Scopes:       app.Scopes,
		CreatedBy:    app.CreatedBy,
		CreatedAt:    app.CreatedAt,
		UpdatedAt:    app.UpdatedAt,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// List handles GET /api/v2/developer/applications. Returns only the
// caller's applications in redacted form.
func (h *ApplicationHandler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", nil))
		return
	}
	rows, err := h.repo.ListByUser(r.Context(), u.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ApplicationListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	out := ListApplicationsResponse{Applications: make([]ApplicationSummary, 0, len(rows))}
	for _, a := range rows {
		out.Applications = append(out.Applications, redact(a))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// Get handles GET /api/v2/developer/applications/{id}. Returns the redacted
// summary (never the client_secret).
func (h *ApplicationHandler) Get(w http.ResponseWriter, r *http.Request) {
	h.GetFor(w, r, chi.URLParam(r, "id"))
}

// GetFor is the chi-independent variant so unit tests can call the handler
// without going through the router.
func (h *ApplicationHandler) GetFor(w http.ResponseWriter, r *http.Request, id string) {
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
			"reason": "callers can only view their own applications",
		}))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(redact(app))
}

// Delete handles DELETE /api/v2/developer/applications/{id}. Hard delete:
// the client_id and client_secret become immediately unusable against the
// future OAuth token endpoint.
func (h *ApplicationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	h.DeleteFor(w, r, chi.URLParam(r, "id"))
}

// DeleteFor is the chi-independent variant — same role as GetFor.
func (h *ApplicationHandler) DeleteFor(w http.ResponseWriter, r *http.Request, id string) {
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
			"reason": "callers can only delete their own applications",
		}))
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, ErrApplicationNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ApplicationNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ApplicationDeleteFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RegisterRoutes wires the developer-console endpoints onto a chi router.
// Split out so wire-up tests can assemble the handler+router without
// depending on the full cmd/server bootstrap.
func (h *ApplicationHandler) RegisterRoutes(r chi.Router) {
	r.Post("/api/v2/developer/applications", h.Create)
	r.Get("/api/v2/developer/applications", h.List)
	r.Get("/api/v2/developer/applications/{id}", h.Get)
	r.Delete("/api/v2/developer/applications/{id}", h.Delete)
}

func redact(a *Application) ApplicationSummary {
	redirects := a.RedirectURIs
	if redirects == nil {
		redirects = []string{}
	}
	scopes := a.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return ApplicationSummary{
		ID:           a.ID,
		Name:         a.Name,
		Description:  a.Description,
		ClientID:     a.ClientID,
		RedirectURIs: redirects,
		Scopes:       scopes,
		CreatedBy:    a.CreatedBy,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
}
