package oms

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// US-214 Interface Method Signatures.
//
// An InterfaceMethod declares a callable signature on an Interface: a name,
// an ordered params list, and a return type. ActionTypes may claim to
// implement a specific method via ActionType.ImplementsMethodRID — that
// pointer is what powers polymorphic dispatch (the invoke endpoint looks up
// candidate ActionTypes keyed by ImplementsMethodRID, then filters to the
// one whose rule targets the caller-supplied ObjectType apiName).

// InterfaceMethodParam is a single parameter declared on an interface
// method. Kept as a plain struct (not json.RawMessage) so the validator,
// handler, and eventual SDK generator can all look at Name / Type directly.
type InterfaceMethodParam struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Required bool            `json:"required,omitempty"`
	Default  json.RawMessage `json:"default,omitempty"`
}

// InterfaceMethodReturns describes the return shape of an interface method.
// Minimal for now — just a base type name. Downstream consumers that need
// full DataType-style parametrization can read the raw `returns` JSONB.
type InterfaceMethodReturns struct {
	Type string `json:"type"`
}

// InterfaceMethod is the OMS-side declaration of a named method on an
// Interface. Rows live in the `interface_methods` table; a single row owns
// one method signature for one Interface (UNIQUE (interface_rid, name)).
type InterfaceMethod struct {
	RID          string                 `json:"rid"`
	InterfaceRID string                 `json:"interfaceRid"`
	Name         string                 `json:"name"`
	Params       []InterfaceMethodParam `json:"params"`
	Returns      InterfaceMethodReturns `json:"returns"`
	Description  string                 `json:"description,omitempty"`
	CreatedAt    time.Time              `json:"-"`
}

// Validate enforces the minimum shape for a persistable InterfaceMethod.
// Run by the repo writes and by admin handlers before mutation so schema
// errors surface at definition time rather than at invocation time.
//
// Only the envelope is enforced (rid / interfaceRid / name non-empty, each
// param has a non-empty name + type). Return-type validation is intentionally
// deferred — the `returns` JSONB may carry richer shapes (arrays, struct
// literals) that grow over time, and enforcing a closed enum here would
// just churn with every new BaseType.
func (im *InterfaceMethod) Validate() error {
	if im.RID == "" {
		return fmt.Errorf("interface method requires rid")
	}
	if im.InterfaceRID == "" {
		return fmt.Errorf("interface method requires interfaceRid")
	}
	if im.Name == "" {
		return fmt.Errorf("interface method requires name")
	}
	seen := make(map[string]struct{}, len(im.Params))
	for i, p := range im.Params {
		if p.Name == "" {
			return fmt.Errorf("interface method param[%d] requires name", i)
		}
		if p.Type == "" {
			return fmt.Errorf("interface method param[%d] %q requires type", i, p.Name)
		}
		if _, dup := seen[p.Name]; dup {
			return fmt.Errorf("interface method param %q declared twice", p.Name)
		}
		seen[p.Name] = struct{}{}
	}
	return nil
}

// InterfaceMethodStore is the narrow 5-method CRUD surface for
// interface_methods. Kept outside oms.Repository so the many mock repos
// scattered through the test tree do not have to grow stubs — the same
// pattern established by LinkPropertyStore / ComputedPropertyStore /
// MediaAssetStore.
type InterfaceMethodStore interface {
	CreateInterfaceMethod(ctx context.Context, im *InterfaceMethod) error
	GetInterfaceMethod(ctx context.Context, rid string) (*InterfaceMethod, error)
	ListInterfaceMethods(ctx context.Context, interfaceRID string) ([]InterfaceMethod, error)
	UpdateInterfaceMethod(ctx context.Context, im *InterfaceMethod) error
	DeleteInterfaceMethod(ctx context.Context, rid string) error
}
