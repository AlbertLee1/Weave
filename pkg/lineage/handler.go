// Package lineage exposes the read-side of the lineage_edges table written
// by pkg/actions and the pipeline ingest paths. The platform answers
// "where did this object come from?" via a depth-bounded BFS over the
// directed provenance graph rooted at any RID.
//
// Wire shape (graph):
//
//	{
//	  "root":      "<rid>",
//	  "direction": "upstream|downstream|both",
//	  "depth":     1..MaxDepth,
//	  "truncated": true|false,
//	  "nodes": [{"rid": "...", "type": "..."}, ...],
//	  "edges": [{"from":"...","to":"...","operation":"...","timestamp":"..."}, ...]
//	}
//
// US-300.
package lineage

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/oms"
)

// Direction values recognised on the ?direction= query parameter.
const (
	DirectionUpstream   = "upstream"
	DirectionDownstream = "downstream"
	DirectionBoth       = "both"
)

// DefaultDepth is the BFS depth used when the caller omits ?depth=.
// Most UI lineage panels open one hop; deeper traversals are explicit.
const DefaultDepth = 1

// MaxDepth caps the BFS depth so a malicious caller cannot turn the
// endpoint into an unbounded graph walk. 10 is comfortably above any
// realistic UI surface.
const MaxDepth = 10

// pageLimit is the per-node neighbour fanout. When ListUpstream/Downstream
// returns this many rows the response surfaces Truncated=true so callers
// know unseen edges exist beyond the BFS frontier.
const pageLimit = 200

// Handler serves GET /api/v2/objects/{rid}/lineage. The store may be nil
// in degraded-mode (no PG) bootstraps; in that case every request is
// rejected with 404 LineageNotConfigured rather than panicking. The
// optional columnStore (US-377) backs the property-level read endpoints
// — when nil those routes return 404 ColumnLineageNotConfigured but the
// object-level GetLineage path is unaffected.
type Handler struct {
	store       oms.LineageStore
	columnStore oms.ColumnLineageStore
}

// NewHandler binds a Handler to a LineageStore. Pass nil for degraded
// mode — the route is still mounted and returns 404 LineageNotConfigured.
func NewHandler(store oms.LineageStore) *Handler {
	return &Handler{store: store}
}

// SetColumnLineageStore wires the optional column-level lineage store
// (US-377). When set the property-level read endpoints serve real data;
// when nil they return 404 ColumnLineageNotConfigured but stay
// discoverable so SDKs / curl can probe the contract in degraded mode.
func (h *Handler) SetColumnLineageStore(s oms.ColumnLineageStore) {
	h.columnStore = s
}

// RegisterRoutes mounts the lineage endpoints on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/objects/{rid}/lineage", h.GetLineage)
	// US-377: column-level lineage. Two routes — one for upstream
	// (which dataset columns feed this property?), one for the reverse
	// impact analysis (which downstream properties break if this
	// dataset column goes away?).
	r.Get("/api/v2/lineage/property/{rid}", h.GetPropertyLineage)
	r.Get("/api/v2/lineage/dataset-columns/impact", h.GetDatasetColumnImpact)
}

// Node is one vertex in the lineage graph.
type Node struct {
	RID  string `json:"rid"`
	Type string `json:"type,omitempty"`
}

// Edge is one directed provenance relation in the lineage graph.
type Edge struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Operation string    `json:"operation,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Response is the wire shape returned by GET /api/v2/objects/{rid}/lineage.
type Response struct {
	Root      string `json:"root"`
	Direction string `json:"direction"`
	Depth     int    `json:"depth"`
	Truncated bool   `json:"truncated"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
}

// GetLineage executes a depth-bounded BFS from the rid path parameter.
// Direction=upstream traverses lineage_edges via downstream_rid (back to
// the source); direction=downstream walks the inverse; direction=both
// explores in both directions in lockstep.
func (h *Handler) GetLineage(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("LineageNotConfigured", nil))
		return
	}

	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRID", map[string]string{"rid": rid}))
		return
	}

	direction := strings.TrimSpace(r.URL.Query().Get("direction"))
	if direction == "" {
		direction = DirectionUpstream
	}
	if direction != DirectionUpstream && direction != DirectionDownstream && direction != DirectionBoth {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidDirection", map[string]string{
			"direction": direction,
			"allowed":   "upstream|downstream|both",
		}))
		return
	}

	depth := DefaultDepth
	if raw := strings.TrimSpace(r.URL.Query().Get("depth")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxDepth {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidDepth", map[string]string{
				"depth":   raw,
				"minimum": "1",
				"maximum": strconv.Itoa(MaxDepth),
			}))
			return
		}
		depth = n
	}

	visitedNodes := map[string]struct{}{rid: {}}
	nodeOrder := []string{rid}
	type edgeKey struct {
		from string
		to   string
		op   string
		ts   time.Time
	}
	visitedEdges := map[edgeKey]struct{}{}
	var edges []Edge
	truncated := false

	queue := []string{rid}
	for level := 0; level < depth && len(queue) > 0; level++ {
		var next []string
		for _, cur := range queue {
			if direction == DirectionUpstream || direction == DirectionBoth {
				got, err := h.store.ListUpstreamLineage(r.Context(), cur, pageLimit)
				if err != nil {
					apierror.WriteJSON(w, apierror.NewInternal("LineageQueryFailed", map[string]string{
						"message": err.Error(),
					}))
					return
				}
				if len(got) >= pageLimit {
					truncated = true
				}
				for _, e := range got {
					k := edgeKey{from: e.UpstreamRID, to: e.DownstreamRID, op: e.Operation, ts: e.Timestamp}
					if _, dup := visitedEdges[k]; dup {
						continue
					}
					visitedEdges[k] = struct{}{}
					edges = append(edges, Edge{
						From: e.UpstreamRID, To: e.DownstreamRID,
						Operation: e.Operation, Timestamp: e.Timestamp,
					})
					if _, seen := visitedNodes[e.UpstreamRID]; !seen {
						visitedNodes[e.UpstreamRID] = struct{}{}
						nodeOrder = append(nodeOrder, e.UpstreamRID)
						next = append(next, e.UpstreamRID)
					}
				}
			}
			if direction == DirectionDownstream || direction == DirectionBoth {
				got, err := h.store.ListDownstreamLineage(r.Context(), cur, pageLimit)
				if err != nil {
					apierror.WriteJSON(w, apierror.NewInternal("LineageQueryFailed", map[string]string{
						"message": err.Error(),
					}))
					return
				}
				if len(got) >= pageLimit {
					truncated = true
				}
				for _, e := range got {
					k := edgeKey{from: e.UpstreamRID, to: e.DownstreamRID, op: e.Operation, ts: e.Timestamp}
					if _, dup := visitedEdges[k]; dup {
						continue
					}
					visitedEdges[k] = struct{}{}
					edges = append(edges, Edge{
						From: e.UpstreamRID, To: e.DownstreamRID,
						Operation: e.Operation, Timestamp: e.Timestamp,
					})
					if _, seen := visitedNodes[e.DownstreamRID]; !seen {
						visitedNodes[e.DownstreamRID] = struct{}{}
						nodeOrder = append(nodeOrder, e.DownstreamRID)
						next = append(next, e.DownstreamRID)
					}
				}
			}
		}
		queue = next
	}

	nodes := make([]Node, 0, len(nodeOrder))
	for _, nrid := range nodeOrder {
		nodes = append(nodes, Node{RID: nrid, Type: NodeType(nrid)})
	}
	if edges == nil {
		edges = []Edge{}
	}

	resp := Response{
		Root:      rid,
		Direction: direction,
		Depth:     depth,
		Truncated: truncated,
		Nodes:     nodes,
		Edges:     edges,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// NodeType extracts the canonical resource-type segment from an RID. The
// shape is "ri.{service}.{realm}.{resourceType}.{...}" — anything that
// doesn't parse cleanly returns "" so callers can render the node without
// a label rather than crashing.
func NodeType(rid string) string {
	parts := strings.Split(rid, ".")
	if len(parts) < 5 || parts[0] != "ri" {
		return ""
	}
	return parts[3]
}

// columnPageLimit caps the per-call row count returned by the
// property-lineage and reverse-impact endpoints so a runaway listing
// cannot drag the whole table back through a single HTTP response.
// Mirrors pageLimit (200) so the operational mental model is uniform.
const columnPageLimit = 200

// PropertyUpstream is one upstream column reference rendered for the
// GET /api/v2/lineage/property/{rid} response.
type PropertyUpstream struct {
	BindingRID    string    `json:"bindingRid"`
	SrcDatasetRID string    `json:"srcDatasetRid"`
	SrcColumn     string    `json:"srcColumn"`
	Timestamp     time.Time `json:"timestamp"`
}

// PropertyLineageResponse is the wire shape returned by
// GET /api/v2/lineage/property/{rid}.
type PropertyLineageResponse struct {
	PropertyRID string             `json:"propertyRid"`
	Upstream    []PropertyUpstream `json:"upstream"`
	Truncated   bool               `json:"truncated"`
}

// DatasetColumnImpactedProperty is one downstream property reference
// rendered for the GET /api/v2/lineage/dataset-columns/impact response.
type DatasetColumnImpactedProperty struct {
	BindingRID         string    `json:"bindingRid"`
	DstObjectTypeRID   string    `json:"dstObjectTypeRid"`
	DstPropertyRID     string    `json:"dstPropertyRid"`
	DstPropertyAPIName string    `json:"dstPropertyApiName"`
	Timestamp          time.Time `json:"timestamp"`
}

// DatasetColumnImpactResponse is the wire shape returned by
// GET /api/v2/lineage/dataset-columns/impact?dataset=...&column=....
type DatasetColumnImpactResponse struct {
	DatasetRID string                          `json:"datasetRid"`
	Column     string                          `json:"column"`
	Impacted   []DatasetColumnImpactedProperty `json:"impacted"`
	Truncated  bool                            `json:"truncated"`
}

// GetPropertyLineage answers "which dataset columns feed this property?"
// by listing every ColumnLineageEdge whose dst_property_rid matches the
// supplied path parameter. Newest-first by ts. Returns 404
// ColumnLineageNotConfigured when the column-level store is unwired.
func (h *Handler) GetPropertyLineage(w http.ResponseWriter, r *http.Request) {
	if h.columnStore == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ColumnLineageNotConfigured", nil))
		return
	}
	propRID := strings.TrimSpace(chi.URLParam(r, "rid"))
	if propRID == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPropertyRID", map[string]string{
			"rid": propRID,
		}))
		return
	}
	edges, err := h.columnStore.ListUpstreamColumnLineageForProperty(r.Context(), propRID, columnPageLimit)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ColumnLineageQueryFailed", map[string]string{
			"message": err.Error(),
		}))
		return
	}
	upstream := make([]PropertyUpstream, 0, len(edges))
	for _, e := range edges {
		upstream = append(upstream, PropertyUpstream{
			BindingRID:    e.BindingRID,
			SrcDatasetRID: e.SrcDatasetRID,
			SrcColumn:     e.SrcColumn,
			Timestamp:     e.Timestamp,
		})
	}
	resp := PropertyLineageResponse{
		PropertyRID: propRID,
		Upstream:    upstream,
		Truncated:   len(edges) >= columnPageLimit,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetDatasetColumnImpact is the reverse-impact analysis: given an
// upstream (dataset, column) pair, list every downstream property that
// derives from it. Used by admin tooling to answer "what breaks if I
// drop this column?" before applying a destructive schema change.
func (h *Handler) GetDatasetColumnImpact(w http.ResponseWriter, r *http.Request) {
	if h.columnStore == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ColumnLineageNotConfigured", nil))
		return
	}
	dataset := strings.TrimSpace(r.URL.Query().Get("dataset"))
	column := strings.TrimSpace(r.URL.Query().Get("column"))
	if dataset == "" || column == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingDatasetOrColumn", map[string]string{
			"dataset": dataset,
			"column":  column,
		}))
		return
	}
	edges, err := h.columnStore.ListDownstreamColumnLineageForDatasetColumn(r.Context(), dataset, column, columnPageLimit)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ColumnLineageQueryFailed", map[string]string{
			"message": err.Error(),
		}))
		return
	}
	impacted := make([]DatasetColumnImpactedProperty, 0, len(edges))
	for _, e := range edges {
		impacted = append(impacted, DatasetColumnImpactedProperty{
			BindingRID:         e.BindingRID,
			DstObjectTypeRID:   e.DstObjectTypeRID,
			DstPropertyRID:     e.DstPropertyRID,
			DstPropertyAPIName: e.DstPropertyAPIName,
			Timestamp:          e.Timestamp,
		})
	}
	resp := DatasetColumnImpactResponse{
		DatasetRID: dataset,
		Column:     column,
		Impacted:   impacted,
		Truncated:  len(edges) >= columnPageLimit,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
