package oss

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

// activityPageSizeDefault and activityPageSizeMax bound the server-side
// pageSize. The defaults match the broader OSS pagination convention so
// downstream UIs get familiar batch sizes.
const (
	activityPageSizeDefault = 50
	activityPageSizeMax     = 200
)

// SetActivityStore wires the per-object activity timeline store (US-312).
// When nil the GET .../activity route returns ActivityStoreNotConfigured —
// matches the AttachmentStore / TimeSeriesStore degraded-mode shape.
func (h *Handler) SetActivityStore(store oms.ObjectActivityStore) {
	h.activityStore = store
}

// ObjectActivityResponse is the wire shape for the activity feed endpoint.
// `Data` is ordered by version DESC. `NextPageToken` is empty on the final
// page and otherwise carries an opaque cursor that the client passes back
// via ?pageToken= to fetch the previous (older) page.
type ObjectActivityResponse struct {
	Data          []oms.ObjectHistory `json:"data"`
	NextPageToken string              `json:"nextPageToken,omitempty"`
}

// GetObjectActivity handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/activity.
//
// Pagination is cursor-based on the monotonically increasing per-PK
// `version` column: the server returns up to pageSize+1 rows ordered
// version DESC, slices off the trailing probe row when the page is full,
// and emits the trailing row's version as the next-page token. Tokens are
// opaque (base64 of the decimal version) so future migrations can switch
// the column without breaking clients.
func (h *Handler) GetObjectActivity(w http.ResponseWriter, r *http.Request) {
	if h.activityStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActivityStoreNotConfigured", nil))
		return
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")

	ot, err := h.omsRepo.GetObjectTypeByAPIName(r.Context(), ontologyRID, objectType)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectType": objectType,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ListActivityFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	pageSize := activityPageSizeDefault
	if raw := r.URL.Query().Get("pageSize"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPageSize", map[string]string{
				"pageSize": raw,
			}))
			return
		}
		if n > activityPageSizeMax {
			n = activityPageSizeMax
		}
		pageSize = n
	}

	beforeVersion, ok := decodeActivityPageToken(r.URL.Query().Get("pageToken"))
	if !ok {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPageToken", map[string]string{
			"pageToken": r.URL.Query().Get("pageToken"),
		}))
		return
	}

	rows, err := h.activityStore.ListObjectHistoryPage(r.Context(), ot.RID, primaryKey, beforeVersion, pageSize+1)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ActivityStoreError", map[string]string{
			"message": err.Error(),
		}))
		return
	}

	resp := ObjectActivityResponse{Data: rows}
	if len(rows) > pageSize {
		resp.Data = rows[:pageSize]
		resp.NextPageToken = encodeActivityPageToken(resp.Data[len(resp.Data)-1].Version)
	}
	if resp.Data == nil {
		resp.Data = []oms.ObjectHistory{}
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// decodeActivityPageToken converts the opaque pageToken back to its
// "before-version" int64. An empty token decodes to 0 (fetch newest page).
// A non-empty but malformed token is a 400 — the bool return signals that.
func decodeActivityPageToken(token string) (int64, bool) {
	if token == "" {
		return 0, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// encodeActivityPageToken renders an int64 as the opaque next-page cursor.
// The base64 wrapping keeps the token shape uniform across the API even
// though the underlying representation is just a decimal int.
func encodeActivityPageToken(version int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(version, 10)))
}
