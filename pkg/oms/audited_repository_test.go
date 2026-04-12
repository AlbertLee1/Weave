package oms_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// inMemoryRepo is a minimal in-memory OMS repository for audit tests.
// It stores core entities (Ontology, ObjectType, Property, LinkType,
// ActionType, Interface) and supports Get/Create/Update/Delete.
type inMemoryRepo struct {
	oms.Repository // embed noopRepo for unimplemented methods
	mu             sync.Mutex

	ontologies       map[string]*oms.Ontology
	objectTypes      map[string]*oms.ObjectType
	properties       map[string]*oms.Property
	linkTypes        map[string]*oms.LinkType
	actionTypes      map[string]*oms.ActionType
	interfaces       map[string]*oms.Interface
	securityPolicies map[string]*oms.SecurityPolicy
}

func newInMemoryRepo() *inMemoryRepo {
	return &inMemoryRepo{
		Repository:       &noopRepo{},
		ontologies:       map[string]*oms.Ontology{},
		objectTypes:      map[string]*oms.ObjectType{},
		properties:       map[string]*oms.Property{},
		linkTypes:        map[string]*oms.LinkType{},
		actionTypes:      map[string]*oms.ActionType{},
		interfaces:       map[string]*oms.Interface{},
		securityPolicies: map[string]*oms.SecurityPolicy{},
	}
}

// --- Ontology ---

func (r *inMemoryRepo) CreateOntology(_ context.Context, o *oms.Ontology) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ontologies[o.RID]; ok {
		return oms.ErrDuplicate
	}
	cp := *o
	r.ontologies[o.RID] = &cp
	return nil
}

func (r *inMemoryRepo) GetOntology(_ context.Context, rid string) (*oms.Ontology, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o, ok := r.ontologies[rid]; ok {
		cp := *o
		return &cp, nil
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryRepo) UpdateOntology(_ context.Context, o *oms.Ontology) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ontologies[o.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *o
	r.ontologies[o.RID] = &cp
	return nil
}

// --- ObjectType ---

func (r *inMemoryRepo) CreateObjectType(_ context.Context, ot *oms.ObjectType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objectTypes[ot.RID]; ok {
		return oms.ErrDuplicate
	}
	cp := *ot
	r.objectTypes[ot.RID] = &cp
	return nil
}

func (r *inMemoryRepo) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ot, ok := r.objectTypes[rid]; ok {
		cp := *ot
		return &cp, nil
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryRepo) UpdateObjectType(_ context.Context, ot *oms.ObjectType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objectTypes[ot.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *ot
	r.objectTypes[ot.RID] = &cp
	return nil
}

func (r *inMemoryRepo) DeleteObjectType(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.objectTypes[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.objectTypes, rid)
	return nil
}

// --- Property ---

func (r *inMemoryRepo) CreateProperty(_ context.Context, p *oms.Property) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.properties[p.RID]; ok {
		return oms.ErrDuplicate
	}
	cp := *p
	r.properties[p.RID] = &cp
	return nil
}

func (r *inMemoryRepo) GetProperty(_ context.Context, rid string) (*oms.Property, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.properties[rid]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryRepo) UpdateProperty(_ context.Context, p *oms.Property) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.properties[p.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *p
	r.properties[p.RID] = &cp
	return nil
}

func (r *inMemoryRepo) DeleteProperty(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.properties[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.properties, rid)
	return nil
}

// --- LinkType ---

func (r *inMemoryRepo) CreateLinkType(_ context.Context, lt *oms.LinkType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.linkTypes[lt.RID]; ok {
		return oms.ErrDuplicate
	}
	cp := *lt
	r.linkTypes[lt.RID] = &cp
	return nil
}

func (r *inMemoryRepo) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lt, ok := r.linkTypes[rid]; ok {
		cp := *lt
		return &cp, nil
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryRepo) UpdateLinkType(_ context.Context, lt *oms.LinkType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.linkTypes[lt.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *lt
	r.linkTypes[lt.RID] = &cp
	return nil
}

func (r *inMemoryRepo) DeleteLinkType(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.linkTypes[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.linkTypes, rid)
	return nil
}

// --- ActionType ---

func (r *inMemoryRepo) CreateActionType(_ context.Context, at *oms.ActionType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.actionTypes[at.RID]; ok {
		return oms.ErrDuplicate
	}
	cp := *at
	r.actionTypes[at.RID] = &cp
	return nil
}

func (r *inMemoryRepo) GetActionType(_ context.Context, rid string) (*oms.ActionType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if at, ok := r.actionTypes[rid]; ok {
		cp := *at
		return &cp, nil
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryRepo) UpdateActionType(_ context.Context, at *oms.ActionType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.actionTypes[at.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *at
	r.actionTypes[at.RID] = &cp
	return nil
}

func (r *inMemoryRepo) DeleteActionType(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.actionTypes[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.actionTypes, rid)
	return nil
}

// --- Interface ---

func (r *inMemoryRepo) CreateInterface(_ context.Context, iface *oms.Interface) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.interfaces[iface.RID]; ok {
		return oms.ErrDuplicate
	}
	cp := *iface
	r.interfaces[iface.RID] = &cp
	return nil
}

func (r *inMemoryRepo) GetInterface(_ context.Context, rid string) (*oms.Interface, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if iface, ok := r.interfaces[rid]; ok {
		cp := *iface
		return &cp, nil
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryRepo) UpdateInterface(_ context.Context, iface *oms.Interface) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.interfaces[iface.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *iface
	r.interfaces[iface.RID] = &cp
	return nil
}

func (r *inMemoryRepo) DeleteInterface(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.interfaces[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.interfaces, rid)
	return nil
}

// --- SecurityPolicy ---

func (r *inMemoryRepo) CreateSecurityPolicy(_ context.Context, sp *oms.SecurityPolicy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.securityPolicies[sp.RID]; ok {
		return oms.ErrDuplicate
	}
	cp := *sp
	r.securityPolicies[sp.RID] = &cp
	return nil
}

func (r *inMemoryRepo) GetSecurityPolicy(_ context.Context, rid string) (*oms.SecurityPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sp, ok := r.securityPolicies[rid]; ok {
		cp := *sp
		return &cp, nil
	}
	return nil, oms.ErrNotFound
}

func (r *inMemoryRepo) UpdateSecurityPolicy(_ context.Context, sp *oms.SecurityPolicy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.securityPolicies[sp.RID]; !ok {
		return oms.ErrNotFound
	}
	cp := *sp
	r.securityPolicies[sp.RID] = &cp
	return nil
}

func (r *inMemoryRepo) DeleteSecurityPolicy(_ context.Context, rid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.securityPolicies[rid]; !ok {
		return oms.ErrNotFound
	}
	delete(r.securityPolicies, rid)
	return nil
}

// --- Helpers ---

type auditDiff struct {
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

func parseDiff(t *testing.T, raw json.RawMessage) auditDiff {
	t.Helper()
	var d auditDiff
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}
	return d
}

func TestOMSAuditTrail(t *testing.T) {
	store := audit.NewMemoryStore()
	inner := newInMemoryRepo()
	actorFn := func(ctx context.Context) string {
		if u := auth.UserFromContext(ctx); u != nil {
			return u.ID
		}
		return ""
	}
	repo := oms.NewAuditedRepository(inner, store, actorFn)

	userCtx := auth.WithUser(context.Background(), &auth.User{
		ID:   "user-1",
		Name: "Alice",
	})

	t.Run("CreateObjectType", func(t *testing.T) {
		ot := &oms.ObjectType{
			RID:         "ri.ontology.main.objectType.employee",
			APIName:     "Employee",
			DisplayName: "Employee",
			PrimaryKey:  "employeeId",
			Status:      "ACTIVE",
			Visibility:  "NORMAL",
		}
		if err := repo.CreateObjectType(userCtx, ot); err != nil {
			t.Fatalf("create: %v", err)
		}

		events := store.Events()
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		ev := events[0]
		if ev.ActorID != "user-1" {
			t.Errorf("actorID = %q, want %q", ev.ActorID, "user-1")
		}
		if ev.Action != "CREATE" {
			t.Errorf("action = %q, want %q", ev.Action, "CREATE")
		}
		if ev.ResourceType != "ObjectType" {
			t.Errorf("resourceType = %q, want %q", ev.ResourceType, "ObjectType")
		}
		if ev.ResourceRID != ot.RID {
			t.Errorf("resourceRID = %q, want %q", ev.ResourceRID, ot.RID)
		}
		diff := parseDiff(t, ev.DiffJSON)
		if string(diff.Before) != "null" {
			t.Errorf("before = %s, want null", diff.Before)
		}
		if len(diff.After) == 0 || string(diff.After) == "null" {
			t.Error("after should contain JSON, got null/empty")
		}
	})

	t.Run("UpdateObjectType", func(t *testing.T) {
		updated := &oms.ObjectType{
			RID:         "ri.ontology.main.objectType.employee",
			APIName:     "Employee",
			DisplayName: "Updated Employee",
			PrimaryKey:  "employeeId",
			Status:      "ACTIVE",
			Visibility:  "NORMAL",
		}
		if err := repo.UpdateObjectType(userCtx, updated); err != nil {
			t.Fatalf("update: %v", err)
		}

		events := store.Events()
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
		ev := events[1]
		if ev.Action != "UPDATE" {
			t.Errorf("action = %q, want %q", ev.Action, "UPDATE")
		}
		if ev.ResourceType != "ObjectType" {
			t.Errorf("resourceType = %q, want %q", ev.ResourceType, "ObjectType")
		}
		diff := parseDiff(t, ev.DiffJSON)
		if string(diff.Before) == "null" {
			t.Error("before should contain old state")
		}
		if string(diff.After) == "null" {
			t.Error("after should contain new state")
		}
		// Verify the before has the old DisplayName and after has the new one.
		var beforeMap, afterMap map[string]interface{}
		json.Unmarshal(diff.Before, &beforeMap)
		json.Unmarshal(diff.After, &afterMap)
		if beforeMap["displayName"] != "Employee" {
			t.Errorf("before displayName = %v, want Employee", beforeMap["displayName"])
		}
		if afterMap["displayName"] != "Updated Employee" {
			t.Errorf("after displayName = %v, want Updated Employee", afterMap["displayName"])
		}
	})

	t.Run("DeleteObjectType", func(t *testing.T) {
		if err := repo.DeleteObjectType(userCtx, "ri.ontology.main.objectType.employee"); err != nil {
			t.Fatalf("delete: %v", err)
		}

		events := store.Events()
		if len(events) != 3 {
			t.Fatalf("expected 3 events, got %d", len(events))
		}
		ev := events[2]
		if ev.Action != "DELETE" {
			t.Errorf("action = %q, want %q", ev.Action, "DELETE")
		}
		if ev.ResourceType != "ObjectType" {
			t.Errorf("resourceType = %q, want %q", ev.ResourceType, "ObjectType")
		}
		diff := parseDiff(t, ev.DiffJSON)
		if string(diff.Before) == "null" {
			t.Error("before should contain old state")
		}
		if string(diff.After) != "null" {
			t.Errorf("after = %s, want null", diff.After)
		}
	})

	t.Run("CreateOntology", func(t *testing.T) {
		o := &oms.Ontology{
			RID:         "ri.ontology.main.ontology.test",
			APIName:     "test",
			DisplayName: "Test Ontology",
		}
		if err := repo.CreateOntology(userCtx, o); err != nil {
			t.Fatalf("create ontology: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "CREATE" || last.ResourceType != "Ontology" {
			t.Errorf("got action=%q type=%q, want CREATE/Ontology", last.Action, last.ResourceType)
		}
	})

	t.Run("UpdateOntology", func(t *testing.T) {
		o := &oms.Ontology{
			RID:         "ri.ontology.main.ontology.test",
			APIName:     "test",
			DisplayName: "Updated Test Ontology",
		}
		if err := repo.UpdateOntology(userCtx, o); err != nil {
			t.Fatalf("update ontology: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "UPDATE" || last.ResourceType != "Ontology" {
			t.Errorf("got action=%q type=%q, want UPDATE/Ontology", last.Action, last.ResourceType)
		}
	})

	t.Run("CreateProperty", func(t *testing.T) {
		p := &oms.Property{
			RID:           "ri.ontology.main.property.name",
			ObjectTypeRID: "ri.ontology.main.objectType.employee",
			APIName:       "name",
			BaseType:      "string",
		}
		if err := repo.CreateProperty(userCtx, p); err != nil {
			t.Fatalf("create property: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "CREATE" || last.ResourceType != "Property" {
			t.Errorf("got action=%q type=%q, want CREATE/Property", last.Action, last.ResourceType)
		}
	})

	t.Run("DeleteProperty", func(t *testing.T) {
		if err := repo.DeleteProperty(userCtx, "ri.ontology.main.property.name"); err != nil {
			t.Fatalf("delete property: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "DELETE" || last.ResourceType != "Property" {
			t.Errorf("got action=%q type=%q, want DELETE/Property", last.Action, last.ResourceType)
		}
	})

	t.Run("CreateLinkType", func(t *testing.T) {
		lt := &oms.LinkType{
			RID:              "ri.ontology.main.linkType.manages",
			APIName:          "manages",
			DisplayName:      "Manages",
			SourceObjectType: "Employee",
			TargetObjectType: "Employee",
			Cardinality:      "ONE_TO_MANY",
		}
		if err := repo.CreateLinkType(userCtx, lt); err != nil {
			t.Fatalf("create linkType: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "CREATE" || last.ResourceType != "LinkType" {
			t.Errorf("got action=%q type=%q, want CREATE/LinkType", last.Action, last.ResourceType)
		}
	})

	t.Run("DeleteLinkType", func(t *testing.T) {
		if err := repo.DeleteLinkType(userCtx, "ri.ontology.main.linkType.manages"); err != nil {
			t.Fatalf("delete linkType: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "DELETE" || last.ResourceType != "LinkType" {
			t.Errorf("got action=%q type=%q, want DELETE/LinkType", last.Action, last.ResourceType)
		}
	})

	t.Run("CreateActionType", func(t *testing.T) {
		at := &oms.ActionType{
			RID:         "ri.ontology.main.actionType.promote",
			APIName:     "promote",
			DisplayName: "Promote",
			Status:      "ACTIVE",
		}
		if err := repo.CreateActionType(userCtx, at); err != nil {
			t.Fatalf("create actionType: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "CREATE" || last.ResourceType != "ActionType" {
			t.Errorf("got action=%q type=%q, want CREATE/ActionType", last.Action, last.ResourceType)
		}
	})

	t.Run("DeleteActionType", func(t *testing.T) {
		if err := repo.DeleteActionType(userCtx, "ri.ontology.main.actionType.promote"); err != nil {
			t.Fatalf("delete actionType: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "DELETE" || last.ResourceType != "ActionType" {
			t.Errorf("got action=%q type=%q, want DELETE/ActionType", last.Action, last.ResourceType)
		}
	})

	t.Run("CreateInterface", func(t *testing.T) {
		iface := &oms.Interface{
			RID:         "ri.ontology.main.interface.person",
			APIName:     "Person",
			DisplayName: "Person",
		}
		if err := repo.CreateInterface(userCtx, iface); err != nil {
			t.Fatalf("create interface: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "CREATE" || last.ResourceType != "Interface" {
			t.Errorf("got action=%q type=%q, want CREATE/Interface", last.Action, last.ResourceType)
		}
	})

	t.Run("DeleteInterface", func(t *testing.T) {
		if err := repo.DeleteInterface(userCtx, "ri.ontology.main.interface.person"); err != nil {
			t.Fatalf("delete interface: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "DELETE" || last.ResourceType != "Interface" {
			t.Errorf("got action=%q type=%q, want DELETE/Interface", last.Action, last.ResourceType)
		}
	})

	t.Run("NoUserContext", func(t *testing.T) {
		// When no user is in context, actorID should be empty/unknown.
		o := &oms.Ontology{
			RID:         "ri.ontology.main.ontology.anon",
			APIName:     "anon",
			DisplayName: "Anonymous",
		}
		if err := repo.CreateOntology(context.Background(), o); err != nil {
			t.Fatalf("create: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.ActorID != "" {
			t.Errorf("actorID = %q, want empty for anonymous", last.ActorID)
		}
	})

	t.Run("ErrorDoesNotAudit", func(t *testing.T) {
		countBefore := len(store.Events())
		// Duplicate create should fail; no audit event should be recorded.
		_ = repo.CreateOntology(userCtx, &oms.Ontology{
			RID:         "ri.ontology.main.ontology.anon",
			APIName:     "anon",
			DisplayName: "Anonymous",
		})
		if len(store.Events()) != countBefore {
			t.Error("audit event should not be recorded on error")
		}
	})

	t.Run("CreateSecurityPolicy", func(t *testing.T) {
		sp := &oms.SecurityPolicy{
			RID:           "ri.ontology.main.policy.row1",
			ObjectTypeRID: "ri.ontology.main.objectType.employee",
			PolicyType:    "OBJECT",
			Rules:         json.RawMessage(`[{"type":"eq","userAttr":"dept","objectProperty":"department"}]`),
		}
		if err := repo.CreateSecurityPolicy(userCtx, sp); err != nil {
			t.Fatalf("create: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "CREATE" || last.ResourceType != "SecurityPolicy" {
			t.Errorf("got action=%q type=%q, want CREATE/SecurityPolicy", last.Action, last.ResourceType)
		}
		if last.ResourceRID != sp.RID {
			t.Errorf("resourceRID = %q, want %q", last.ResourceRID, sp.RID)
		}
		if last.ActorID != "user-1" {
			t.Errorf("actorID = %q, want user-1", last.ActorID)
		}
	})

	t.Run("UpdateSecurityPolicy", func(t *testing.T) {
		sp := &oms.SecurityPolicy{
			RID:           "ri.ontology.main.policy.row1",
			ObjectTypeRID: "ri.ontology.main.objectType.employee",
			PolicyType:    "OBJECT",
			Rules:         json.RawMessage(`[{"type":"in","userAttr":"region","objectProperty":"region"}]`),
		}
		if err := repo.UpdateSecurityPolicy(userCtx, sp); err != nil {
			t.Fatalf("update: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "UPDATE" || last.ResourceType != "SecurityPolicy" {
			t.Errorf("got action=%q type=%q, want UPDATE/SecurityPolicy", last.Action, last.ResourceType)
		}
		diff := parseDiff(t, last.DiffJSON)
		if string(diff.Before) == "null" {
			t.Error("before should contain old state")
		}
		if string(diff.After) == "null" {
			t.Error("after should contain new state")
		}
	})

	t.Run("DeleteSecurityPolicy", func(t *testing.T) {
		if err := repo.DeleteSecurityPolicy(userCtx, "ri.ontology.main.policy.row1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		events := store.Events()
		last := events[len(events)-1]
		if last.Action != "DELETE" || last.ResourceType != "SecurityPolicy" {
			t.Errorf("got action=%q type=%q, want DELETE/SecurityPolicy", last.Action, last.ResourceType)
		}
		diff := parseDiff(t, last.DiffJSON)
		if string(diff.Before) == "null" {
			t.Error("before should contain old state")
		}
		if string(diff.After) != "null" {
			t.Errorf("after = %s, want null", diff.After)
		}
	})
}
