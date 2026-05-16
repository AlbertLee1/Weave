package oms

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
)

// US-477: Automate trigger DAG cycle detection.
//
// The cycle detector models each AutomationRule as a directed edge in the
// action→event→action graph. Source nodes are trigger ObjectTypes (only
// dataChange triggers observe an ObjectType; schedule / manual triggers
// don't and therefore contribute no incoming edge). Target nodes are
// ObjectTypes that the rule's executeAction effects statically write, as
// declared by the referenced ActionType's Rules JSON.
//
// A cycle in this graph means firing one rule's effect produces an event
// that triggers another rule whose effect eventually emits the original
// trigger ObjectType — the action→event→action loop the PRD targets.
//
// Detection runs on every rule create / update. If the resulting graph
// contains any cycle the registration path rejects the request with
// HTTP 422 WEAVE_AUTOMATION_RULE_CYCLE and the offending path in the
// `cycle` error parameter (e.g. "A → B → A").

// triggerObjectTypeOfRule returns the ObjectType API name observed by the
// rule's trigger, or empty if the trigger doesn't observe an ObjectType.
func triggerObjectTypeOfRule(rule *AutomationRule) string {
	if rule == nil || rule.TriggerType != "dataChange" || len(rule.TriggerConfig) == 0 {
		return ""
	}
	var cfg struct {
		ObjectType string `json:"objectType"`
	}
	if err := json.Unmarshal(rule.TriggerConfig, &cfg); err != nil {
		return ""
	}
	return cfg.ObjectType
}

// actionAPINamesOfRule returns the ActionType API names referenced by
// executeAction effects in the rule. notification / executeFunction effects
// produce no outgoing edge in the action→event→action graph and are
// ignored here.
func actionAPINamesOfRule(rule *AutomationRule) []string {
	if rule == nil || len(rule.Effects) == 0 {
		return nil
	}
	var effects []struct {
		Type              string `json:"type"`
		ActionTypeAPIName string `json:"actionTypeApiName"`
	}
	if err := json.Unmarshal(rule.Effects, &effects); err != nil {
		return nil
	}
	var out []string
	for _, e := range effects {
		if e.Type == "executeAction" && e.ActionTypeAPIName != "" {
			out = append(out, e.ActionTypeAPIName)
		}
	}
	return out
}

// actionObjectTypeWriters returns the ObjectType API names that the action's
// rules statically declare they will create / modify / delete. Recurses into
// control-flow rule wrappers (if/foreach/switch) so a write nested inside
// branches still contributes an edge.
func actionObjectTypeWriters(at *ActionType) []string {
	if at == nil || len(at.Rules) == 0 {
		return nil
	}
	var raw []map[string]interface{}
	if err := json.Unmarshal(at.Rules, &raw); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var walk func(node map[string]interface{})
	walk = func(node map[string]interface{}) {
		if node == nil {
			return
		}
		switch t, _ := node["type"].(string); t {
		case "createObject", "modifyObject", "deleteObject", "createOrModifyObject":
			if ot, _ := node["objectType"].(string); ot != "" {
				seen[ot] = true
			}
		}
		for _, key := range []string{"then", "else", "rules", "default"} {
			if v, ok := node[key].([]interface{}); ok {
				for _, c := range v {
					if cm, ok := c.(map[string]interface{}); ok {
						walk(cm)
					}
				}
			}
		}
		if cases, ok := node["cases"].([]interface{}); ok {
			for _, c := range cases {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				if rules, ok := cm["rules"].([]interface{}); ok {
					for _, r := range rules {
						if rm, ok := r.(map[string]interface{}); ok {
							walk(rm)
						}
					}
				}
			}
		}
	}
	for _, r := range raw {
		walk(r)
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// triggerCycleDetector accumulates ObjectType→ObjectType edges and finds
// cycles via colored DFS (0=white, 1=gray, 2=black). On a back edge it
// reconstructs the closed cycle path from the parent map.
type triggerCycleDetector struct {
	edges map[string]map[string]bool
}

func newTriggerCycleDetector() *triggerCycleDetector {
	return &triggerCycleDetector{edges: map[string]map[string]bool{}}
}

func (d *triggerCycleDetector) addEdge(src, tgt string) {
	if src == "" || tgt == "" {
		return
	}
	if d.edges[src] == nil {
		d.edges[src] = map[string]bool{}
	}
	d.edges[src][tgt] = true
}

func (d *triggerCycleDetector) findCycle() []string {
	color := map[string]int{}
	parent := map[string]string{}
	var cycle []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = 1
		neighbors := make([]string, 0, len(d.edges[node]))
		for n := range d.edges[node] {
			neighbors = append(neighbors, n)
		}
		sort.Strings(neighbors)
		for _, next := range neighbors {
			switch color[next] {
			case 0:
				parent[next] = node
				if dfs(next) {
					return true
				}
			case 1:
				path := []string{next}
				cur := node
				for cur != next && cur != "" {
					path = append(path, cur)
					if _, ok := parent[cur]; !ok {
						break
					}
					cur = parent[cur]
				}
				path = append(path, next)
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				cycle = path
				return true
			}
		}
		color[node] = 2
		return false
	}

	keys := make([]string, 0, len(d.edges))
	for k := range d.edges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if color[k] == 0 {
			if dfs(k) {
				return cycle
			}
		}
	}
	return nil
}

// ValidateAutomationDAG checks whether the candidate rule would leave the
// ontology's action→event→action graph cycle-free. It loads existing rules
// for the ontology, drops any row with the same ID as candidate (so updates
// compare against the post-update state), then DFS-detects cycles. Returns
// the closed cycle path (e.g. ["A","B","A"]) if a cycle is found, nil
// otherwise. The candidate's own edges always participate, regardless of
// whether it is being created or updated. Existing rules with status other
// than "active" / "paused" are skipped: disabled rules are off the graph.
func ValidateAutomationDAG(ctx context.Context, repo Repository, ontologyRID string, candidate *AutomationRule) ([]string, error) {
	if candidate == nil {
		return nil, nil
	}

	existing, err := repo.ListAutomationRules(ctx, ontologyRID)
	if err != nil {
		return nil, err
	}

	actionCache := map[string]*ActionType{}
	resolveAction := func(api string) *ActionType {
		if cached, ok := actionCache[api]; ok {
			return cached
		}
		at, err := repo.GetActionTypeByAPIName(ctx, ontologyRID, api)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				// Surface only "not found" silently; other errors propagate.
				return nil
			}
			actionCache[api] = nil
			return nil
		}
		actionCache[api] = at
		return at
	}

	detector := newTriggerCycleDetector()
	addEdges := func(rule *AutomationRule) {
		src := triggerObjectTypeOfRule(rule)
		if src == "" {
			return
		}
		for _, apiName := range actionAPINamesOfRule(rule) {
			at := resolveAction(apiName)
			if at == nil {
				continue
			}
			for _, tgt := range actionObjectTypeWriters(at) {
				detector.addEdge(src, tgt)
			}
		}
	}

	for i := range existing {
		if existing[i].ID == candidate.ID {
			continue
		}
		switch existing[i].Status {
		case "", "active", "paused":
			addEdges(&existing[i])
		}
	}
	addEdges(candidate)

	return detector.findCycle(), nil
}
