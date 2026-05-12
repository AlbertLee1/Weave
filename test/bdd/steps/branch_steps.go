//go:build bdd

package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cucumber/godog"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// httpResult is the scenario-scoped snapshot of the last HTTP exchange. Step
// definitions stash it on suiteState via setLastResponse so later Then-steps
// can assert against statusCode / body without re-issuing the request.
type httpResult struct {
	statusCode int
	body       []byte
}

// scenarioCarryover lives on the scoped key inside a context.Context so step
// callbacks can hand the last response between each other without leaking onto
// suiteState (suiteState carries data that survives the entire test process —
// status codes from one scenario must NOT leak into another).
type scenarioCarryover struct {
	lastMerge    *httpResult
	lastProposal *httpResult
}

func (c *scenarioCarryover) reset() {
	c.lastMerge = nil
	c.lastProposal = nil
}

// registerBranchMergeSteps wires the branch + proposal step regex on top of
// the shared suiteState. The HTTP surface under test is the full chi-routed
// OMSHandler so scenarios exercise the same code paths the cmd/server entry
// point hits in production.
func registerBranchMergeSteps(_ testing.TB, sc *godog.ScenarioContext, state *suiteState) {
	carry := &scenarioCarryover{}

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		carry.reset()
		return ctx, nil
	})

	sc.Given(`^an objectType "([^"]+)" with displayName "([^"]+)" exists in ontology "([^"]+)"$`,
		func(otAPIName, displayName, ontologyAPIName string) error {
			ontologyRID, ok := state.ontologyRIDFor(ontologyAPIName)
			if !ok {
				return fmt.Errorf("ontology %q not seeded", ontologyAPIName)
			}
			otRID := rid.NewObjectTypeRID()
			ot := &oms.ObjectType{
				RID:         otRID,
				OntologyRID: ontologyRID,
				APIName:     otAPIName,
				DisplayName: displayName,
				PrimaryKey:  "id",
				PrimaryKeys: []string{"id"},
				Status:      "ACTIVE",
				Visibility:  "NORMAL",
			}
			if err := state.repo.CreateObjectType(context.Background(), ot); err != nil {
				return fmt.Errorf("CreateObjectType(%s): %w", otAPIName, err)
			}
			state.rememberObjectTypeRID(ontologyAPIName, otAPIName, otRID)
			return nil
		})

	sc.Given(`^an open branch "([^"]+)" off ontology "([^"]+)"$`,
		func(branchName, ontologyAPIName string) error {
			ontologyRID, ok := state.ontologyRIDFor(ontologyAPIName)
			if !ok {
				return fmt.Errorf("ontology %q not seeded", ontologyAPIName)
			}
			// Pin BaseVersion to the current canonical version so that the
			// branch records the snapshot point at fork time — mirrors the
			// CreateBranch handler logic in pkg/oms/handlers_branch.go.
			ver, err := state.repo.GetOntologyVersion(context.Background(), ontologyRID)
			if err != nil {
				ver = 0
			}
			b := &oms.OntologyBranch{
				ID:          rid.NewBranchRID(),
				OntologyRID: ontologyRID,
				Name:        branchName,
				BaseVersion: int64(ver),
				Status:      "open",
				CreatedBy:   "bdd",
			}
			if err := state.repo.CreateBranch(context.Background(), b); err != nil {
				return fmt.Errorf("CreateBranch(%s): %w", branchName, err)
			}
			state.rememberBranch(branchName, b.ID)
			return nil
		})

	sc.Given(`^the branch "([^"]+)" records a MODIFIED change on "([^"]+)" with new displayName "([^"]+)"$`,
		func(branchName, otAPIName, newDisplay string) error {
			return recordModifyObjectTypeChange(state, branchName, otAPIName, "", newDisplay)
		})

	sc.Given(`^the branch "([^"]+)" records a MODIFIED change on "([^"]+)" with before displayName "([^"]+)" and new displayName "([^"]+)"$`,
		func(branchName, otAPIName, beforeDisplay, newDisplay string) error {
			return recordModifyObjectTypeChange(state, branchName, otAPIName, beforeDisplay, newDisplay)
		})

	sc.Given(`^main updates the objectType "([^"]+)" displayName to "([^"]+)"$`,
		func(otAPIName, newDisplay string) error {
			// "main" here means: change the canonical row outside any branch.
			// We resolve the OT RID via the (only) ontology tracked in the
			// scenario state to keep the regex narrow.
			var otRID string
			var ontologyRID string
			state.mu.Lock()
			for key, r := range state.objectTypeRIDs {
				if endsWithSlashThenName(key, otAPIName) {
					otRID = r
				}
			}
			for _, r := range state.apiNameToRID {
				ontologyRID = r
				break
			}
			state.mu.Unlock()
			if otRID == "" {
				return fmt.Errorf("no ObjectType RID tracked for %q", otAPIName)
			}
			ot := &oms.ObjectType{
				RID:         otRID,
				OntologyRID: ontologyRID,
				APIName:     otAPIName,
				DisplayName: newDisplay,
				PrimaryKey:  "id",
				PrimaryKeys: []string{"id"},
				Status:      "ACTIVE",
				Visibility:  "NORMAL",
			}
			if err := state.repo.UpdateObjectType(context.Background(), ot); err != nil {
				return fmt.Errorf("UpdateObjectType(%s): %w", otAPIName, err)
			}
			// Bumping the ontology version mirrors how the admin handlers
			// emit a version increment on schema mutation; the merge handler
			// uses currentVersion > BaseVersion to gate conflict detection.
			if _, err := state.repo.IncrementOntologyVersion(context.Background(), ontologyRID); err != nil {
				return fmt.Errorf("IncrementOntologyVersion: %w", err)
			}
			return nil
		})

	sc.When(`^I POST merge for branch "([^"]+)" with no conflict resolution$`,
		func(branchName string) error {
			return postBranchMerge(state, carry, branchName, "{}")
		})

	sc.When(`^I POST merge for branch "([^"]+)" with conflict resolution body '([^']+)'$`,
		func(branchName, body string) error {
			return postBranchMerge(state, carry, branchName, body)
		})

	sc.Then(`^the merge response status is (\d+)$`, func(want int) error {
		if carry.lastMerge == nil {
			return fmt.Errorf("no merge response recorded")
		}
		if carry.lastMerge.statusCode != want {
			return fmt.Errorf("merge status = %d, want %d; body=%s",
				carry.lastMerge.statusCode, want, string(carry.lastMerge.body))
		}
		return nil
	})

	sc.Then(`^the merge response has appliedCount (\d+) and skippedCount (\d+)$`,
		func(applied, skipped int) error {
			if carry.lastMerge == nil {
				return fmt.Errorf("no merge response recorded")
			}
			var resp oms.MergeBranchResponse
			if err := json.Unmarshal(carry.lastMerge.body, &resp); err != nil {
				return fmt.Errorf("decode merge response: %w; body=%s", err, string(carry.lastMerge.body))
			}
			if resp.AppliedCount != applied {
				return fmt.Errorf("appliedCount = %d, want %d", resp.AppliedCount, applied)
			}
			if resp.SkippedCount != skipped {
				return fmt.Errorf("skippedCount = %d, want %d", resp.SkippedCount, skipped)
			}
			return nil
		})

	sc.Then(`^the merge response errorCode is "([^"]+)"$`, func(want string) error {
		if carry.lastMerge == nil {
			return fmt.Errorf("no merge response recorded")
		}
		var generic map[string]interface{}
		if err := json.Unmarshal(carry.lastMerge.body, &generic); err != nil {
			return fmt.Errorf("decode merge response: %w; body=%s", err, string(carry.lastMerge.body))
		}
		got, _ := generic["errorCode"].(string)
		if got != want {
			return fmt.Errorf("errorCode = %q, want %q; body=%s", got, want, string(carry.lastMerge.body))
		}
		return nil
	})

	sc.Then(`^the merge response lists a conflict on "([^"]+)"$`,
		func(resolutionKey string) error {
			if carry.lastMerge == nil {
				return fmt.Errorf("no merge response recorded")
			}
			var generic map[string]interface{}
			if err := json.Unmarshal(carry.lastMerge.body, &generic); err != nil {
				return fmt.Errorf("decode merge response: %w; body=%s", err, string(carry.lastMerge.body))
			}
			conflicts, _ := generic["conflicts"].([]interface{})
			for _, raw := range conflicts {
				c, _ := raw.(map[string]interface{})
				if c["resolutionKey"] == resolutionKey {
					return nil
				}
			}
			return fmt.Errorf("resolutionKey %q not present in conflicts; body=%s",
				resolutionKey, string(carry.lastMerge.body))
		})

	sc.Then(`^the branch "([^"]+)" has status "([^"]+)" in the database$`,
		func(branchName, want string) error {
			id, ok := state.branchIDFor(branchName)
			if !ok {
				return fmt.Errorf("branch %q not tracked", branchName)
			}
			b, err := state.repo.GetBranch(context.Background(), id)
			if err != nil {
				return fmt.Errorf("GetBranch(%s): %w", id, err)
			}
			if b.Status != want {
				return fmt.Errorf("branch %q status = %q, want %q", branchName, b.Status, want)
			}
			return nil
		})

	sc.Then(`^the objectType "([^"]+)" in ontology "([^"]+)" has displayName "([^"]+)" in the database$`,
		func(otAPIName, ontologyAPIName, want string) error {
			ontologyRID, ok := state.ontologyRIDFor(ontologyAPIName)
			if !ok {
				return fmt.Errorf("ontology %q not seeded", ontologyAPIName)
			}
			ot, err := state.repo.GetObjectTypeByAPIName(context.Background(), ontologyRID, otAPIName)
			if err != nil {
				return fmt.Errorf("GetObjectTypeByAPIName(%s/%s): %w", ontologyAPIName, otAPIName, err)
			}
			if ot.DisplayName != want {
				return fmt.Errorf("displayName = %q, want %q", ot.DisplayName, want)
			}
			return nil
		})

	// --- Proposal lifecycle steps ---

	sc.Given(`^a proposal "([^"]+)" authored by "([^"]+)" targets branch "([^"]+)" with title "([^"]+)"$`,
		func(alias, author, branchName, title string) error {
			branchID, ok := state.branchIDFor(branchName)
			if !ok {
				return fmt.Errorf("branch %q not tracked", branchName)
			}
			b, err := state.repo.GetBranch(context.Background(), branchID)
			if err != nil {
				return fmt.Errorf("GetBranch(%s): %w", branchID, err)
			}
			p := &oms.OntologyProposal{
				ID:          rid.NewProposalRID(),
				BranchID:    branchID,
				OntologyRID: b.OntologyRID,
				Title:       title,
				Status:      "pending",
				Author:      author,
			}
			if err := state.repo.CreateProposal(context.Background(), p); err != nil {
				return fmt.Errorf("CreateProposal: %w", err)
			}
			state.rememberProposal(alias, p.ID)
			return nil
		})

	sc.When(`^"([^"]+)" approves proposal "([^"]+)"$`,
		func(reviewer, alias string) error {
			return postProposalReview(state, carry, alias, "approve", reviewer)
		})

	sc.When(`^"([^"]+)" rejects proposal "([^"]+)"$`,
		func(reviewer, alias string) error {
			return postProposalReview(state, carry, alias, "reject", reviewer)
		})

	sc.When(`^I POST merge for proposal "([^"]+)"$`, func(alias string) error {
		return postProposalMerge(state, carry, alias)
	})

	sc.Then(`^the proposal merge response status is (\d+)$`, func(want int) error {
		if carry.lastProposal == nil {
			return fmt.Errorf("no proposal merge response recorded")
		}
		if carry.lastProposal.statusCode != want {
			return fmt.Errorf("proposal merge status = %d, want %d; body=%s",
				carry.lastProposal.statusCode, want, string(carry.lastProposal.body))
		}
		return nil
	})

	sc.Then(`^the proposal "([^"]+)" has status "([^"]+)" in the database$`,
		func(alias, want string) error {
			id, ok := state.proposalIDFor(alias)
			if !ok {
				return fmt.Errorf("proposal %q not tracked", alias)
			}
			p, err := state.repo.GetProposal(context.Background(), id)
			if err != nil {
				return fmt.Errorf("GetProposal(%s): %w", id, err)
			}
			if p.Status != want {
				return fmt.Errorf("proposal %q status = %q, want %q", alias, p.Status, want)
			}
			return nil
		})
}

// recordModifyObjectTypeChange writes a MODIFIED branch_change row directly
// via the repository, mirroring what admin_handlers.go does when an admin
// PUT lands on a ?branch= path. The BeforeState defaults to the OBJECT's
// current main payload (the merge handler later compares this to live main
// state to decide whether the branch saw stale data → conflict).
func recordModifyObjectTypeChange(state *suiteState, branchName, otAPIName, beforeOverrideDisplay, newDisplay string) error {
	branchID, ok := state.branchIDFor(branchName)
	if !ok {
		return fmt.Errorf("branch %q not tracked", branchName)
	}
	branch, err := state.repo.GetBranch(context.Background(), branchID)
	if err != nil {
		return fmt.Errorf("GetBranch(%s): %w", branchID, err)
	}
	current, err := state.repo.GetObjectTypeByAPIName(context.Background(), branch.OntologyRID, otAPIName)
	if err != nil {
		return fmt.Errorf("GetObjectTypeByAPIName(%s): %w", otAPIName, err)
	}

	before := *current
	if beforeOverrideDisplay != "" {
		before.DisplayName = beforeOverrideDisplay
	}
	after := *current
	after.DisplayName = newDisplay

	beforeJSON, err := json.Marshal(&before)
	if err != nil {
		return fmt.Errorf("encode before state: %w", err)
	}
	afterJSON, err := json.Marshal(&after)
	if err != nil {
		return fmt.Errorf("encode after state: %w", err)
	}

	change := &oms.BranchChange{
		ID:          uuid.New().String(),
		BranchID:    branchID,
		ChangeType:  "MODIFIED",
		EntityType:  "objectType",
		EntityRID:   current.RID,
		BeforeState: beforeJSON,
		AfterState:  afterJSON,
	}
	if err := state.repo.CreateBranchChange(context.Background(), change); err != nil {
		return fmt.Errorf("CreateBranchChange: %w", err)
	}
	return nil
}

func postBranchMerge(state *suiteState, carry *scenarioCarryover, branchName, body string) error {
	branchID, ok := state.branchIDFor(branchName)
	if !ok {
		return fmt.Errorf("branch %q not tracked", branchName)
	}
	branch, err := state.repo.GetBranch(context.Background(), branchID)
	if err != nil {
		return fmt.Errorf("GetBranch(%s): %w", branchID, err)
	}
	ontology, err := state.repo.GetOntology(context.Background(), branch.OntologyRID)
	if err != nil {
		return fmt.Errorf("GetOntology(%s): %w", branch.OntologyRID, err)
	}

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/branches/{branchId}/merge", state.handler.MergeBranch)

	url := fmt.Sprintf("/api/v2/ontologies/%s/branches/%s/merge", ontology.APIName, branchID)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	carry.lastMerge = &httpResult{statusCode: w.Code, body: w.Body.Bytes()}
	return nil
}

func postProposalReview(state *suiteState, carry *scenarioCarryover, alias, decision, reviewer string) error {
	proposalID, ok := state.proposalIDFor(alias)
	if !ok {
		return fmt.Errorf("proposal %q not tracked", alias)
	}
	proposal, err := state.repo.GetProposal(context.Background(), proposalID)
	if err != nil {
		return fmt.Errorf("GetProposal(%s): %w", proposalID, err)
	}
	ontology, err := state.repo.GetOntology(context.Background(), proposal.OntologyRID)
	if err != nil {
		return fmt.Errorf("GetOntology(%s): %w", proposal.OntologyRID, err)
	}

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/approve", state.handler.ApproveProposal)
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/reject", state.handler.RejectProposal)

	verb := "approve"
	if decision == "reject" {
		verb = "reject"
	}
	url := fmt.Sprintf("/api/v2/ontologies/%s/proposals/%s/%s", ontology.APIName, proposalID, verb)
	body := fmt.Sprintf(`{"reviewer":%q}`, reviewer)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	carry.lastProposal = &httpResult{statusCode: w.Code, body: w.Body.Bytes()}
	if w.Code >= 400 {
		return fmt.Errorf("review %s for %q returned %d: %s", verb, alias, w.Code, w.Body.String())
	}
	return nil
}

func postProposalMerge(state *suiteState, carry *scenarioCarryover, alias string) error {
	proposalID, ok := state.proposalIDFor(alias)
	if !ok {
		return fmt.Errorf("proposal %q not tracked", alias)
	}
	proposal, err := state.repo.GetProposal(context.Background(), proposalID)
	if err != nil {
		return fmt.Errorf("GetProposal(%s): %w", proposalID, err)
	}
	ontology, err := state.repo.GetOntology(context.Background(), proposal.OntologyRID)
	if err != nil {
		return fmt.Errorf("GetOntology(%s): %w", proposal.OntologyRID, err)
	}

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/proposals/{proposalId}/merge", state.handler.MergeProposal)

	url := fmt.Sprintf("/api/v2/ontologies/%s/proposals/%s/merge", ontology.APIName, proposalID)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	carry.lastProposal = &httpResult{statusCode: w.Code, body: w.Body.Bytes()}
	return nil
}

// endsWithSlashThenName reports whether the map key "<ontology>/<ot>" matches
// a bare ot name. Pulled out for readability since the same pattern recurs.
func endsWithSlashThenName(mapKey, otAPIName string) bool {
	needle := "/" + otAPIName
	if len(mapKey) < len(needle) {
		return false
	}
	return mapKey[len(mapKey)-len(needle):] == needle
}
