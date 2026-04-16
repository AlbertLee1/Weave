//go:build integration

package oms_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// --- AutomationRule CRUD ---

func TestAutomationRule_Create(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	rule := &oms.AutomationRule{
		ID:          "ar-001",
		OntologyRID: ont.RID,
		Name:        "Daily sync",
		Description: "Sync data every 6 hours",
		Status:      "active",
		TriggerType: "schedule",
		TriggerConfig: json.RawMessage(`{"cron":"0 */6 * * *"}`),
		Effects:     json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"syncData"}]`),
		CreatedBy:   "user-1",
	}
	err := repo.CreateAutomationRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("CreateAutomationRule failed: %v", err)
	}

	if rule.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if rule.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestAutomationRule_Get(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	rule := &oms.AutomationRule{
		ID:            "ar-002",
		OntologyRID:   ont.RID,
		Name:          "Change watcher",
		Description:   "Watch for employee changes",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: json.RawMessage(`{"objectType":"Employee","editTypes":["CREATE"]}`),
		Effects:       json.RawMessage(`[{"type":"notification","channel":"platform"}]`),
		CreatedBy:     "user-2",
	}
	if err := repo.CreateAutomationRule(context.Background(), rule); err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	got, err := repo.GetAutomationRule(context.Background(), "ar-002")
	if err != nil {
		t.Fatalf("GetAutomationRule failed: %v", err)
	}
	if got.Name != "Change watcher" {
		t.Errorf("expected name 'Change watcher', got %q", got.Name)
	}
	if got.TriggerType != "dataChange" {
		t.Errorf("expected triggerType 'dataChange', got %q", got.TriggerType)
	}
	if got.Status != "active" {
		t.Errorf("expected status 'active', got %q", got.Status)
	}
	if got.OntologyRID != ont.RID {
		t.Errorf("expected ontologyRID %q, got %q", ont.RID, got.OntologyRID)
	}
}

func TestAutomationRule_Get_NotFound(t *testing.T) {
	repo := setupRepo(t)
	_, err := repo.GetAutomationRule(context.Background(), "nonexistent")
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAutomationRule_List(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	for i, name := range []string{"Rule A", "Rule B"} {
		rule := &oms.AutomationRule{
			ID:            "ar-list-" + string(rune('1'+i)),
			OntologyRID:   ont.RID,
			Name:          name,
			Status:        "active",
			TriggerType:   "manual",
			TriggerConfig: json.RawMessage(`{}`),
			Effects:       json.RawMessage(`[]`),
			CreatedBy:     "user-1",
		}
		if err := repo.CreateAutomationRule(context.Background(), rule); err != nil {
			t.Fatalf("seed rule %s: %v", name, err)
		}
	}

	list, err := repo.ListAutomationRules(context.Background(), ont.RID)
	if err != nil {
		t.Fatalf("ListAutomationRules failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 rules, got %d", len(list))
	}
}

func TestAutomationRule_Update(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	rule := &oms.AutomationRule{
		ID:            "ar-upd-001",
		OntologyRID:   ont.RID,
		Name:          "Original Name",
		Description:   "Original Desc",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: json.RawMessage(`{"cron":"0 0 * * *"}`),
		Effects:       json.RawMessage(`[]`),
		CreatedBy:     "user-1",
	}
	if err := repo.CreateAutomationRule(context.Background(), rule); err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	rule.Name = "Updated Name"
	rule.Description = "Updated Desc"
	rule.Status = "paused"
	rule.TriggerConfig = json.RawMessage(`{"cron":"0 */12 * * *"}`)
	rule.Effects = json.RawMessage(`[{"type":"executeAction"}]`)

	err := repo.UpdateAutomationRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("UpdateAutomationRule failed: %v", err)
	}

	got, _ := repo.GetAutomationRule(context.Background(), rule.ID)
	if got.Name != "Updated Name" {
		t.Errorf("expected 'Updated Name', got %q", got.Name)
	}
	if got.Status != "paused" {
		t.Errorf("expected 'paused', got %q", got.Status)
	}
}

func TestAutomationRule_Delete(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	rule := &oms.AutomationRule{
		ID:            "ar-del-001",
		OntologyRID:   ont.RID,
		Name:          "To Delete",
		Status:        "active",
		TriggerType:   "manual",
		TriggerConfig: json.RawMessage(`{}`),
		Effects:       json.RawMessage(`[]`),
		CreatedBy:     "user-1",
	}
	if err := repo.CreateAutomationRule(context.Background(), rule); err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	err := repo.DeleteAutomationRule(context.Background(), rule.ID)
	if err != nil {
		t.Fatalf("DeleteAutomationRule failed: %v", err)
	}

	_, err = repo.GetAutomationRule(context.Background(), rule.ID)
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestAutomationRule_Delete_NotFound(t *testing.T) {
	repo := setupRepo(t)
	err := repo.DeleteAutomationRule(context.Background(), "nonexistent")
	if !errors.Is(err, oms.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- AutomationExecution CRUD ---

func TestAutomationExecution_Insert(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	rule := &oms.AutomationRule{
		ID:            "ar-exec-001",
		OntologyRID:   ont.RID,
		Name:          "Test Rule",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: json.RawMessage(`{}`),
		Effects:       json.RawMessage(`[]`),
		CreatedBy:     "user-1",
	}
	if err := repo.CreateAutomationRule(context.Background(), rule); err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	exec := &oms.AutomationExecution{
		ID:           "ae-001",
		RuleID:       rule.ID,
		TriggerEvent: json.RawMessage(`{"source":"cron","tick":"2026-04-16T00:00:00Z"}`),
		StartedAt:    time.Now(),
		Status:       "running",
		RetryCount:   0,
	}
	err := repo.InsertExecution(context.Background(), exec)
	if err != nil {
		t.Fatalf("InsertExecution failed: %v", err)
	}
}

func TestAutomationExecution_List(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	rule := &oms.AutomationRule{
		ID:            "ar-exec-list",
		OntologyRID:   ont.RID,
		Name:          "Test Rule",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: json.RawMessage(`{}`),
		Effects:       json.RawMessage(`[]`),
		CreatedBy:     "user-1",
	}
	if err := repo.CreateAutomationRule(context.Background(), rule); err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	now := time.Now()
	completedAt := now.Add(5 * time.Second)
	for i, status := range []string{"success", "error", "success"} {
		exec := &oms.AutomationExecution{
			ID:           "ae-list-" + string(rune('1'+i)),
			RuleID:       rule.ID,
			TriggerEvent: json.RawMessage(`{}`),
			StartedAt:    now,
			CompletedAt:  &completedAt,
			Status:       status,
			RetryCount:   0,
		}
		if status == "error" {
			exec.Error = "something went wrong"
			exec.RetryCount = 2
		}
		if err := repo.InsertExecution(context.Background(), exec); err != nil {
			t.Fatalf("seed execution %d: %v", i, err)
		}
	}

	list, err := repo.ListExecutions(context.Background(), rule.ID)
	if err != nil {
		t.Fatalf("ListExecutions failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(list))
	}

	// Verify error execution has error details
	var errExec *oms.AutomationExecution
	for i := range list {
		if list[i].Status == "error" {
			errExec = &list[i]
			break
		}
	}
	if errExec == nil {
		t.Fatal("expected to find error execution")
	}
	if errExec.Error != "something went wrong" {
		t.Errorf("expected error message, got %q", errExec.Error)
	}
	if errExec.RetryCount != 2 {
		t.Errorf("expected retry_count 2, got %d", errExec.RetryCount)
	}
	if errExec.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestAutomationExecution_ListEmpty(t *testing.T) {
	repo := setupRepo(t)
	ont := seedOntology(t, repo)

	rule := &oms.AutomationRule{
		ID:            "ar-exec-empty",
		OntologyRID:   ont.RID,
		Name:          "Empty Rule",
		Status:        "active",
		TriggerType:   "manual",
		TriggerConfig: json.RawMessage(`{}`),
		Effects:       json.RawMessage(`[]`),
		CreatedBy:     "user-1",
	}
	if err := repo.CreateAutomationRule(context.Background(), rule); err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	list, err := repo.ListExecutions(context.Background(), rule.ID)
	if err != nil {
		t.Fatalf("ListExecutions failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 executions, got %d", len(list))
	}
}
