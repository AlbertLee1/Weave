package oss

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/geotemporal"
	"github.com/liyang/weave/pkg/oms"
)

// SetGeotemporalStore wires the geotemporal series store so the handler can
// serve the object-path GeotemporalSeriesProperty endpoints. When nil, those
// routes return GeotemporalStoreNotConfigured.
func (h *Handler) SetGeotemporalStore(store geotemporal.Store) {
	h.geotemporalStore = store
}

// GetGeotemporalLatestValue handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/geotemporalSeries/{propertyName}/latestValue.
func (h *Handler) GetGeotemporalLatestValue(w http.ResponseWriter, r *http.Request) {
	key, ok := h.resolveGeotemporalKey(w, r)
	if !ok {
		return
	}
	v, err := h.geotemporalStore.LatestValue(r.Context(), key)
	if err != nil {
		writeGeotemporalError(w, err)
		return
	}
	writeJSONOK(w, v)
}

// StreamGeotemporalHistoricValues handles
// POST /api/v2/ontologies/{o}/objects/{type}/{pk}/geotemporalSeries/{propertyName}/streamHistoricValues.
func (h *Handler) StreamGeotemporalHistoricValues(w http.ResponseWriter, r *http.Request) {
	key, ok := h.resolveGeotemporalKey(w, r)
	if !ok {
		return
	}
	values, err := h.geotemporalStore.StreamHistoricValues(r.Context(), key)
	if err != nil {
		writeGeotemporalError(w, err)
		return
	}
	if values == nil {
		values = []geotemporal.Value{}
	}
	writeJSONOK(w, values)
}

// resolveGeotemporalKey loads the addressed object (to verify it exists and
// returns 404 for missing objects) and returns the SeriesKey derived from
// the URL parameters. It writes the appropriate error response and returns
// ok=false on any failure.
func (h *Handler) resolveGeotemporalKey(w http.ResponseWriter, r *http.Request) (geotemporal.SeriesKey, bool) {
	if h.geotemporalStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("GeotemporalStoreNotConfigured", nil))
		return geotemporal.SeriesKey{}, false
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")
	propertyName := chi.URLParam(r, "propertyName")

	obj, err := h.svc.GetObject(r.Context(), GetObjectRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		PrimaryKey:  primaryKey,
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectNotFound", map[string]string{
				"objectType": objectType,
				"primaryKey": primaryKey,
			}))
			return geotemporal.SeriesKey{}, false
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectFailed", map[string]string{
			"reason": err.Error(),
		}))
		return geotemporal.SeriesKey{}, false
	}
	_ = obj // existence check only — the series key is derived from the path.

	return geotemporal.SeriesKey{
		Ontology:   ontologyRID,
		ObjectType: objectType,
		PrimaryKey: primaryKey,
		Property:   propertyName,
	}, true
}

func writeGeotemporalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, geotemporal.ErrNoValues):
		apierror.WriteJSON(w, apierror.NewNotFound("GeotemporalValueNotFound", nil))
	default:
		apierror.WriteJSON(w, apierror.NewInternal("GeotemporalStoreError", map[string]string{
			"message": err.Error(),
		}))
	}
}
