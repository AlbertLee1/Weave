package automate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/where"
	"github.com/liyang/weave/pkg/rid"
)

// DataChangeTriggerConfig defines the trigger configuration for data-change-based automation rules.
type DataChangeTriggerConfig struct {
	ObjectType string          `json:"objectType"`
	EditTypes  []string        `json:"editTypes"`
	Where      json.RawMessage `json:"where,omitempty"`
	DebounceMs int             `json:"debounceMs,omitempty"`
}

// ParseDataChangeTriggerConfig parses trigger config JSON into a DataChangeTriggerConfig.
func ParseDataChangeTriggerConfig(raw json.RawMessage) (*DataChangeTriggerConfig, error) {
	var cfg DataChangeTriggerConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid trigger config JSON: %w", err)
	}
	if cfg.ObjectType == "" {
		return nil, fmt.Errorf("objectType is required in data change trigger config")
	}
	return &cfg, nil
}

// DataChangeRuleLoader loads active data-change automation rules from storage.
type DataChangeRuleLoader interface {
	ListActiveDataChangeRules(ctx context.Context) ([]oms.AutomationRule, error)
}

// ObjectPropertyFetcher fetches object properties for where clause evaluation.
type ObjectPropertyFetcher interface {
	FetchProperties(ctx context.Context, objectType, primaryKey string) (map[string]interface{}, error)
}

// watcherRule is a parsed, cached automation rule ready for event matching.
type watcherRule struct {
	rule   oms.AutomationRule
	config *DataChangeTriggerConfig
	where  *where.WhereClause // parsed from config.Where; nil if no where clause
}

// debounceEntry tracks a pending debounced execution.
type debounceEntry struct {
	timer *time.Timer
	event funnel.ChangeEvent
}

// Watcher evaluates incoming data change events against active automation rules.
type Watcher struct {
	loader             DataChangeRuleLoader
	recorder           ExecutionRecorder
	propertyFetcher    ObjectPropertyFetcher
	actionApplier      ActionApplier
	functionDispatcher AutomateFunctionDispatcher
	notifier           NotificationCreator
	mu                 sync.Mutex
	rules              []watcherRule
	debounceTimers     map[string]*debounceEntry // ruleID → pending debounce
	ctx                context.Context
	cancel             context.CancelFunc
}

// NewWatcher creates a new Watcher.
func NewWatcher(loader DataChangeRuleLoader, recorder ExecutionRecorder) *Watcher {
	return &Watcher{
		loader:         loader,
		recorder:       recorder,
		rules:          nil,
		debounceTimers: make(map[string]*debounceEntry),
	}
}

// SetPropertyFetcher sets an optional property fetcher for where clause evaluation.
func (w *Watcher) SetPropertyFetcher(fetcher ObjectPropertyFetcher) {
	w.propertyFetcher = fetcher
}

// SetActionApplier sets the action applier for executeAction effects.
func (w *Watcher) SetActionApplier(applier ActionApplier) {
	w.actionApplier = applier
}

// SetFunctionDispatcher sets the function dispatcher for executeFunction effects.
func (w *Watcher) SetFunctionDispatcher(dispatcher AutomateFunctionDispatcher) {
	w.functionDispatcher = dispatcher
}

// SetNotificationCreator sets the notification creator for notification effects.
func (w *Watcher) SetNotificationCreator(notifier NotificationCreator) {
	w.notifier = notifier
}

// Start loads active data-change rules and prepares the watcher for event processing.
func (w *Watcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)

	rules, err := w.loader.ListActiveDataChangeRules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load active data change rules: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, rule := range rules {
		wr, err := parseWatcherRule(rule)
		if err != nil {
			log.Printf("[automate] failed to parse rule %s (%s): %v", rule.ID, rule.Name, err)
			continue
		}
		w.rules = append(w.rules, *wr)
	}

	return nil
}

// Stop cancels pending debounce timers and cleans up.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for id, entry := range w.debounceTimers {
		entry.timer.Stop()
		delete(w.debounceTimers, id)
	}

	if w.cancel != nil {
		w.cancel()
	}
}

// AddRule dynamically adds a data-change rule to the watcher.
func (w *Watcher) AddRule(rule oms.AutomationRule) error {
	wr, err := parseWatcherRule(rule)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.rules = append(w.rules, *wr)
	return nil
}

// RemoveRule removes a rule from the watcher by ID.
func (w *Watcher) RemoveRule(ruleID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i, r := range w.rules {
		if r.rule.ID == ruleID {
			w.rules = append(w.rules[:i], w.rules[i+1:]...)
			break
		}
	}

	// Cancel any pending debounce timer
	if entry, ok := w.debounceTimers[ruleID]; ok {
		entry.timer.Stop()
		delete(w.debounceTimers, ruleID)
	}
}

// PauseRule removes a rule from the watcher (same as RemoveRule, called on pause).
func (w *Watcher) PauseRule(ruleID string) {
	w.RemoveRule(ruleID)
}

// ResumeRule re-adds a rule to the watcher (called on resume).
func (w *Watcher) ResumeRule(rule oms.AutomationRule) error {
	return w.AddRule(rule)
}

// HandleChangeEvent is the callback that evaluates a ChangeEvent against all active rules.
// Wire this as the consumer's SetOnChange callback.
func (w *Watcher) HandleChangeEvent(event funnel.ChangeEvent) {
	w.mu.Lock()
	rules := make([]watcherRule, len(w.rules))
	copy(rules, w.rules)
	w.mu.Unlock()

	for _, wr := range rules {
		if w.matchRule(&wr, event) {
			w.triggerRule(&wr, event)
		}
	}
}

// matchRule checks if a ChangeEvent matches a rule's trigger config.
func (w *Watcher) matchRule(wr *watcherRule, event funnel.ChangeEvent) bool {
	// Check objectType
	if wr.config.ObjectType != event.ObjectType {
		return false
	}

	// Check editType
	if !editTypeMatches(wr.config.EditTypes, event.EditType) {
		return false
	}

	// Check where clause (optional)
	if wr.where != nil {
		if w.propertyFetcher == nil {
			// No fetcher available — skip where evaluation (treat as match)
			return true
		}

		ctx := w.ctx
		if ctx == nil {
			ctx = context.Background()
		}

		props, err := w.propertyFetcher.FetchProperties(ctx, event.ObjectType, event.PrimaryKey)
		if err != nil {
			log.Printf("[automate] failed to fetch properties for %s/%s: %v", event.ObjectType, event.PrimaryKey, err)
			return false
		}
		if props == nil {
			// Object not found (e.g., DELETE) — treat as match
			return true
		}

		return where.MatchClause(wr.where, props)
	}

	return true
}

// triggerRule fires or debounces execution for a matched rule.
func (w *Watcher) triggerRule(wr *watcherRule, event funnel.ChangeEvent) {
	if wr.config.DebounceMs <= 0 {
		w.executeRule(wr.rule.ID, wr.rule.OntologyRID, event, wr.rule.Effects)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	ruleID := wr.rule.ID
	ontologyRID := wr.rule.OntologyRID
	debounceMs := wr.config.DebounceMs
	effects := wr.rule.Effects

	// Reset existing debounce timer
	if entry, ok := w.debounceTimers[ruleID]; ok {
		entry.timer.Stop()
		entry.event = event
		entry.timer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, func() {
			w.mu.Lock()
			e := w.debounceTimers[ruleID]
			delete(w.debounceTimers, ruleID)
			w.mu.Unlock()
			if e != nil {
				w.executeRule(ruleID, ontologyRID, e.event, effects)
			}
		})
	} else {
		entry := &debounceEntry{event: event}
		entry.timer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, func() {
			w.mu.Lock()
			e := w.debounceTimers[ruleID]
			delete(w.debounceTimers, ruleID)
			w.mu.Unlock()
			if e != nil {
				w.executeRule(ruleID, ontologyRID, e.event, effects)
			}
		})
		w.debounceTimers[ruleID] = entry
	}
}

func (w *Watcher) executeRule(ruleID, ontologyRID string, event funnel.ChangeEvent, effects json.RawMessage) {
	startedAt := time.Now()

	triggerEvent, _ := json.Marshal(map[string]interface{}{
		"type":       "dataChange",
		"objectType": event.ObjectType,
		"primaryKey": event.PrimaryKey,
		"editType":   string(event.EditType),
		"ruleId":     ruleID,
	})

	log.Printf("[automate] data change trigger fired for rule %s: %s %s %s", ruleID, event.EditType, event.ObjectType, event.PrimaryKey)

	ctx := w.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Build event data for template resolution
	eventData := &TriggerEventData{
		PrimaryKey: event.PrimaryKey,
		EditType:   string(event.EditType),
		ObjectType: event.ObjectType,
	}

	// Fetch properties for template resolution (if fetcher available)
	if w.propertyFetcher != nil {
		props, err := w.propertyFetcher.FetchProperties(ctx, event.ObjectType, event.PrimaryKey)
		if err == nil && props != nil {
			eventData.Properties = props
		}
	}

	// Process effects
	var effectResults []EffectResult
	var execErr error
	if len(effects) > 0 {
		effectResults, execErr = processEffects(ctx, effects, ontologyRID, eventData, w.actionApplier, w.functionDispatcher, w.notifier)
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

	if err := w.recorder.InsertExecution(ctx, exec); err != nil {
		log.Printf("[automate] failed to record execution for rule %s: %v", ruleID, err)
	}
}

// parseWatcherRule parses an AutomationRule into a watcherRule with cached config.
func parseWatcherRule(rule oms.AutomationRule) (*watcherRule, error) {
	cfg, err := ParseDataChangeTriggerConfig(rule.TriggerConfig)
	if err != nil {
		return nil, err
	}

	wr := &watcherRule{
		rule:   rule,
		config: cfg,
	}

	// Parse where clause if present
	if len(cfg.Where) > 0 && string(cfg.Where) != "null" {
		var wc where.WhereClause
		if err := json.Unmarshal(cfg.Where, &wc); err != nil {
			return nil, fmt.Errorf("invalid where clause: %w", err)
		}
		wr.where = &wc
	}

	return wr, nil
}

// editTypeMatches checks if a ChangeEvent's EditType is in the allowed list.
func editTypeMatches(allowed []string, editType funnel.EditType) bool {
	if len(allowed) == 0 {
		return true // empty list means match all
	}
	et := string(editType)
	for _, a := range allowed {
		if a == et {
			return true
		}
	}
	return false
}
