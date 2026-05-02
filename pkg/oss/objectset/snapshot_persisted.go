package objectset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oss"
)

// PersistedSnapshot is the in-memory shape passed to PersistedSnapshotStore
// when the handler freezes an ObjectSet. The cmd/server adapter is
// responsible for marshalling Definition into the JSONB column on the
// underlying object_set_snapshots row and unmarshalling it back on read.
//
// US-365 fields (DefinitionHash / SnapshotAt / IsImmutable) record the
// canonical-JSON sha256 of Definition, the snapshot transaction id assigned
// at create time, and the immutability flag (always true for snapshots —
// they exist precisely to provide byte-for-byte identical re-loads). These
// fields propagate through the cmd/server adapter into the object_set_snapshots
// columns added by migration 000082.
type PersistedSnapshot struct {
	RID             string
	OntologyAPIName string
	ObjectType      string
	Definition      *Definition
	PrimaryKeys     []string
	Truncated       bool
	CreatedBy       string
	CreatedAt       time.Time
	DefinitionHash  string
	SnapshotAt      int64
	IsImmutable     bool
}

// ErrSnapshotNotFound is the sentinel a PersistedSnapshotStore should return
// when no row matches the requested rid. The handler maps it to a 404
// SnapshotNotFound apierror.
var ErrSnapshotNotFound = errors.New("object set snapshot not found")

// PersistedSnapshotStore is the narrow read/write surface the snapshot
// handler depends on. The cmd/server bootstrap satisfies it via an adapter
// over the uncached *PGRepository (oms.ObjectSetSnapshotStore); test routers
// can plug an in-memory fake. Keeping it local to pkg/oss/objectset matches
// the same direction-of-dependency rule used by HistorySnapshotProvider:
// pkg/oms never imports pkg/oss/objectset, so all bridge logic lives in
// cmd/server.
type PersistedSnapshotStore interface {
	CreatePersistedSnapshot(ctx context.Context, snap *PersistedSnapshot) error
	GetPersistedSnapshot(ctx context.Context, rid string) (*PersistedSnapshot, error)
}

// SetPersistedSnapshotStore wires the optional US-224 snapshot store. When
// attached the CreateSnapshot / GetSnapshot routes are functional; absent
// they return SnapshotsUnavailable 400 so degraded-mode test routers without
// PG keep mounting the routes for contract-test discoverability.
func (h *Handler) SetPersistedSnapshotStore(store PersistedSnapshotStore) {
	h.persistedSnapshots = store
}

// CreateSnapshotResponse is the wire shape returned by CreateSnapshot.
//
// US-365 added DefinitionHash, SnapshotAt, IsImmutable so SDKs can verify
// they got back the exact definition they posted (hash) and chain follow-up
// reads against the snapshot transaction id that was allocated.
type CreateSnapshotResponse struct {
	SnapshotRID    string    `json:"snapshotRid"`
	ObjectType     string    `json:"objectType"`
	PrimaryKeys    []string  `json:"primaryKeys"`
	TotalCount     string    `json:"totalCount"`
	Truncated      bool      `json:"truncated,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	DefinitionHash string    `json:"definitionHash,omitempty"`
	SnapshotAt     int64     `json:"snapshotAt,omitempty"`
	IsImmutable    bool      `json:"isImmutable"`
}

// GetSnapshotResponse is the wire shape returned by GetSnapshot. It mirrors
// the LoadObjectSetResponse envelope so SDK consumers can reuse the same
// row decoder, with the snapshot identity + creation time stamped on the
// outer object.
type GetSnapshotResponse struct {
	SnapshotRID    string            `json:"snapshotRid"`
	ObjectType     string            `json:"objectType"`
	Data           []*oss.WireObject `json:"data"`
	TotalCount     string            `json:"totalCount"`
	CreatedAt      time.Time         `json:"createdAt"`
	DefinitionHash string            `json:"definitionHash,omitempty"`
	SnapshotAt     int64             `json:"snapshotAt,omitempty"`
	IsImmutable    bool              `json:"isImmutable"`
}

// CreateSnapshot handles POST
//
//	/api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/snapshot
//
// It reads the previously-stored ObjectSet (via the in-memory Store the same
// CreateTemporary endpoint populates), executes it once, and freezes the
// resulting (ObjectType, PrimaryKeys, Truncated) tuple alongside the source
// Definition so a future GetSnapshot call returns the same membership even
// after the underlying base set mutates.
func (h *Handler) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.persistedSnapshots == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SnapshotsUnavailable", map[string]string{
			"reason": "persisted snapshot store is not configured on this server",
		}))
		return
	}

	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	objectSetRid := chi.URLParam(r, "objectSetRid")

	def, err := h.store.Get(objectSetRid)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ObjectSetNotFound", map[string]string{
			"objectSetRid": objectSetRid,
		}))
		return
	}

	ctx := WithOntologyScope(r.Context(), ontologyAPIName)
	result, err := h.executor.Execute(ctx, def)
	if err != nil {
		apierror.WriteJSON(w, executeError(err))
		return
	}

	defJSON, _ := json.Marshal(def)
	snap := &PersistedSnapshot{
		RID:             fmt.Sprintf("ri.objectsets.main.snapshot.%s", uuid.New().String()),
		OntologyAPIName: ontologyAPIName,
		ObjectType:      result.ObjectType,
		Definition:      def,
		PrimaryKeys:     append([]string(nil), result.PrimaryKeys...),
		Truncated:       result.Truncated,
		CreatedAt:       time.Now().UTC(),
		DefinitionHash:  HashDefinition(defJSON),
		SnapshotAt:      NextSnapshotAt(),
		IsImmutable:     true,
	}
	if u := auth.UserFromContext(ctx); u != nil {
		snap.CreatedBy = u.ID
	}

	if err := h.persistedSnapshots.CreatePersistedSnapshot(ctx, snap); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SnapshotPersistFailed", map[string]string{
			"error": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, &CreateSnapshotResponse{
		SnapshotRID:    snap.RID,
		ObjectType:     snap.ObjectType,
		PrimaryKeys:    snap.PrimaryKeys,
		TotalCount:     strconv.Itoa(len(snap.PrimaryKeys)),
		Truncated:      snap.Truncated,
		CreatedAt:      snap.CreatedAt,
		DefinitionHash: snap.DefinitionHash,
		SnapshotAt:     snap.SnapshotAt,
		IsImmutable:    snap.IsImmutable,
	})
}

// GetSnapshot handles GET
//
//	/api/v2/ontologies/{ontologyApiName}/objectSets/snapshots/{snapshotRid}
//
// Loads the frozen PrimaryKeys list from the persisted snapshot and renders
// each row through the live Bleve index using the same WireObject envelope
// LoadObjects produces. PK membership is frozen at create time; current
// property values are returned because the v1 design only freezes membership
// (not historical property snapshots).
func (h *Handler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.persistedSnapshots == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SnapshotsUnavailable", map[string]string{
			"reason": "persisted snapshot store is not configured on this server",
		}))
		return
	}

	snapshotRID := chi.URLParam(r, "snapshotRid")
	ctx := WithOntologyScope(r.Context(), chi.URLParam(r, "ontologyApiName"))

	snap, err := h.persistedSnapshots.GetPersistedSnapshot(ctx, snapshotRID)
	if err != nil {
		if errors.Is(err, ErrSnapshotNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("SnapshotNotFound", map[string]string{
				"snapshotRid": snapshotRID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SnapshotLookupFailed", map[string]string{
			"error": err.Error(),
		}))
		return
	}

	data := make([]*oss.WireObject, 0, len(snap.PrimaryKeys))
	if h.indexMgr != nil && snap.ObjectType != "" {
		for _, pk := range snap.PrimaryKeys {
			searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{pk}))
			searchReq.Fields = []string{"*"}
			searchReq.Size = 1

			res, err := h.indexMgr.Search(scopedIndexKey(ctx, h.indexMgr, snap.ObjectType), searchReq)
			if err != nil || len(res.Hits) == 0 {
				continue
			}
			data = append(data, oss.FormatObject(snap.ObjectType, pk, res.Hits[0].Fields))
		}
	}

	data, err = h.applyPropertyVisibility(ctx, snap.ObjectType, data)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PropertyFilterFailed", map[string]string{
			"error": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, &GetSnapshotResponse{
		SnapshotRID:    snap.RID,
		ObjectType:     snap.ObjectType,
		Data:           data,
		TotalCount:     strconv.Itoa(len(snap.PrimaryKeys)),
		CreatedAt:      snap.CreatedAt,
		DefinitionHash: snap.DefinitionHash,
		SnapshotAt:     snap.SnapshotAt,
		IsImmutable:    snap.IsImmutable,
	})
}
