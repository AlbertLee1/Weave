package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/audit"
)

// APIKeyCreateRequest is the JSON body of POST /api/admin/api-keys.
type APIKeyCreateRequest struct {
	Name      string   `json:"name"`
	ExpiresAt string   `json:"expiresAt,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
}

// APIKeyCreateResponse is returned from POST /api/admin/api-keys. RawKey is
// included EXACTLY once at creation time and can never be retrieved again.
type APIKeyCreateResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	RawKey    string     `json:"rawKey"`
	Prefix    string     `json:"prefix"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// APIKeySummary is the redacted shape returned from GET /api/admin/api-keys.
// Critically, it has NO RawKey field; the response is therefore a list of
// these and the rawKey value is NEVER serialized again after creation.
type APIKeySummary struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// APIKeyListResponse is the JSON shape of GET /api/admin/api-keys.
type APIKeyListResponse struct {
	Keys []APIKeySummary `json:"keys"`
}

// APIKeyHandler implements the admin REST endpoints for managing API keys.
type APIKeyHandler struct {
	repo       APIKeyRepository
	auditStore audit.Store
}

// NewAPIKeyHandler constructs an admin handler around an APIKeyRepository.
// auditStore may be nil to disable audit logging.
func NewAPIKeyHandler(repo APIKeyRepository, auditStore audit.Store) *APIKeyHandler {
	return &APIKeyHandler{repo: repo, auditStore: auditStore}
}

// Create handles POST /api/admin/api-keys. It mints a fresh key, persists
// only the SHA-256 hash + prefix, and returns the raw key in the response
// EXACTLY once. The owning user is taken from the request context (the same
// user the auth middleware populated).
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}

	var req APIKeyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAPIKeyRequest", map[string]string{
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

	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidExpiresAt", map[string]string{
				"reason": "expiresAt must be RFC3339",
			}))
			return
		}
		expiresAt = &t
	}

	raw, prefix, err := GenerateAPIKey()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("APIKeyGenerationFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	scopes := req.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	rec := &APIKeyRecord{
		KeyHash:   HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    u.ID,
		Name:      req.Name,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	}
	if err := h.repo.Create(r.Context(), rec); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("APIKeyCreateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "api_key_create",
			ResourceType: "APIKey",
			ResourceRID:  rec.ID,
		})
	}

	resp := APIKeyCreateResponse{
		ID:        rec.ID,
		Name:      rec.Name,
		RawKey:    raw,
		Prefix:    rec.KeyPrefix,
		Scopes:    scopes,
		CreatedAt: rec.CreatedAt,
		ExpiresAt: rec.ExpiresAt,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// List handles GET /api/admin/api-keys. It returns the calling user's active
// API keys with the raw key field intentionally omitted.
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}

	rows, err := h.repo.ListByUser(r.Context(), u.ID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("APIKeyListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	out := APIKeyListResponse{Keys: make([]APIKeySummary, 0, len(rows))}
	for _, row := range rows {
		scopes := row.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		out.Keys = append(out.Keys, APIKeySummary{
			ID:         row.ID,
			Name:       row.Name,
			Prefix:     row.KeyPrefix,
			Scopes:     scopes,
			CreatedAt:  row.CreatedAt,
			ExpiresAt:  row.ExpiresAt,
			LastUsedAt: row.LastUsedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// Delete handles DELETE /api/admin/api-keys/{id}. The path parameter is
// extracted via chi.URLParam.
func (h *APIKeyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.DeleteFor(w, r, id)
}

// DeleteFor implements the soft-revoke logic for a known id. It is split out
// from Delete so unit tests can call the handler without going through chi.
// Returns 204 on success, 401 on missing user, 403 if the caller is not the
// owner, 404 if the key doesn't exist.
func (h *APIKeyHandler) DeleteFor(w http.ResponseWriter, r *http.Request, id string) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}
	if id == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingAPIKeyID", map[string]string{
			"reason": "id path parameter is required",
		}))
		return
	}

	row, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("APIKeyNotFound", map[string]string{
				"id": id,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("APIKeyLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if row.UserID != u.ID {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("APIKeyNotOwned", map[string]string{
			"reason": "callers can only revoke their own api keys",
		}))
		return
	}

	if err := h.repo.Revoke(r.Context(), id); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("APIKeyRevokeFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "api_key_revoke",
			ResourceType: "APIKey",
			ResourceRID:  id,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
