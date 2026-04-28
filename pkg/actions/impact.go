// US-301: Action 变更追踪. The impact endpoint answers "which objects did
// this action invocation change?". It reads the lineage_edges rows written
// during commit (executor.recordLineage) where upstream_rid is the action-
// log's canonical RID, and renders the affected objects as a flat list.
//
// Wire shape:
//
//	{
//	  "actionRid": "ri.actions.main.action-log.42",
//	  "actionLog": { ... } | null,   // populated when the log row can be loaded
//	  "objects": [
//	    {"rid":"...","objectType":"Employee","primaryKey":"EMP-001",
//	     "operation":"MODIFY","timestamp":"..."}, ...
//	  ],
//	  "truncated": true|false
//	}
//
// Always emits an empty array for objects when no edges are stored so SDKs
// can read `len(objects)` without nil-checks.
package actions

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
)

// actionLogRIDPrefix is the canonical prefix produced by
// oms.ActionLogLineageRID. Any RID outside this prefix is rejected with
// InvalidActionRID so callers get a clean 400 instead of an empty result.
const actionLogRIDPrefix = "ri.actions.main.action-log."

// impactPageLimit caps the per-request edge fanout. When ListDownstreamLineage
// returns at least this many rows the response surfaces truncated=true so
// callers know more impacted objects exist beyond the window. Matches the
// pkg/lineage handler's pageLimit so both surfaces agree on "this many is
// the boundary".
const impactPageLimit = 200

// ImpactObject describes a single object touched by an action invocation,
// flattened from a lineage_edges row.
type ImpactObject struct {
	RID        string    `json:"rid"`
	ObjectType string    `json:"objectType,omitempty"`
	PrimaryKey string    `json:"primaryKey,omitempty"`
	Operation  string    `json:"operation,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// ImpactResponse is the wire shape returned by GET /api/v2/actions/{rid}/impact.
type ImpactResponse struct {
	ActionRID string         `json:"actionRid"`
	ActionLog *oms.ActionLog `json:"actionLog,omitempty"`
	Objects   []ImpactObject `json:"objects"`
	Truncated bool           `json:"truncated"`
}

// Impact handles GET /api/v2/actions/{rid}/impact.
//
// {rid} is the action-log RID produced by oms.ActionLogLineageRID — the same
// upstream RID the executor stamps onto every lineage edge during
// recordLineage. The handler:
//  1. Validates the RID prefix; non-action-log RIDs are rejected with
//     InvalidActionRID 400.
//  2. Best-effort fetches the underlying ActionLog row (parsed id from the
//     RID's suffix) so callers can correlate the action with its parameters
//     and timestamp without a second roundtrip. A missing / non-existent
//     row is silently omitted (impact is still discoverable from edges).
//  3. Lists downstream lineage edges and flattens them into ImpactObject
//     rows, decoding objectType+primaryKey when the RID matches the
//     phonograph2-objects shape produced by oms.ObjectLineageRID.
//
// Returns 404 ImpactNotConfigured when no LineageStore is wired (degraded
// mode), mirroring the pkg/lineage GetLineage contract. Always emits an
// empty array for objects when no edges are stored so SDKs can read
// len(objects) without nil-checks.
func (h *Handler) Impact(w http.ResponseWriter, r *http.Request) {
	store := h.executor.LineageStore()
	if store == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ImpactNotConfigured", nil))
		return
	}

	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingActionRID", nil))
		return
	}
	if !strings.HasPrefix(rid, actionLogRIDPrefix) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidActionRID", map[string]string{
			"rid":      rid,
			"expected": actionLogRIDPrefix + "<id>",
		}))
		return
	}

	resp := ImpactResponse{
		ActionRID: rid,
		Objects:   []ImpactObject{},
	}

	// Best-effort enrichment: look up the action log row by its parsed id.
	// A missing log (legacy edges, manually-inserted rows, ...) leaves
	// ActionLog nil rather than failing the request — the lineage view is
	// the authoritative answer.
	if id, ok := parseActionLogID(rid); ok && h.executor.omsRepo != nil {
		log, err := h.executor.omsRepo.GetActionLog(r.Context(), id)
		if err == nil && log != nil {
			resp.ActionLog = log
		} else if err != nil && !errors.Is(err, oms.ErrNotFound) {
			// Surface unexpected store failures so operators don't see a
			// silently-missing actionLog field on a transient PG hiccup.
			apierror.WriteJSON(w, apierror.NewInternal("ImpactQueryFailed", map[string]string{
				"message": err.Error(),
			}))
			return
		}
	}

	edges, err := store.ListDownstreamLineage(r.Context(), rid, impactPageLimit)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ImpactQueryFailed", map[string]string{
			"message": err.Error(),
		}))
		return
	}
	if len(edges) >= impactPageLimit {
		resp.Truncated = true
	}
	for _, edge := range edges {
		objType, pk := parseObjectLineageRID(edge.DownstreamRID)
		resp.Objects = append(resp.Objects, ImpactObject{
			RID:        edge.DownstreamRID,
			ObjectType: objType,
			PrimaryKey: pk,
			Operation:  edge.Operation,
			Timestamp:  edge.Timestamp,
		})
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// parseActionLogID pulls the BIGSERIAL id out of an action-log RID. Returns
// (0, false) when the suffix is missing or non-numeric so the caller can
// skip enrichment without surfacing a confusing error.
func parseActionLogID(rid string) (int64, bool) {
	if !strings.HasPrefix(rid, actionLogRIDPrefix) {
		return 0, false
	}
	suffix := strings.TrimPrefix(rid, actionLogRIDPrefix)
	if suffix == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(suffix, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// parseObjectLineageRID is the inverse of oms.ObjectLineageRID. The wire
// format is "ri.phonograph2-objects.main.object.<objectType>.<primaryKey>"
// (objectType segment optional). PrimaryKey may itself contain dots (composite
// keys, RIDs as PKs) so we split out the fixed prefix once and treat
// EVERYTHING after the objectType as the primary key. Returns ("", "") for
// non-object RIDs so callers can render impacted resources of other shapes
// (pipeline runs, ingest sessions, etc.) without empty-string artefacts.
func parseObjectLineageRID(rid string) (objectType, primaryKey string) {
	const prefix = "ri.phonograph2-objects.main.object."
	if !strings.HasPrefix(rid, prefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(rid, prefix)
	if rest == "" {
		return "", ""
	}
	// Two shapes: "<objectType>.<primaryKey>" (modern) or "<primaryKey>"
	// (legacy single-segment). Heuristic: when there is no dot, the entire
	// rest is the primary key; otherwise the FIRST segment is the type and
	// the remainder (which may itself contain dots) is the key.
	idx := strings.IndexByte(rest, '.')
	if idx < 0 {
		return "", rest
	}
	return rest[:idx], rest[idx+1:]
}
