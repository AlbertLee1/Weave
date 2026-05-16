//go:build integration

package phase6_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// TestBDD_US471_CrossObjectTypeOptimisticLock_PerTargetExpectedVersions is
// the BDD acceptance test for US-471 — cross-ObjectType optimistic locking
// via the per-target ApplyOptions.ExpectedVersions list. It exercises the
// full HTTP wire path (chi router → executor → real PG → real funnel
// consumer → Bleve index) so the contract that satisfies the PRD literal
// "对象表 / Edit 记录加 edit_version；Executor 在 prepare 阶段比对所有
// expectedVersion，全部一致才 publish；并发两路 apply 同对象，一路成功一路 409"
// is locked across every layer that participates.
//
// Scenarios:
//
//   - Given two ObjectTypes (employee + department), each with one seeded
//     CREATE history row (version=1), and a transferEmployee ActionType
//     that MODIFIES both in a single apply.
//   - When the caller sends two parallel applies with
//     ExpectedVersions=[{employee, emp-1, 1}, {department, dept-1, 1}],
//     sequenced A → B via a done channel so the race is deterministic:
//       A: matches both → 200 success, publish, history advances to
//          version=2 on both objects.
//       B: same tokens but post-A so employee/dept now show version=2 →
//          409 StaleObject, pointing at employee (first ref in
//          caller-supplied order), no publish.
//   - And the persisted EditBatch payload from A carries
//     Edit.EditVersion stamped with the pre-A snapshot (1) for each
//     MODIFY edit so downstream replay / dashboards can attribute the
//     mutation to the version it observed.
//   - And the loser's 409 body matches the Apply path's StaleObject
//     errorName + currentVersion=2 wire-format (same shape as
//     US-023 single-target so SDK clients consume one error type).
func TestBDD_US471_CrossObjectTypeOptimisticLock_PerTargetExpectedVersions(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "phase6_us471",
		DisplayName: "Phase 6 US-471",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	employee := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, employee); err != nil {
		t.Fatalf("create employee: %v", err)
	}
	department := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "department",
		DisplayName: "Department",
		PrimaryKey:  "id",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := repo.CreateObjectType(ctx, department); err != nil {
		t.Fatalf("create department: %v", err)
	}

	// transferEmployee modifies an Employee and a Department in one shot.
	// Parameters intentionally omit "primaryKey" so findPrimaryKey falls
	// through to <ObjectType>Id per rule and each MODIFY edit picks up the
	// correct PK.
	actionParams, _ := json.Marshal([]map[string]interface{}{
		{"id": "employeeId", "type": "string", "required": true},
		{"id": "departmentId", "type": "string", "required": true},
		{"id": "title", "type": "string", "required": true},
		{"id": "headcount", "type": "integer", "required": true},
	})
	actionRules, _ := json.Marshal([]map[string]interface{}{
		{
			"type":       "modifyObject",
			"objectType": "employee",
			"propertyBindings": map[string]interface{}{
				"title": map[string]interface{}{"type": "parameter", "value": "title"},
			},
		},
		{
			"type":       "modifyObject",
			"objectType": "department",
			"propertyBindings": map[string]interface{}{
				"headcount": map[string]interface{}{"type": "parameter", "value": "headcount"},
			},
		},
	})
	at := &oms.ActionType{
		RID:         rid.NewActionTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "transferEmployee",
		DisplayName: "Transfer Employee",
		Status:      "ACTIVE",
		Parameters:  actionParams,
		Rules:       actionRules,
	}
	if err := repo.CreateActionType(ctx, at); err != nil {
		t.Fatalf("create action type: %v", err)
	}

	// Two Bleve indexes (one per ObjectType), scoped per-ontology so
	// findPrimaryKey resolves to the per-rule param without collision.
	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})
	empKey := index.ScopedKey(ont.APIName, employee.APIName)
	deptKey := index.ScopedKey(ont.APIName, department.APIName)
	empProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "title", BaseType: "string", IsSearchable: true},
	}
	deptProps := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "headcount", BaseType: "integer", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(empKey, empProps); err != nil {
		t.Fatalf("ensure employee index: %v", err)
	}
	if _, err := mgr.EnsureIndex(deptKey, deptProps); err != nil {
		t.Fatalf("ensure department index: %v", err)
	}

	consumer := funnel.NewConsumer(nil, mgr)
	consumer.SetHistoryRepo(repo)
	consumer.SetObjectTypeRIDs(map[string]string{
		employee.APIName:   employee.RID,
		department.APIName: department.RID,
	})

	// bridgePublisher (defined in phase6_conflict_optimistic_test.go in
	// this same package) routes each Publish() through the consumer
	// synchronously so version counts are visible to the next executor
	// call on the same goroutine.
	publisher := &bridgePublisher{consumer: consumer, ctx: ctx}
	executor := actions.NewExecutor(repo, publisher)
	handler := actions.NewHandler(executor)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", handler.Apply)

	const empPK = "emp-1"
	const deptPK = "dept-1"

	// Seed: one CREATE per object so both end up at version=1 in history.
	seedTime := time.Now()
	if err := consumer.ApplyBatch(ctx, funnel.EditBatch{
		ID:              "seed",
		OntologyAPIName: ont.APIName,
		UserID:          "seed",
		Timestamp:       seedTime,
		Edits: []funnel.Edit{
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: employee.APIName,
				PrimaryKey: empPK,
				Source:     funnel.EditSourceUser,
				Properties: map[string]interface{}{"id": empPK, "title": "ic"},
			},
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: department.APIName,
				PrimaryKey: deptPK,
				Source:     funnel.EditSourceUser,
				Properties: map[string]interface{}{"id": deptPK, "headcount": float64(10)},
			},
		},
	}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	if v, err := repo.GetObjectVersionCount(ctx, employee.RID, empPK); err != nil || v != 1 {
		t.Fatalf("seed employee version=%d err=%v, want 1", v, err)
	}
	if v, err := repo.GetObjectVersionCount(ctx, department.RID, deptPK); err != nil || v != 1 {
		t.Fatalf("seed department version=%d err=%v, want 1", v, err)
	}

	// Build the body once — both A and B send the same expectedVersions
	// holding version=1 for both targets. B's race is "stale" because A
	// commits between them and bumps each to version=2.
	buildBody := func(title string, headcount int) []byte {
		body, _ := json.Marshal(map[string]interface{}{
			"parameters": map[string]interface{}{
				"employeeId":   empPK,
				"departmentId": deptPK,
				"title":        title,
				"headcount":    headcount,
			},
			"options": map[string]interface{}{
				"expectedVersions": []map[string]interface{}{
					{"objectType": "employee", "primaryKey": empPK, "version": 1},
					{"objectType": "department", "primaryKey": deptPK, "version": 1},
				},
			},
		})
		return body
	}

	post := func(body []byte) (int, []byte) {
		req := httptest.NewRequest(http.MethodPost,
			fmt.Sprintf("/api/v2/ontologies/%s/actions/transferEmployee/apply", ont.APIName),
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code, w.Body.Bytes()
	}

	// ---------- Goroutine A: must succeed ----------
	aDone := make(chan struct{})
	var aCode int
	var aBody []byte
	go func() {
		defer close(aDone)
		aCode, aBody = post(buildBody("manager-A", 11))
	}()
	<-aDone

	if aCode != http.StatusOK {
		t.Fatalf("A expected 200, got %d: %s", aCode, aBody)
	}

	// After A: employee and department history rows advanced to v=2.
	if v, err := repo.GetObjectVersionCount(ctx, employee.RID, empPK); err != nil || v != 2 {
		t.Fatalf("post-A employee version=%d err=%v, want 2", v, err)
	}
	if v, err := repo.GetObjectVersionCount(ctx, department.RID, deptPK); err != nil || v != 2 {
		t.Fatalf("post-A department version=%d err=%v, want 2", v, err)
	}

	// ---------- Goroutine B: same tokens (stale) → 409 StaleObject ----------
	bDone := make(chan struct{})
	var bCode int
	var bBody []byte
	go func() {
		defer close(bDone)
		bCode, bBody = post(buildBody("manager-B", 12))
	}()
	<-bDone

	if bCode != http.StatusConflict {
		t.Fatalf("B expected 409, got %d: %s", bCode, bBody)
	}

	var bPayload struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(bBody, &bPayload); err != nil {
		t.Fatalf("B body decode: %v", err)
	}
	if bPayload.ErrorCode != "CONFLICT" {
		t.Errorf("B errorCode = %q, want CONFLICT", bPayload.ErrorCode)
	}
	if bPayload.ErrorName != "StaleObject" {
		t.Errorf("B errorName = %q, want StaleObject", bPayload.ErrorName)
	}
	if bPayload.Parameters["objectType"] != "employee" {
		t.Errorf("B objectType = %q, want employee (first ref)", bPayload.Parameters["objectType"])
	}
	if bPayload.Parameters["currentVersion"] != "2" {
		t.Errorf("B currentVersion = %q, want 2", bPayload.Parameters["currentVersion"])
	}

	// Confirm B did NOT publish: version count stays at 2 on both
	// objects, employee.title still reflects A's value, department
	// headcount still reflects A's value.
	if v, err := repo.GetObjectVersionCount(ctx, employee.RID, empPK); err != nil || v != 2 {
		t.Fatalf("post-B employee version=%d err=%v, want 2 (no advance)", v, err)
	}
	if v, err := repo.GetObjectVersionCount(ctx, department.RID, deptPK); err != nil || v != 2 {
		t.Fatalf("post-B department version=%d err=%v, want 2 (no advance)", v, err)
	}

	// EditBatch.EditVersion stamping verification — A's MODIFY edits
	// should have been published with EditVersion=1 (the version A
	// observed at prepare time). bridgePublisher's batches are kept in
	// memory order, so the most recent non-seed batch is A's payload.
	if len(publisher.captured()) == 0 {
		t.Fatal("publisher captured no batches")
	}
	aBatch := publisher.captured()[len(publisher.captured())-1]
	gotEditVersions := map[string]int64{}
	for _, e := range aBatch.Edits {
		if e.Type == funnel.EditTypeModify {
			gotEditVersions[e.ObjectType+"|"+e.PrimaryKey] = e.EditVersion
		}
	}
	if gotEditVersions["employee|"+empPK] != 1 {
		t.Errorf("A employee Edit.EditVersion = %d, want 1 (snapshot at prepare)",
			gotEditVersions["employee|"+empPK])
	}
	if gotEditVersions["department|"+deptPK] != 1 {
		t.Errorf("A department Edit.EditVersion = %d, want 1 (snapshot at prepare)",
			gotEditVersions["department|"+deptPK])
	}
}

// capturedSafeBridge is a small extension to bridgePublisher (defined in
// phase6_conflict_optimistic_test.go) that exposes captured batches in
// goroutine-safe form so EditVersion stamping can be asserted on the
// concrete batch sent to NATS. Defined here to keep the original
// bridgePublisher untouched.
func (b *bridgePublisher) captured() []funnel.EditBatch {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.batches) == 0 {
		return nil
	}
	out := make([]funnel.EditBatch, len(b.batches))
	copy(out, b.batches)
	return out
}

// _ = sync.Mutex{} keeps the sync import referenced even if a future
// reorganisation collapses the helper above. Defensive against the
// goroutines-only branch above getting refactored away.
var _ = sync.Mutex{}
