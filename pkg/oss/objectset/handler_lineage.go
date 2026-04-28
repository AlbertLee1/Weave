package objectset

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// LineageNode is one vertex in an ObjectSet derivation graph. The graph is a
// tree rooted at the ObjectSet identified by the URL rid; child nodes carry
// the input ObjectSets that the parent operation derives from. Type-specific
// fields are populated only for the operation types they apply to so the
// wire shape stays compact.
type LineageNode struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	ObjectType string `json:"objectType,omitempty"`
	// Filter
	Where json.RawMessage `json:"where,omitempty"`
	// SearchAround
	Link      string     `json:"link,omitempty"`
	Direction string     `json:"direction,omitempty"`
	Path      []PathStep `json:"path,omitempty"`
	// Reference
	Reference string `json:"reference,omitempty"`
	// AsType / static / interfaceBase / interfaceLinkSearchAround / methodInput
	InterfaceType string `json:"interfaceType,omitempty"`
	InterfaceLink string `json:"interfaceLink,omitempty"`
	Input         string `json:"input,omitempty"`
	// Sample
	Size *int   `json:"size,omitempty"`
	Seed *int64 `json:"seed,omitempty"`
	// WithProperties: enumerated derived-property/aggregation specs.
	DerivedProperties []DerivedPropertyDef `json:"derivedProperties,omitempty"`
}

// LineageEdge is a directed parent-pointer in the derivation tree: the input
// ObjectSet (`from`) feeds the operation that produces `to`. Operation
// mirrors the parent node's Type so callers can render the chain without
// re-joining nodes.
type LineageEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Operation string `json:"operation"`
}

// LineageResponse is the wire shape returned by GET /objectSets/{rid}/lineage.
// Root names the synthetic node id of the requested ObjectSet (always present
// in Nodes); leaves of the tree are pure base/static/reference/methodInput
// sets whose derivation stops here.
type LineageResponse struct {
	RID   string        `json:"rid"`
	Root  string        `json:"root"`
	Nodes []LineageNode `json:"nodes"`
	Edges []LineageEdge `json:"edges"`
}

// GetObjectSetLineage handles GET
// /api/v2/ontologies/{ontologyApiName}/objectSets/{objectSetRid}/lineage. It
// resolves the ObjectSet from the in-memory store and walks the Definition
// tree to surface the filter / union / withProperties / searchAround / ...
// operation chain that produces the final set.
func (h *Handler) GetObjectSetLineage(w http.ResponseWriter, r *http.Request) {
	rid := chi.URLParam(r, "objectSetRid")
	def, err := h.store.Get(rid)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ObjectSetNotFound", map[string]string{
			"objectSetRid": rid,
		}))
		return
	}

	resp := buildObjectSetLineage(rid, def)
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// buildObjectSetLineage walks the Definition tree depth-first, assigning each
// node a synthetic id (n0, n1, ...) and emitting a parent-pointer edge from
// each child input to its enclosing operation. Exposed for direct unit tests
// without the HTTP wrapper.
func buildObjectSetLineage(rid string, def *Definition) LineageResponse {
	nodes := make([]LineageNode, 0, 4)
	edges := make([]LineageEdge, 0, 4)
	counter := 0
	root := walkDefinitionLineage(def, &counter, &nodes, &edges)
	return LineageResponse{RID: rid, Root: root, Nodes: nodes, Edges: edges}
}

// walkDefinitionLineage recurses through inputs first so the slice ordering
// mirrors a topological sort (leaves before the root). Returns the synthetic
// id assigned to def.
func walkDefinitionLineage(def *Definition, counter *int, nodes *[]LineageNode, edges *[]LineageEdge) string {
	if def == nil {
		id := nextLineageID(counter)
		*nodes = append(*nodes, LineageNode{ID: id, Type: "unknown"})
		return id
	}

	// Recurse into children first so the child IDs land before the parent in
	// the nodes slice (topological order).
	var childIDs []string
	if def.ObjectSet != nil {
		childIDs = append(childIDs, walkDefinitionLineage(def.ObjectSet, counter, nodes, edges))
	}
	for _, child := range def.ObjectSets {
		childIDs = append(childIDs, walkDefinitionLineage(child, counter, nodes, edges))
	}

	id := nextLineageID(counter)
	node := LineageNode{ID: id, Type: def.Type}
	populateLineageNode(&node, def)
	*nodes = append(*nodes, node)

	for _, childID := range childIDs {
		*edges = append(*edges, LineageEdge{From: childID, To: id, Operation: def.Type})
	}
	return id
}

// populateLineageNode copies the operation-specific fields from def onto node
// based on the ObjectSet type. Type-specific fields are set only when they
// apply (so the wire shape stays compact via `omitempty`); types not listed
// here surface only their `type` discriminator, which is enough to render the
// step (e.g. nearestNeighbors / asBaseObjectTypes).
func populateLineageNode(node *LineageNode, def *Definition) {
	switch def.Type {
	case "base", "static", "asType":
		node.ObjectType = def.ObjectType
	case "filter":
		node.Where = def.Where
	case "searchAround":
		node.Link = def.Link
		node.Direction = def.Direction
		node.Path = def.Path
	case "reference":
		node.Reference = def.Reference
	case "interfaceBase":
		node.InterfaceType = def.InterfaceType
	case "interfaceLinkSearchAround":
		node.InterfaceLink = def.InterfaceLink
	case "methodInput":
		node.Input = def.Input
	case "sample":
		node.Size = def.Size
		node.Seed = def.Seed
	case "withProperties":
		// Surface every derived property (link-hop aggregations like
		// count/sum/avg AND formula-based metrics) so the operation chain
		// reflects the aggregation step.
		if len(def.DerivedProperties) > 0 {
			node.DerivedProperties = append([]DerivedPropertyDef(nil), def.DerivedProperties...)
		}
	}
}

// nextLineageID returns a stable sequential synthetic id ("n0", "n1", ...).
// The counter is advanced in place so the caller can interleave node and
// edge construction without bookkeeping.
func nextLineageID(counter *int) string {
	id := "n" + strconv.Itoa(*counter)
	*counter++
	return id
}
