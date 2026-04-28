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
// rejected with 404 LineageNotConfigured rather than panicking.
type Handler struct {
	store oms.LineageStore
}

// NewHandler binds a Handler to a LineageStore. Pass nil for degraded
// mode — the route is still mounted and returns 404 LineageNotConfigured.
func NewHandler(store oms.LineageStore) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes mounts the lineage endpoint on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/objects/{rid}/lineage", h.GetLineage)
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
