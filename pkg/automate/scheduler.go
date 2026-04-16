package automate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
	"github.com/robfig/cron/v3"
)

// ScheduleTriggerConfig defines the trigger configuration for schedule-based automation rules.
type ScheduleTriggerConfig struct {
	Cron string `json:"cron"`
}

// ParseScheduleTriggerConfig parses trigger config JSON into a ScheduleTriggerConfig.
func ParseScheduleTriggerConfig(raw json.RawMessage) (*ScheduleTriggerConfig, error) {
	var cfg ScheduleTriggerConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid trigger config JSON: %w", err)
	}
	if cfg.Cron == "" {
		return nil, fmt.Errorf("cron expression is required in trigger config")
	}
	return &cfg, nil
}

// RuleLoader loads automation rules from storage.
type RuleLoader interface {
	ListActiveScheduleRules(ctx context.Context) ([]oms.AutomationRule, error)
}

// ExecutionRecorder records automation executions.
type ExecutionRecorder interface {
	InsertExecution(ctx context.Context, exec *oms.AutomationExecution) error
}

// schedulerEntry tracks a cron entry for a rule.
type schedulerEntry struct {
	ruleID  string
	entryID cron.EntryID
}

// Scheduler manages cron-based automation rule execution.
type Scheduler struct {
	loader             RuleLoader
	recorder           ExecutionRecorder
	actionApplier      ActionApplier
	functionDispatcher AutomateFunctionDispatcher
	cron               *cron.Cron
	mu                 sync.Mutex
	entries            map[string]cron.EntryID // ruleID → cron entryID
	ctx                context.Context
}

// New creates a new Scheduler.
func New(loader RuleLoader, recorder ExecutionRecorder) *Scheduler {
	return &Scheduler{
		loader:   loader,
		recorder: recorder,
		entries:  make(map[string]cron.EntryID),
	}
}

// SetActionApplier sets the action applier for executeAction effects.
func (s *Scheduler) SetActionApplier(applier ActionApplier) {
	s.actionApplier = applier
}

// SetFunctionDispatcher sets the function dispatcher for executeFunction effects.
func (s *Scheduler) SetFunctionDispatcher(dispatcher AutomateFunctionDispatcher) {
	s.functionDispatcher = dispatcher
}

// Start initializes the scheduler, loads active schedule rules, and begins cron execution.
func (s *Scheduler) Start(ctx context.Context) error {
	s.ctx = ctx
	s.cron = cron.New()

	// Load active schedule rules
	rules, err := s.loader.ListActiveScheduleRules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load active schedule rules: %w", err)
	}

	for _, rule := range rules {
		if err := s.addRuleInternal(rule); err != nil {
			log.Printf("[automate] failed to add rule %s (%s): %v", rule.ID, rule.Name, err)
		}
	}

	s.cron.Start()
	return nil
}

// Stop stops the scheduler and waits for running jobs to complete.
func (s *Scheduler) Stop() {
	if s.cron != nil {
		s.cron.Stop()
	}
}

// AddRule dynamically adds a schedule rule to the running scheduler.
func (s *Scheduler) AddRule(rule oms.AutomationRule) error {
	return s.addRuleInternal(rule)
}

// RemoveRule removes a rule from the scheduler by ID.
func (s *Scheduler) RemoveRule(ruleID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryID, ok := s.entries[ruleID]
	if !ok {
		return
	}

	s.cron.Remove(entryID)
	delete(s.entries, ruleID)
}

// PauseRule removes a rule from the scheduler (same as RemoveRule, called on pause).
func (s *Scheduler) PauseRule(ruleID string) {
	s.RemoveRule(ruleID)
}

// ResumeRule re-adds a rule to the scheduler (called on resume).
func (s *Scheduler) ResumeRule(rule oms.AutomationRule) error {
	return s.addRuleInternal(rule)
}

func (s *Scheduler) addRuleInternal(rule oms.AutomationRule) error {
	cfg, err := ParseScheduleTriggerConfig(rule.TriggerConfig)
	if err != nil {
		return err
	}

	ruleID := rule.ID
	ontologyRID := rule.OntologyRID
	effects := rule.Effects

	entryID, err := s.cron.AddFunc(cfg.Cron, func() {
		s.executeRule(ruleID, ontologyRID, effects)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", cfg.Cron, err)
	}

	s.mu.Lock()
	s.entries[ruleID] = entryID
	s.mu.Unlock()

	return nil
}

func (s *Scheduler) executeRule(ruleID, ontologyRID string, effects json.RawMessage) {
	startedAt := time.Now()

	triggerEvent, _ := json.Marshal(map[string]interface{}{
		"type":    "schedule",
		"firedAt": startedAt.UTC().Format(time.RFC3339),
		"ruleId":  ruleID,
	})

	log.Printf("[automate] executing scheduled rule %s", ruleID)

	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Build event data (minimal for schedule triggers)
	eventData := &TriggerEventData{}

	// Process effects
	var effectResults []EffectResult
	var execErr error
	if len(effects) > 0 {
		effectResults, execErr = processEffects(ctx, effects, ontologyRID, eventData, s.actionApplier, s.functionDispatcher)
	}

	completedAt := time.Now()
	status := "success"
	errMsg := ""
	if execErr != nil {
		status = "error"
		errMsg = execErr.Error()
		log.Printf("[automate] effect execution failed for rule %s: %v", ruleID, execErr)
	}

	// Serialize effect results for storage
	var resultJSON json.RawMessage
	if len(effectResults) > 0 {
		resultJSON, _ = json.Marshal(effectResults)
	}

	exec := &oms.AutomationExecution{
		ID:           rid.NewAutomationExecutionRID(),
		RuleID:       ruleID,
		TriggerEvent: triggerEvent,
		StartedAt:    startedAt,
		CompletedAt:  &completedAt,
		Status:       status,
		Error:        errMsg,
		Result:       resultJSON,
	}

	if err := s.recorder.InsertExecution(ctx, exec); err != nil {
		log.Printf("[automate] failed to record execution for rule %s: %v", ruleID, err)
	}
}
