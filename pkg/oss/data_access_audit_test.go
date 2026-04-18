package oss_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/where"
)

// decodeAccessDetails unmarshals the DiffJSON payload produced by the
// data-access auditor so assertions can target specific keys.
func decodeAccessDetails(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}
	return out
}

// enableAudit flips the AuditDataAccess flag on the test ObjectType keyed
// by apiName. Test seeds the OT via setupOSSTest() with the flag false; the
// auditor must gate strictly on this column.
func enableAudit(repo *mockOmsRepo, ontologyRID, apiName string) {
	key := ontologyRID + ":" + apiName
	if ot, ok := repo.objectTypes[key]; ok {
		ot.AuditDataAccess = true
	}
}

func wireAuditor(svc *oss.ServiceImpl) *audit.MemoryStore {
	store := audit.NewMemoryStore()
	svc.SetDataAccessAuditor(oss.NewDataAccessAuditor(store))
	return store
}

func ctxWithUser(id string) context.Context {
	return auth.WithUser(context.Background(), &auth.User{ID: id})
}

func TestDataAccessAudit_GetObject_EnabledRecords(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	enableAudit(repo, testOntologyRID, "employee")
	store := wireAuditor(svc)

	ctx := ctxWithUser("user:alice")
	ctx = audit.WithClientInfo(ctx, audit.ClientInfo{IP: "10.0.0.1", UserAgent: "weave-test"})

	_, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	evt := events[0]
	if evt.Action != oss.DataAccessAction {
		t.Errorf("Action = %q, want %q", evt.Action, oss.DataAccessAction)
	}
	if evt.ResourceType != "ObjectType" {
		t.Errorf("ResourceType = %q, want ObjectType", evt.ResourceType)
	}
	if evt.ResourceRID != "ri.ontology.main.object-type.employee" {
		t.Errorf("ResourceRID = %q, want employee OT RID", evt.ResourceRID)
	}
	if evt.ActorID != "user:alice" {
		t.Errorf("ActorID = %q, want user:alice", evt.ActorID)
	}
	if evt.IP != "10.0.0.1" {
		t.Errorf("IP = %q, want 10.0.0.1", evt.IP)
	}
	if evt.UserAgent != "weave-test" {
		t.Errorf("UserAgent = %q, want weave-test", evt.UserAgent)
	}
	details := decodeAccessDetails(t, evt.DiffJSON)
	if details["operation"] != "getObject" {
		t.Errorf("operation = %v, want getObject", details["operation"])
	}
	if details["primaryKey"] != "emp1" {
		t.Errorf("primaryKey = %v, want emp1", details["primaryKey"])
	}
}

func TestDataAccessAudit_GetObject_DisabledByDefault(t *testing.T) {
	svc, _, _, _ := setupOSSTest(t)
	// Do NOT call enableAudit — the OT's AuditDataAccess stays false.
	store := wireAuditor(svc)

	ctx := ctxWithUser("user:alice")
	_, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}

	if n := len(store.Events()); n != 0 {
		t.Fatalf("expected 0 audit events (flag off), got %d", n)
	}
}

func TestDataAccessAudit_ListObjects_Enabled(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	enableAudit(repo, testOntologyRID, "employee")
	store := wireAuditor(svc)

	ctx := ctxWithUser("user:bob")
	page, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
	})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	details := decodeAccessDetails(t, events[0].DiffJSON)
	if details["operation"] != "listObjects" {
		t.Errorf("operation = %v, want listObjects", details["operation"])
	}
	// count reflects the filtered page data length.
	if got := int(details["count"].(float64)); got != len(page.Data) {
		t.Errorf("count = %d, want %d (page.Data len)", got, len(page.Data))
	}
}

func TestDataAccessAudit_SearchObjects_Enabled(t *testing.T) {
	svc, _, repo, _ := setupOSSTest(t)
	enableAudit(repo, testOntologyRID, "employee")
	store := wireAuditor(svc)

	ctx := ctxWithUser("user:carol")
	_, err := svc.SearchObjects(ctx, oss.SearchObjectsRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		Where: &where.WhereClause{
			Type:  "eq",
			Field: "deptId",
			Value: json.RawMessage(`"d1"`),
		},
	})
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	details := decodeAccessDetails(t, events[0].DiffJSON)
	if details["operation"] != "searchObjects" {
		t.Errorf("operation = %v, want searchObjects", details["operation"])
	}
}

func TestDataAccessAudit_NoAuditorWhenUnset(t *testing.T) {
	// Baseline guardrail: a ServiceImpl with no auditor wired must not panic
	// even when the ObjectType has AuditDataAccess = true. A nil *DataAccessAuditor
	// receiver should short-circuit cleanly.
	svc, _, repo, _ := setupOSSTest(t)
	enableAudit(repo, testOntologyRID, "employee")
	// Intentionally skip SetDataAccessAuditor.

	if _, err := svc.GetObject(ctxWithUser("u"), oss.GetObjectRequest{
		OntologyRID: testOntologyRID,
		ObjectType:  "employee",
		PrimaryKey:  "emp1",
	}); err != nil {
		t.Fatalf("GetObject without auditor: %v", err)
	}
}

func TestDataAccessAuditor_EnabledPredicate(t *testing.T) {
	cases := []struct {
		name string
		a    *oss.DataAccessAuditor
		ot   *oms.ObjectType
		want bool
	}{
		{"nil auditor", nil, &oms.ObjectType{AuditDataAccess: true}, false},
		{"nil store", oss.NewDataAccessAuditor(nil), &oms.ObjectType{AuditDataAccess: true}, false},
		{"nil ot", oss.NewDataAccessAuditor(audit.NewMemoryStore()), nil, false},
		{"flag off", oss.NewDataAccessAuditor(audit.NewMemoryStore()), &oms.ObjectType{}, false},
		{"flag on", oss.NewDataAccessAuditor(audit.NewMemoryStore()), &oms.ObjectType{AuditDataAccess: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Enabled(tc.ot); got != tc.want {
				t.Errorf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
