package oms

import (
	"context"
	"encoding/json"

	"github.com/liyang/weave/pkg/audit"
)

// ActorFunc extracts an actor identifier from a request context. The default
// returns "" (anonymous). Callers that have access to pkg/auth should pass
// a function like:
//
//	func(ctx context.Context) string {
//	    if u := auth.UserFromContext(ctx); u != nil { return u.ID }
//	    return ""
//	}
type ActorFunc func(ctx context.Context) string

// AuditedRepository wraps a Repository and records audit events for every
// create, update, and delete operation on core OMS entities. The diff is
// encoded as {"before": ..., "after": ...} JSON.
type AuditedRepository struct {
	Repository
	store   audit.Store
	actorFn ActorFunc
}

// NewAuditedRepository returns a Repository decorator that records audit
// events to the given store for all write operations. actorFn may be nil,
// in which case the actor is always recorded as "".
func NewAuditedRepository(inner Repository, store audit.Store, actorFn ActorFunc) *AuditedRepository {
	if actorFn == nil {
		actorFn = func(context.Context) string { return "" }
	}
	return &AuditedRepository{Repository: inner, store: store, actorFn: actorFn}
}

// auditDiff is the diff payload stored in DiffJSON.
type auditDiff struct {
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

func makeDiff(before, after any) json.RawMessage {
	d := auditDiff{
		Before: jsonOrNull(before),
		After:  jsonOrNull(after),
	}
	b, _ := json.Marshal(d)
	return b
}

func jsonOrNull(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

func (a *AuditedRepository) record(ctx context.Context, action, resourceType, resourceRID string, diff json.RawMessage) {
	_ = audit.Record(ctx, a.store, audit.AuditEvent{
		ActorID:      a.actorFn(ctx),
		Action:       action,
		ResourceType: resourceType,
		ResourceRID:  resourceRID,
		DiffJSON:     diff,
	})
}

// --- Ontology ---

func (a *AuditedRepository) CreateOntology(ctx context.Context, o *Ontology) error {
	if err := a.Repository.CreateOntology(ctx, o); err != nil {
		return err
	}
	a.record(ctx, "CREATE", "Ontology", o.RID, makeDiff(nil, o))
	return nil
}

func (a *AuditedRepository) UpdateOntology(ctx context.Context, o *Ontology) error {
	before, _ := a.Repository.GetOntology(ctx, o.RID)
	if err := a.Repository.UpdateOntology(ctx, o); err != nil {
		return err
	}
	a.record(ctx, "UPDATE", "Ontology", o.RID, makeDiff(before, o))
	return nil
}

// --- ObjectType ---

func (a *AuditedRepository) CreateObjectType(ctx context.Context, ot *ObjectType) error {
	if err := a.Repository.CreateObjectType(ctx, ot); err != nil {
		return err
	}
	a.record(ctx, "CREATE", "ObjectType", ot.RID, makeDiff(nil, ot))
	return nil
}

func (a *AuditedRepository) UpdateObjectType(ctx context.Context, ot *ObjectType) error {
	before, _ := a.Repository.GetObjectType(ctx, ot.RID)
	if err := a.Repository.UpdateObjectType(ctx, ot); err != nil {
		return err
	}
	a.record(ctx, "UPDATE", "ObjectType", ot.RID, makeDiff(before, ot))
	return nil
}

func (a *AuditedRepository) DeleteObjectType(ctx context.Context, rid string) error {
	before, _ := a.Repository.GetObjectType(ctx, rid)
	if err := a.Repository.DeleteObjectType(ctx, rid); err != nil {
		return err
	}
	a.record(ctx, "DELETE", "ObjectType", rid, makeDiff(before, nil))
	return nil
}

// --- Property ---

func (a *AuditedRepository) CreateProperty(ctx context.Context, p *Property) error {
	if err := a.Repository.CreateProperty(ctx, p); err != nil {
		return err
	}
	a.record(ctx, "CREATE", "Property", p.RID, makeDiff(nil, p))
	return nil
}

func (a *AuditedRepository) UpdateProperty(ctx context.Context, p *Property) error {
	before, _ := a.Repository.GetProperty(ctx, p.RID)
	if err := a.Repository.UpdateProperty(ctx, p); err != nil {
		return err
	}
	a.record(ctx, "UPDATE", "Property", p.RID, makeDiff(before, p))
	return nil
}

func (a *AuditedRepository) DeleteProperty(ctx context.Context, rid string) error {
	before, _ := a.Repository.GetProperty(ctx, rid)
	if err := a.Repository.DeleteProperty(ctx, rid); err != nil {
		return err
	}
	a.record(ctx, "DELETE", "Property", rid, makeDiff(before, nil))
	return nil
}

// --- LinkType ---

func (a *AuditedRepository) CreateLinkType(ctx context.Context, lt *LinkType) error {
	if err := a.Repository.CreateLinkType(ctx, lt); err != nil {
		return err
	}
	a.record(ctx, "CREATE", "LinkType", lt.RID, makeDiff(nil, lt))
	return nil
}

func (a *AuditedRepository) UpdateLinkType(ctx context.Context, lt *LinkType) error {
	before, _ := a.Repository.GetLinkType(ctx, lt.RID)
	if err := a.Repository.UpdateLinkType(ctx, lt); err != nil {
		return err
	}
	a.record(ctx, "UPDATE", "LinkType", lt.RID, makeDiff(before, lt))
	return nil
}

func (a *AuditedRepository) DeleteLinkType(ctx context.Context, rid string) error {
	before, _ := a.Repository.GetLinkType(ctx, rid)
	if err := a.Repository.DeleteLinkType(ctx, rid); err != nil {
		return err
	}
	a.record(ctx, "DELETE", "LinkType", rid, makeDiff(before, nil))
	return nil
}

// --- ActionType ---

func (a *AuditedRepository) CreateActionType(ctx context.Context, at *ActionType) error {
	if err := a.Repository.CreateActionType(ctx, at); err != nil {
		return err
	}
	a.record(ctx, "CREATE", "ActionType", at.RID, makeDiff(nil, at))
	return nil
}

func (a *AuditedRepository) UpdateActionType(ctx context.Context, at *ActionType) error {
	before, _ := a.Repository.GetActionType(ctx, at.RID)
	if err := a.Repository.UpdateActionType(ctx, at); err != nil {
		return err
	}
	a.record(ctx, "UPDATE", "ActionType", at.RID, makeDiff(before, at))
	return nil
}

func (a *AuditedRepository) DeleteActionType(ctx context.Context, rid string) error {
	before, _ := a.Repository.GetActionType(ctx, rid)
	if err := a.Repository.DeleteActionType(ctx, rid); err != nil {
		return err
	}
	a.record(ctx, "DELETE", "ActionType", rid, makeDiff(before, nil))
	return nil
}

// --- Interface ---

func (a *AuditedRepository) CreateInterface(ctx context.Context, iface *Interface) error {
	if err := a.Repository.CreateInterface(ctx, iface); err != nil {
		return err
	}
	a.record(ctx, "CREATE", "Interface", iface.RID, makeDiff(nil, iface))
	return nil
}

func (a *AuditedRepository) UpdateInterface(ctx context.Context, iface *Interface) error {
	before, _ := a.Repository.GetInterface(ctx, iface.RID)
	if err := a.Repository.UpdateInterface(ctx, iface); err != nil {
		return err
	}
	a.record(ctx, "UPDATE", "Interface", iface.RID, makeDiff(before, iface))
	return nil
}

func (a *AuditedRepository) DeleteInterface(ctx context.Context, rid string) error {
	before, _ := a.Repository.GetInterface(ctx, rid)
	if err := a.Repository.DeleteInterface(ctx, rid); err != nil {
		return err
	}
	a.record(ctx, "DELETE", "Interface", rid, makeDiff(before, nil))
	return nil
}

// --- SecurityPolicy ---

func (a *AuditedRepository) CreateSecurityPolicy(ctx context.Context, sp *SecurityPolicy) error {
	if err := a.Repository.CreateSecurityPolicy(ctx, sp); err != nil {
		return err
	}
	a.record(ctx, "CREATE", "SecurityPolicy", sp.RID, makeDiff(nil, sp))
	return nil
}

func (a *AuditedRepository) UpdateSecurityPolicy(ctx context.Context, sp *SecurityPolicy) error {
	before, _ := a.Repository.GetSecurityPolicy(ctx, sp.RID)
	if err := a.Repository.UpdateSecurityPolicy(ctx, sp); err != nil {
		return err
	}
	a.record(ctx, "UPDATE", "SecurityPolicy", sp.RID, makeDiff(before, sp))
	return nil
}

func (a *AuditedRepository) DeleteSecurityPolicy(ctx context.Context, rid string) error {
	before, _ := a.Repository.GetSecurityPolicy(ctx, rid)
	if err := a.Repository.DeleteSecurityPolicy(ctx, rid); err != nil {
		return err
	}
	a.record(ctx, "DELETE", "SecurityPolicy", rid, makeDiff(before, nil))
	return nil
}
