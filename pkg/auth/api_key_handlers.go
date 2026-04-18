package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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

// APIKeyRotateRequest is the JSON body of POST /api/admin/api-keys/{id}/rotate.
// GraceDays is optional: when omitted or <=0 the handler falls back to
// DefaultAPIKeyRotationGrace (7 days).
type APIKeyRotateRequest struct {
	GraceDays int `json:"graceDays,omitempty"`
}

// APIKeyRotateResponse mirrors APIKeyCreateResponse for the freshly minted
// successor, plus the predecessor's final rotates_at timestamp so the
// caller can surface the dual-key window to the owning service.
type APIKeyRotateResponse struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	RawKey            string     `json:"rawKey"`
	Prefix            string     `json:"prefix"`
	Scopes            []string   `json:"scopes"`
	CreatedAt         time.Time  `json:"createdAt"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
	PredecessorID     string     `json:"predecessorId"`
	PredecessorExpiry time.Time  `json:"predecessorExpiry"`
}

// APIKeyRotationWarning is the row shape returned from
// GET /api/admin/api-keys/rotations. Each entry is a predecessor key whose
// rotates_at falls within the requested look-ahead window.
type APIKeyRotationWarning struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Prefix      string    `json:"prefix"`
	RotatesAt   time.Time `json:"rotatesAt"`
	SuccessorID string    `json:"successorId,omitempty"`
}

// APIKeyRotationsResponse is the JSON shape of GET /api/admin/api-keys/rotations.
type APIKeyRotationsResponse struct {
	Warnings []APIKeyRotationWarning `json:"warnings"`
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

// Rotate handles POST /api/admin/api-keys/{id}/rotate. It mints a fresh key
// inheriting the predecessor's Name + Scopes, stamps the predecessor with
// rotates_at = now + grace, and returns the new raw key EXACTLY once. During
// the grace window both keys authenticate; after rotates_at the predecessor
// is rejected by middleware (IsRotationExpired).
func (h *APIKeyHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.RotateFor(w, r, id)
}

// RotateFor implements the rotation logic for a known predecessor id. It is
// split out from Rotate so unit tests can call the handler without going
// through chi. Returns 201 on success, 401 / 403 / 404 / 409 on the usual
// failures (missing user, non-owner, missing predecessor, already rotated).
func (h *APIKeyHandler) RotateFor(w http.ResponseWriter, r *http.Request, id string) {
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

	var req APIKeyRotateRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAPIKeyRequest", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
	}

	predecessor, err := h.repo.GetByID(r.Context(), id)
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
	if predecessor.UserID != u.ID {
		apierror.WriteJSON(w, apierror.NewPermissionDenied("APIKeyNotOwned", map[string]string{
			"reason": "callers can only rotate their own api keys",
		}))
		return
	}
	if predecessor.IsRevoked() {
		apierror.WriteJSON(w, apierror.NewNotFound("APIKeyNotFound", map[string]string{
			"id": id,
		}))
		return
	}
	if predecessor.SuccessorID != nil {
		apierror.WriteJSON(w, apierror.NewConflict("APIKeyAlreadyRotated", map[string]string{
			"id":          id,
			"successorId": *predecessor.SuccessorID,
		}))
		return
	}

	grace := DefaultAPIKeyRotationGrace
	if req.GraceDays > 0 {
		grace = time.Duration(req.GraceDays) * 24 * time.Hour
	}
	graceUntil := time.Now().Add(grace)

	raw, prefix, err := GenerateAPIKey()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("APIKeyGenerationFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	scopes := predecessor.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	successor := &APIKeyRecord{
		KeyHash:   HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    predecessor.UserID,
		Name:      predecessor.Name,
		Scopes:    scopes,
		ExpiresAt: predecessor.ExpiresAt,
	}

	if err := h.repo.Rotate(r.Context(), predecessor.ID, successor, graceUntil); err != nil {
		if errors.Is(err, ErrAPIKeyAlreadyRotated) {
			apierror.WriteJSON(w, apierror.NewConflict("APIKeyAlreadyRotated", map[string]string{
				"id": id,
			}))
			return
		}
		if errors.Is(err, ErrAPIKeyNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("APIKeyNotFound", map[string]string{
				"id": id,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("APIKeyRotateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if h.auditStore != nil {
		_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
			ActorID:      u.ID,
			Action:       "api_key_rotate",
			ResourceType: "APIKey",
			ResourceRID:  successor.ID,
		})
	}

	resp := APIKeyRotateResponse{
		ID:                successor.ID,
		Name:              successor.Name,
		RawKey:            raw,
		Prefix:            successor.KeyPrefix,
		Scopes:            scopes,
		CreatedAt:         successor.CreatedAt,
		ExpiresAt:         successor.ExpiresAt,
		PredecessorID:     predecessor.ID,
		PredecessorExpiry: graceUntil,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// Rotations handles GET /api/admin/api-keys/rotations. Returns every
// non-revoked key owned by the calling user whose rotates_at is within the
// configured warning window (default: DefaultAPIKeyRotationWarning, i.e.
// 7 days). ?withinDays=N narrows or widens the window at query time.
func (h *APIKeyHandler) Rotations(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", map[string]string{
			"reason": "no authenticated user in request context",
		}))
		return
	}

	within := DefaultAPIKeyRotationWarning
	if q := strings.TrimSpace(r.URL.Query().Get("withinDays")); q != "" {
		d, err := strconv.Atoi(q)
		if err != nil || d < 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidWithinDays", map[string]string{
				"reason": "withinDays must be a non-negative integer",
			}))
			return
		}
		within = time.Duration(d) * 24 * time.Hour
	}

	now := time.Now()
	rows, err := h.repo.ListPendingRotations(r.Context(), now, within)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("APIKeyRotationsListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	out := APIKeyRotationsResponse{Warnings: make([]APIKeyRotationWarning, 0, len(rows))}
	for _, row := range rows {
		if row.UserID != u.ID {
			continue
		}
		if row.RotatesAt == nil {
			continue
		}
		warn := APIKeyRotationWarning{
			ID:        row.ID,
			Name:      row.Name,
			Prefix:    row.KeyPrefix,
			RotatesAt: *row.RotatesAt,
		}
		if row.SuccessorID != nil {
			warn.SuccessorID = *row.SuccessorID
		}
		out.Warnings = append(out.Warnings, warn)

		if h.auditStore != nil {
			_ = audit.Record(r.Context(), h.auditStore, audit.AuditEvent{
				ActorID:      u.ID,
				Action:       "api_key_rotation_warning",
				ResourceType: "APIKey",
				ResourceRID:  row.ID,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}
