//go:build bdd

package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// registerAutomationSteps wires the US-015 automation_rule_lifecycle
// feature's step regex onto the scenario context. The harness drives the
// real chi-routed OMSHandler automation rule endpoints against a real PG
// container (via suiteState) and asserts at three layers: HTTP status
// code, structured response body, and durable rows in the
// automation_rules + automation_executions tables.
//
// Because no asynchronous trigger executor exists for automation rules
// yet, the "fires with payload" step is a synchronous in-test emulator
// that re-fetches the rule from PG, applies the documented contract
// (status=="active" AND trigger_config.condition truthy → record one
// success execution row, otherwise no-op), and persists via the same
// PGRepository so the executions endpoint observes the same rows the
// real async executor would have written.
func registerAutomationSteps(t testing.TB, sc *godog.ScenarioContext, state *suiteState) {
	// --- When: create + lifecycle transitions on the rule -------------

	sc.When(
		`^I POST a new automation rule named "([^"]+)" on ontology "([^"]+)" with triggerType "([^"]+)" and condition "([^"]+)"$`,
		func(name, ontologyAPIName, triggerType, condition string) error {
			// The Background step "Given a fresh weave database" already
			// brought the container up and seeded the ontology — do NOT
			// call ensureContainer here, that path TRUNCATEs ontologies
			// CASCADE and wipes the just-seeded fixture.
			triggerConfig := map[string]string{"condition": condition}
			tcJSON, err := json.Marshal(triggerConfig)
			if err != nil {
				return fmt.Errorf("marshal triggerConfig: %w", err)
			}
			body := map[string]interface{}{
				"name":          name,
				"description":   "BDD US-015 fixture",
				"triggerType":   triggerType,
				"triggerConfig": json.RawMessage(tcJSON),
				"effects":       json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"noop"}]`),
				"createdBy":     "bdd",
			}
			payload, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("marshal create body: %w", err)
			}
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/ontologies/"+ontologyAPIName+"/automationRules",
				bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			state.automationRouter.ServeHTTP(rr, req)
			state.lastAutomationResponse = &automationHTTPResult{
				statusCode: rr.Code,
				body:       rr.Body.Bytes(),
			}
			// On success, capture the rule ID so subsequent steps can
			// reference the rule by its declarative name. We do not
			// short-circuit on failure — the next step asserts status.
			if rr.Code == http.StatusCreated {
				var created oms.AutomationRule
				if err := json.Unmarshal(rr.Body.Bytes(), &created); err == nil && created.ID != "" {
					state.rememberAutomationRule(name, created.ID)
				}
			}
			return nil
		},
	)

	sc.When(`^I POST pause on automation rule "([^"]+)"$`, func(name string) error {
		id, ok := state.automationRuleIDFor(name)
		if !ok {
			return fmt.Errorf("automation rule %q not seeded", name)
		}
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/bdd_automation/automationRules/"+id+"/pause", nil)
		rr := httptest.NewRecorder()
		state.automationRouter.ServeHTTP(rr, req)
		state.lastAutomationResponse = &automationHTTPResult{
			statusCode: rr.Code,
			body:       rr.Body.Bytes(),
		}
		return nil
	})

	sc.When(`^I POST resume on automation rule "([^"]+)"$`, func(name string) error {
		id, ok := state.automationRuleIDFor(name)
		if !ok {
			return fmt.Errorf("automation rule %q not seeded", name)
		}
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/bdd_automation/automationRules/"+id+"/resume", nil)
		rr := httptest.NewRecorder()
		state.automationRouter.ServeHTTP(rr, req)
		state.lastAutomationResponse = &automationHTTPResult{
			statusCode: rr.Code,
			body:       rr.Body.Bytes(),
		}
		return nil
	})

	sc.When(`^the automation rule "([^"]+)" fires with payload (.+)$`,
		func(name, payloadJSON string) error {
			id, ok := state.automationRuleIDFor(name)
			if !ok {
				return fmt.Errorf("automation rule %q not seeded", name)
			}
			ctx := context.Background()
			rule, err := state.repo.GetAutomationRule(ctx, id)
			if err != nil {
				return fmt.Errorf("GetAutomationRule(%s): %w", id, err)
			}
			// Contract: paused or disabled rules drop trigger fires.
			if rule.Status != "active" {
				return nil
			}
			// Contract: condition gate. The BDD fixture stores the
			// condition as a string flag under trigger_config.condition;
			// anything other than "true" is treated as falsy. Real
			// production wiring will swap this for CEL evaluation
			// against the trigger payload, but the lifecycle invariant
			// (no execution row when condition is falsy) is identical.
			cond, err := extractCondition(rule.TriggerConfig)
			if err != nil {
				return fmt.Errorf("parse triggerConfig.condition: %w", err)
			}
			if cond != "true" {
				return nil
			}
			started := time.Now().UTC()
			completed := started.Add(1 * time.Millisecond)
			exec := &oms.AutomationExecution{
				ID:           rid.NewAutomationExecutionRID(),
				RuleID:       id,
				TriggerEvent: json.RawMessage(payloadJSON),
				StartedAt:    started,
				CompletedAt:  &completed,
				Status:       "success",
				Result:       json.RawMessage(`{"emitted":1}`),
			}
			if err := state.repo.InsertExecution(ctx, exec); err != nil {
				return fmt.Errorf("InsertExecution: %w", err)
			}
			return nil
		},
	)

	// --- Then: HTTP layer ---------------------------------------------

	sc.Then(`^the automation HTTP status code is (\d+)$`, func(want int) error {
		if state.lastAutomationResponse == nil {
			return errors.New("no automation response captured")
		}
		if state.lastAutomationResponse.statusCode != want {
			return fmt.Errorf("automation status code = %d, want %d; body=%s",
				state.lastAutomationResponse.statusCode, want,
				state.lastAutomationResponse.body)
		}
		return nil
	})

	sc.Then(`^the automation response body has status "([^"]+)"$`, func(want string) error {
		resp, err := decodeAutomationRuleResponse(state.lastAutomationResponse)
		if err != nil {
			return err
		}
		if resp.Status != want {
			return fmt.Errorf("response.status = %q, want %q", resp.Status, want)
		}
		return nil
	})

	sc.Then(`^the automation response body has triggerType "([^"]+)"$`, func(want string) error {
		resp, err := decodeAutomationRuleResponse(state.lastAutomationResponse)
		if err != nil {
			return err
		}
		if resp.TriggerType != want {
			return fmt.Errorf("response.triggerType = %q, want %q", resp.TriggerType, want)
		}
		return nil
	})

	sc.Then(`^the automation response errorName is "([^"]+)"$`, func(want string) error {
		if state.lastAutomationResponse == nil {
			return errors.New("no automation response captured")
		}
		var generic map[string]interface{}
		if err := json.Unmarshal(state.lastAutomationResponse.body, &generic); err != nil {
			return fmt.Errorf("decode automation error body: %w", err)
		}
		got, _ := generic["errorName"].(string)
		if got != want {
			return fmt.Errorf("errorName = %q, want %q; body=%s",
				got, want, string(state.lastAutomationResponse.body))
		}
		return nil
	})

	// --- Then: DB row assertions on automation_rules ------------------

	sc.Then(`^the automation rule "([^"]+)" exists in the database with status "([^"]+)"$`,
		func(name, want string) error {
			id, ok := state.automationRuleIDFor(name)
			if !ok {
				return fmt.Errorf("automation rule %q not seeded", name)
			}
			rule, err := state.repo.GetAutomationRule(context.Background(), id)
			if err != nil {
				return fmt.Errorf("GetAutomationRule(%s): %w", id, err)
			}
			if rule.Status != want {
				return fmt.Errorf("automation_rules.status = %q, want %q", rule.Status, want)
			}
			return nil
		},
	)

	// --- Then: DB row assertions on automation_executions -------------

	sc.Then(`^the automation rule "([^"]+)" has (\d+) execution rows? in the database$`,
		func(name string, want int) error {
			id, ok := state.automationRuleIDFor(name)
			if !ok {
				return fmt.Errorf("automation rule %q not seeded", name)
			}
			execs, err := state.repo.ListExecutions(context.Background(), id)
			if err != nil {
				return fmt.Errorf("ListExecutions(%s): %w", id, err)
			}
			if got := len(execs); got != want {
				return fmt.Errorf("automation_executions rows = %d, want %d", got, want)
			}
			return nil
		},
	)

	sc.Then(`^the most recent execution of automation rule "([^"]+)" has status "([^"]+)"$`,
		func(name, want string) error {
			id, ok := state.automationRuleIDFor(name)
			if !ok {
				return fmt.Errorf("automation rule %q not seeded", name)
			}
			execs, err := state.repo.ListExecutions(context.Background(), id)
			if err != nil {
				return fmt.Errorf("ListExecutions(%s): %w", id, err)
			}
			if len(execs) == 0 {
				return fmt.Errorf("no executions for rule %q", name)
			}
			latest := execs[len(execs)-1]
			if latest.Status != want {
				return fmt.Errorf("latest execution.status = %q, want %q", latest.Status, want)
			}
			return nil
		},
	)

	// --- Then: end-to-end check through the executions HTTP endpoint --

	sc.Then(`^the automation executions endpoint returns (\d+) entr(?:y|ies) for rule "([^"]+)"$`,
		func(want int, name string) error {
			id, ok := state.automationRuleIDFor(name)
			if !ok {
				return fmt.Errorf("automation rule %q not seeded", name)
			}
			req := httptest.NewRequest(http.MethodGet,
				"/api/v2/ontologies/bdd_automation/automationRules/"+id+"/executions", nil)
			rr := httptest.NewRecorder()
			state.automationRouter.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				return fmt.Errorf("executions GET status = %d, want 200; body=%s",
					rr.Code, rr.Body.String())
			}
			var page struct {
				Data  []oms.AutomationExecution `json:"data"`
				Total int                       `json:"total"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
				return fmt.Errorf("decode executions page: %w", err)
			}
			if page.Total != want {
				return fmt.Errorf("executions endpoint total = %d, want %d", page.Total, want)
			}
			if len(page.Data) != want {
				return fmt.Errorf("executions endpoint data length = %d, want %d",
					len(page.Data), want)
			}
			return nil
		},
	)
}

// extractCondition pulls the "condition" string flag out of an
// automation rule's trigger_config JSON. Returns an empty string when
// the field is absent. Anything other than literal "true" is treated as
// falsy by the lifecycle contract.
func extractCondition(triggerConfig json.RawMessage) (string, error) {
	if len(triggerConfig) == 0 {
		return "", nil
	}
	var generic map[string]interface{}
	if err := json.Unmarshal(triggerConfig, &generic); err != nil {
		return "", err
	}
	if v, ok := generic["condition"].(string); ok {
		return v, nil
	}
	return "", nil
}

// decodeAutomationRuleResponse unmarshals the success body of the
// create/pause/resume endpoints into an AutomationRule. Returns an error
// when the body is empty or malformed; callers use this for status +
// triggerType assertions on Then-steps.
func decodeAutomationRuleResponse(resp *automationHTTPResult) (*oms.AutomationRule, error) {
	if resp == nil {
		return nil, errors.New("no automation response captured")
	}
	var rule oms.AutomationRule
	if err := json.Unmarshal(resp.body, &rule); err != nil {
		return nil, fmt.Errorf("decode automation rule body: %w; body=%s",
			err, string(resp.body))
	}
	return &rule, nil
}
