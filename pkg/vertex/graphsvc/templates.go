package graphsvc

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrTemplateNotFound is returned when a template RID does not exist.
var ErrTemplateNotFound = errors.New("graph template not found")

// GraphTemplate is a parameterized graph saved from an existing graph. The
// payload is a snapshot at save-time; ParameterizedFields enumerates JSON
// pointer paths that VTX-012 will resolve at instantiate time.
type GraphTemplate struct {
	RID                 string
	SourceGraphRID      string
	Name                string
	Payload             json.RawMessage
	ParameterizedFields []string
	Parameters          json.RawMessage
	CreatedBy           string
	CreatedAt           time.Time
}

// TemplateStore persists GraphTemplate rows. VTX-009 needs only Create/Get;
// VTX-012 will extend with Instantiate + List.
type TemplateStore interface {
	Create(ctx context.Context, t *GraphTemplate) error
	Get(ctx context.Context, rid string) (*GraphTemplate, error)
}
