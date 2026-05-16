package oss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/scenarios"
)

// ScenarioIDHeader is the HTTP header that selects a scenario overlay for
// any Ontology Read request. Empty / absent header means "read base".
const ScenarioIDHeader = "X-Scenario-Id"

// ScenarioReader is the persistence subset the OSS overlay needs.
// pkg/scenarios.Repo satisfies it; tests can swap in a fake.
type ScenarioReader interface {
	GetScenario(ctx context.Context, rid string) (*scenarios.Scenario, error)
	ListEdits(ctx context.Context, rid string) ([]scenarios.ScenarioEdit, error)
}

// SetScenarioReader wires the scenario reader for X-Scenario-Id overlay on
// read endpoints. When nil (the default), the header is silently ignored.
func (h *Handler) SetScenarioReader(r ScenarioReader) {
	h.scenarioReader = r
}

// SetScenarioConflictAuditor wires the US-481 audit sink that records one
// row per Read request whose fold surfaces ≥1 ScenarioConflict. Nil disables
// auditing (degraded-mode default).
func (h *Handler) SetScenarioConflictAuditor(a *ScenarioConflictAuditor) {
	h.scenarioConflictAuditor = a
}

// ScenarioOverlay caches scenario metadata + edits for one HTTP request so
// each handler doesn't re-query the scenarios store.
type ScenarioOverlay struct {
	Scenario *scenarios.Scenario
	Edits    []scenarios.ScenarioEdit
}

// loadScenarioOverlay extracts the X-Scenario-Id header. Returns:
//   - (nil, nil) — header absent or no reader wired (no overlay path)
//   - (*overlay, nil) — scenario exists and matches the request's ontology
//   - (nil, *apiErr) — 404 ScenarioNotFound or 409 ScenarioOntologyMismatch
//
// Validation is done before the underlying GetObject / ListObjects fetch so
// callers do not waste DB work on a bad header.
func (h *Handler) loadScenarioOverlay(ctx context.Context, r *http.Request, ontologyRID string) (*ScenarioOverlay, *apierror.APIError) {
	sid := r.Header.Get(ScenarioIDHeader)
	if sid == "" || h.scenarioReader == nil {
		return nil, nil
	}
	scen, err := h.scenarioReader.GetScenario(ctx, sid)
	if errors.Is(err, scenarios.ErrScenarioNotFound) || (err == nil && scen == nil) {
		return nil, apierror.NewNotFound("ScenarioNotFound", map[string]string{"scenarioId": sid})
	}
	if err != nil {
		return nil, apierror.NewInternal("ScenarioLookupFailed", map[string]string{
			"scenarioId": sid,
			"reason":     err.Error(),
		})
	}
	if scen.ParentOntologyCommit != ontologyRID {
		return nil, apierror.NewConflict("ScenarioOntologyMismatch", map[string]string{
			"scenarioId":             sid,
			"expectedOntologyCommit": ontologyRID,
			"actualOntologyCommit":   scen.ParentOntologyCommit,
		})
	}
	edits, err := h.scenarioReader.ListEdits(ctx, sid)
	if err != nil {
		return nil, apierror.NewInternal("ScenarioEditsListFailed", map[string]string{
			"scenarioId": sid,
			"reason":     err.Error(),
		})
	}
	return &ScenarioOverlay{Scenario: scen, Edits: edits}, nil
}

// applyToObject folds the overlay's edits onto a single WireObject. Returns
// (overlaid, deleted). When deleted=true the caller should respond 404
// because the scenario removed this object. Conflicts surfaced by fold are
// silently dropped — callers that need audit visibility should call
// applyToObjectWithConflicts instead.
func (o *ScenarioOverlay) applyToObject(obj *WireObject) (*WireObject, bool) {
	overlaid, deleted, _ := o.applyToObjectWithConflicts(obj)
	return overlaid, deleted
}

// applyToObjectWithConflicts is the US-481 superset of applyToObject. It
// additionally returns any ScenarioConflict the fold surfaced for the
// per-object key. The conflicts list is empty when no edit touches the key
// or the touching edits replay cleanly.
func (o *ScenarioOverlay) applyToObjectWithConflicts(obj *WireObject) (*WireObject, bool, []scenarios.ScenarioConflict) {
	if o == nil || obj == nil {
		return obj, false, nil
	}
	target := scenarios.ObjectKey{ObjectType: obj.APIName, ObjectID: fmt.Sprintf("%v", obj.PrimaryKey)}
	baseView := wireObjectToView(obj)
	folded, deleted, conflicts := scenarios.FoldObjectWithConflicts(target, baseView, o.Edits)
	if deleted {
		return nil, true, conflicts
	}
	if folded == nil {
		// No edit touched this key — return original.
		return obj, false, conflicts
	}
	return viewToWireObject(folded, obj), false, conflicts
}

// applyToPage folds the overlay's edits onto every row in an ObjectPage.
// Deleted rows are filtered out. Order is preserved. The page is returned
// unchanged if the overlay or the page itself is nil / empty. Conflicts are
// silently dropped — callers that need audit visibility should use
// applyToPageWithConflicts.
func (o *ScenarioOverlay) applyToPage(page *ObjectPage) *ObjectPage {
	out, _ := o.applyToPageWithConflicts(page)
	return out
}

// applyToPageWithConflicts is the US-481 superset of applyToPage. It returns
// the same overlaid page plus the union of all per-row conflicts in
// row-then-edit-seq order.
func (o *ScenarioOverlay) applyToPageWithConflicts(page *ObjectPage) (*ObjectPage, []scenarios.ScenarioConflict) {
	if o == nil || page == nil || len(page.Data) == 0 {
		return page, nil
	}
	out := make([]*WireObject, 0, len(page.Data))
	var allConflicts []scenarios.ScenarioConflict
	for _, obj := range page.Data {
		overlaid, deleted, conflicts := o.applyToObjectWithConflicts(obj)
		allConflicts = append(allConflicts, conflicts...)
		if deleted {
			continue
		}
		out = append(out, overlaid)
	}
	cp := *page
	cp.Data = out
	return &cp, allConflicts
}

func wireObjectToView(obj *WireObject) *scenarios.ObjectView {
	props := make(map[string]json.RawMessage, len(obj.Properties))
	for k, v := range obj.Properties {
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		props[k] = b
	}
	return &scenarios.ObjectView{
		ObjectType: obj.APIName,
		ObjectID:   fmt.Sprintf("%v", obj.PrimaryKey),
		Properties: props,
	}
}

func viewToWireObject(view *scenarios.ObjectView, template *WireObject) *WireObject {
	out := make(map[string]interface{}, len(view.Properties))
	for k, v := range view.Properties {
		var decoded any
		if err := json.Unmarshal(v, &decoded); err != nil {
			continue
		}
		out[k] = decoded
	}
	return &WireObject{
		RID:        template.RID,
		PrimaryKey: template.PrimaryKey,
		APIName:    template.APIName,
		Properties: out,
		Highlights: template.Highlights,
	}
}
