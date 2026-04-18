package oms

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ComputedAggregation declares how a ComputedProperty collapses the values
// found by traversing source_link_rid into a single scalar. Type is one of
// count / sum / avg / min / max. Field is the target property on the linked
// object type; ignored for count and required for the numeric metrics.
type ComputedAggregation struct {
	Type  string `json:"type"`
	Field string `json:"field,omitempty"`
}

// Validate rejects aggregation specs the resolver cannot evaluate. Callers
// (admin handlers, repository writes) are expected to run this before
// persisting a ComputedProperty so we fail fast at definition time rather
// than deep inside the query path.
func (a ComputedAggregation) Validate() error {
	switch strings.ToLower(a.Type) {
	case "count":
		return nil
	case "sum", "avg", "min", "max":
		if a.Field == "" {
			return fmt.Errorf("aggregation %q requires field", a.Type)
		}
		return nil
	default:
		return fmt.Errorf("unknown aggregation type %q", a.Type)
	}
}

// DefaultComputedPropertyCacheTTL is the TTL applied when a ComputedProperty
// row leaves cache_ttl_seconds at zero. Picked per US-202 spec.
const DefaultComputedPropertyCacheTTL = 60 * time.Second

// ComputedProperty is the OMS-side declaration of an aggregation-based
// computed property. Its value is produced at query time by walking
// SourceLinkRID from each base object and running Aggregation across the
// linked set. Results are cached in-process for CacheTTLSeconds so repeat
// reads do not re-walk the link or re-scan Bleve on every request.
type ComputedProperty struct {
	RID             string              `json:"rid"`
	ObjectTypeRID   string              `json:"objectTypeRid"`
	APIName         string              `json:"apiName"`
	DisplayName     string              `json:"displayName,omitempty"`
	Description     string              `json:"description,omitempty"`
	SourceLinkRID   string              `json:"sourceLinkRid"`
	Aggregation     ComputedAggregation `json:"aggregation"`
	CacheTTLSeconds int                 `json:"cacheTtlSeconds"`
	CreatedAt       time.Time           `json:"-"`
}

// CacheTTL returns CacheTTLSeconds as a time.Duration, substituting
// DefaultComputedPropertyCacheTTL when the row was created with a zero
// value. Negative values are clamped to zero (no caching) so a misconfigured
// row is never read as "cache forever".
func (c ComputedProperty) CacheTTL() time.Duration {
	if c.CacheTTLSeconds == 0 {
		return DefaultComputedPropertyCacheTTL
	}
	if c.CacheTTLSeconds < 0 {
		return 0
	}
	return time.Duration(c.CacheTTLSeconds) * time.Second
}

// Validate checks the required shape of a ComputedProperty row. It is used
// by repository writes and by admin handlers before they mutate the OMS
// catalog.
func (c ComputedProperty) Validate() error {
	if c.RID == "" {
		return fmt.Errorf("computed property requires rid")
	}
	if c.ObjectTypeRID == "" {
		return fmt.Errorf("computed property requires objectTypeRid")
	}
	if c.APIName == "" {
		return fmt.Errorf("computed property requires apiName")
	}
	if c.SourceLinkRID == "" {
		return fmt.Errorf("computed property requires sourceLinkRid")
	}
	return c.Aggregation.Validate()
}

// ComputedPropertyStore is the narrow read/write surface the resolver and
// admin handlers depend on. It lives outside Repository so the many mock
// implementations scattered through the test tree do not have to grow stub
// methods for every row type introduced in later phases.
type ComputedPropertyStore interface {
	CreateComputedProperty(ctx context.Context, cp *ComputedProperty) error
	GetComputedProperty(ctx context.Context, rid string) (*ComputedProperty, error)
	ListComputedProperties(ctx context.Context, objectTypeRID string) ([]ComputedProperty, error)
	UpdateComputedProperty(ctx context.Context, cp *ComputedProperty) error
	DeleteComputedProperty(ctx context.Context, rid string) error
}
