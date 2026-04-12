package actions

import (
	"context"
	"fmt"
	"strings"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// RoutingDispatcher implements FunctionDispatcher by routing based on the
// ActionType.FunctionRID prefix:
//   - http:// or https:// → HTTPDispatcher (external function service)
//   - anything else (ri.* RIDs) → GojaDispatcher (embedded Goja runtime)
//
// Either sub-dispatcher may be nil; dispatch to a nil sub-dispatcher returns
// an error describing the missing configuration.
type RoutingDispatcher struct {
	goja *GojaDispatcher
	http *HTTPDispatcher
}

// NewRoutingDispatcher creates a RoutingDispatcher. Both sub-dispatchers are
// optional — pass nil for a mode you don't support and the router will return
// a clear error if that path is hit at runtime.
func NewRoutingDispatcher(goja *GojaDispatcher, http *HTTPDispatcher) *RoutingDispatcher {
	return &RoutingDispatcher{goja: goja, http: http}
}

// Dispatch routes the action to the appropriate sub-dispatcher based on the
// FunctionRID prefix.
func (d *RoutingDispatcher) Dispatch(ctx context.Context, at *oms.ActionType, params map[string]interface{}) ([]funnel.Edit, error) {
	rid := at.FunctionRID

	if strings.HasPrefix(rid, "http://") || strings.HasPrefix(rid, "https://") {
		if d.http != nil {
			return d.http.Dispatch(ctx, at, params)
		}
		return nil, fmt.Errorf("routing dispatcher: HTTP dispatcher not configured for URL %q", rid)
	}

	// Default: ri.* RIDs route to Goja.
	if d.goja != nil {
		return d.goja.Dispatch(ctx, at, params)
	}
	return nil, fmt.Errorf("routing dispatcher: Goja dispatcher not configured for RID %q", rid)
}
