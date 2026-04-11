package oss_test

import (
	"context"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// TestMultiOntologyIsolation_SameObjectTypeName is the US-044 acceptance test:
// two ontologies that share an ObjectType API name must keep their underlying
// data fully isolated. The Bleve manager keys indexes per
// {ontologyApiName}__{objectType}, so a List/Get/Search call scoped to one
// ontology must NEVER return rows from the other ontology even though both
// indexes carry an ObjectType named "Employee".
//
// The test seeds two scoped indexes with disjoint primary keys, builds an OSS
// service, and verifies the service routes each request to the correct
// per-ontology Bleve index.
func TestMultiOntologyIsolation_SameObjectTypeName(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	props := []index.Property{
		{APIName: "employeeId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}

	// Pre-create the per-ontology scoped indexes that the production funnel
	// consumer would create automatically when an EditBatch lands.
	northwindKey := index.ScopedKey("northwind", "Employee")
	chinookKey := index.ScopedKey("chinook", "Employee")
	if _, err := mgr.EnsureIndex(northwindKey, props); err != nil {
		t.Fatalf("EnsureIndex(northwind): %v", err)
	}
	if _, err := mgr.EnsureIndex(chinookKey, props); err != nil {
		t.Fatalf("EnsureIndex(chinook): %v", err)
	}

	// Seed disjoint data: northwind owns nw-1/nw-2; chinook owns ch-1/ch-2.
	northwindRows := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"nw-1", map[string]interface{}{"employeeId": "nw-1", "name": "Northwind Alice"}},
		{"nw-2", map[string]interface{}{"employeeId": "nw-2", "name": "Northwind Bob"}},
	}
	for _, r := range northwindRows {
		if err := mgr.IndexDocument(northwindKey, r.id, r.doc); err != nil {
			t.Fatalf("IndexDocument(northwind, %s): %v", r.id, err)
		}
	}

	chinookRows := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"ch-1", map[string]interface{}{"employeeId": "ch-1", "name": "Chinook Carol"}},
	}
	for _, r := range chinookRows {
		if err := mgr.IndexDocument(chinookKey, r.id, r.doc); err != nil {
			t.Fatalf("IndexDocument(chinook, %s): %v", r.id, err)
		}
	}

	// Bleve indexing is asynchronous; let it settle before any read.
	time.Sleep(200 * time.Millisecond)

	// Sanity-check the manager itself before exercising the service. Each
	// scoped index must hold only its own rows.
	if c, err := mgr.DocCount(northwindKey); err != nil || c != 2 {
		t.Fatalf("northwind index doc count: got=%d err=%v want=2", c, err)
	}
	if c, err := mgr.DocCount(chinookKey); err != nil || c != 1 {
		t.Fatalf("chinook index doc count: got=%d err=%v want=1", c, err)
	}

	// Wire two ObjectType definitions in the mock OMS repo, one per ontology,
	// using the same API name "Employee" so the only thing distinguishing the
	// rows is the ontology scope.
	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.northwind-employee",
		OntologyRID: "northwind",
		APIName:     "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.chinook-employee",
		OntologyRID: "chinook",
		APIName:     "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	svc := oss.NewService(repo, mgr, &mockLinkResolver{results: map[string][]string{}})

	ctx := context.Background()

	// Service must use the per-ontology scoped index. The northwind request
	// should see exactly the two northwind rows; the chinook request should
	// see exactly the one chinook row. Each ontology must NEVER see the other
	// ontology's rows.
	northwindPage, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: "northwind",
		ObjectType:  "Employee",
		PageSize:    50,
	})
	if err != nil {
		t.Fatalf("ListObjects(northwind): %v", err)
	}
	if got := len(northwindPage.Data); got != 2 {
		t.Fatalf("northwind ListObjects returned %d rows, want 2: %+v", got, northwindPage.Data)
	}
	for _, obj := range northwindPage.Data {
		pk, _ := obj.PrimaryKey.(string)
		if pk != "nw-1" && pk != "nw-2" {
			t.Errorf("northwind ListObjects leaked foreign primaryKey %q", pk)
		}
	}

	chinookPage, err := svc.ListObjects(ctx, oss.ListObjectsRequest{
		OntologyRID: "chinook",
		ObjectType:  "Employee",
		PageSize:    50,
	})
	if err != nil {
		t.Fatalf("ListObjects(chinook): %v", err)
	}
	if got := len(chinookPage.Data); got != 1 {
		t.Fatalf("chinook ListObjects returned %d rows, want 1: %+v", got, chinookPage.Data)
	}
	if pk, _ := chinookPage.Data[0].PrimaryKey.(string); pk != "ch-1" {
		t.Errorf("chinook ListObjects returned wrong primaryKey %q, want ch-1", pk)
	}

	// CountObjects must also be per-ontology.
	nwCount, err := svc.CountObjects(ctx, oss.CountObjectsRequest{
		OntologyRID: "northwind",
		ObjectType:  "Employee",
	})
	if err != nil {
		t.Fatalf("CountObjects(northwind): %v", err)
	}
	if nwCount.Count != 2 {
		t.Fatalf("CountObjects(northwind) = %d, want 2", nwCount.Count)
	}

	chCount, err := svc.CountObjects(ctx, oss.CountObjectsRequest{
		OntologyRID: "chinook",
		ObjectType:  "Employee",
	})
	if err != nil {
		t.Fatalf("CountObjects(chinook): %v", err)
	}
	if chCount.Count != 1 {
		t.Fatalf("CountObjects(chinook) = %d, want 1", chCount.Count)
	}

	// GetObject scoped to one ontology must NOT find a primary key from the
	// other ontology, even though they share the ObjectType apiName.
	if _, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: "northwind",
		ObjectType:  "Employee",
		PrimaryKey:  "ch-1", // chinook's row, must be invisible from northwind
	}); err == nil {
		t.Fatal("northwind GetObject(ch-1) succeeded — cross-ontology leak")
	}
	if _, err := svc.GetObject(ctx, oss.GetObjectRequest{
		OntologyRID: "chinook",
		ObjectType:  "Employee",
		PrimaryKey:  "nw-1", // northwind's row, must be invisible from chinook
	}); err == nil {
		t.Fatal("chinook GetObject(nw-1) succeeded — cross-ontology leak")
	}
}
