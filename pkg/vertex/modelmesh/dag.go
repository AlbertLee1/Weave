// Package modelmesh implements the Vertex Model Mesh planner and runner
// (VTX-052). A "mesh" is a set of model invocations whose data flow is
// expressed implicitly: each model declares the object properties it
// reads (InputProperties) and the properties it writes
// (OutputProperties). The planner builds the DAG induced by the
// "producer property → consumer property" relation, returns a layered
// topological order whose siblings can run concurrently, and rejects
// cyclic meshes with ErrCycleDetected.
//
// The package is HTTP-thin: the DAG and runner are pure Go with no
// transport coupling. The Handler is a thin façade wired in by
// cmd/server/main.go alongside the other Vertex registrations.
package modelmesh

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ModelNode describes one model invocation participating in a mesh. The
// planner only needs ID + InputProperties + OutputProperties to compute
// the dataflow DAG; FunctionRID and Parameters are carried through so
// the executor wired in by cmd/server/main.go has everything it needs
// to dispatch the underlying Function (e.g. a Live Model Deployment
// wrapper from VTX-050) without a second lookup.
type ModelNode struct {
	ID               string         `json:"id"`
	FunctionRID      string         `json:"functionRid,omitempty"`
	InputProperties  []string       `json:"inputProperties,omitempty"`
	OutputProperties []string       `json:"outputProperties,omitempty"`
	Parameters       map[string]any `json:"parameters,omitempty"`
}

// Layer is the set of model IDs that can execute concurrently within a
// single topological generation. Layers are returned in execution
// order (layer 0 first). IDs within a layer are sorted alphabetically
// to keep the wire output deterministic for snapshot tests and SDK
// consumers.
type Layer []string

// ErrEmptyModelID is returned by BuildDependencyGraph when a node has
// a blank ID. We refuse to silently coerce because two blank IDs
// would collide in the adjacency map without any user-visible signal.
var ErrEmptyModelID = errors.New("modelmesh: model ID is required")

// ErrDuplicateModelID is returned by BuildDependencyGraph when two
// nodes share the same ID. The mesh is keyed by ID throughout (the
// adjacency map, the in-degree map, the layer output), so duplicates
// would silently shadow each other and corrupt the planner's output.
var ErrDuplicateModelID = errors.New("modelmesh: duplicate model ID")

// ErrCycleDetected is the sentinel TopologicalLayers wraps in a
// CycleError. Callers that only need a yes/no should use errors.Is
// against this sentinel; callers that want to display the cycle path
// should use errors.As to unwrap into *CycleError.
var ErrCycleDetected = errors.New("modelmesh: cycle detected")

// CycleError carries the offending cycle path so the HTTP layer (and
// the eventual UI) can show the user which models form the loop. The
// path is a node sequence whose first and last entries are the same
// node, e.g. ["m1","m2","m1"].
type CycleError struct {
	Cycle []string
}

// Error renders the cycle path in arrow form for log consumption
// (the structured cycle is also exposed through CycleError.Cycle for
// UI rendering).
func (e *CycleError) Error() string {
	return fmt.Sprintf("modelmesh: cycle detected: %s", strings.Join(e.Cycle, " -> "))
}

// Is makes errors.Is(err, ErrCycleDetected) succeed for any
// *CycleError, so callers can discriminate without losing the path.
func (e *CycleError) Is(target error) bool {
	return target == ErrCycleDetected
}

// BuildDependencyGraph returns the adjacency map of the dataflow DAG
// induced by `models`. An edge A→B exists when A.OutputProperties and
// B.InputProperties intersect (A produces a property B consumes), so
// A must run before B. Every model ID is a key in the result, with an
// empty slice when the node has no dependents — this guarantees
// downstream consumers can iterate the full node set off the map's
// keys without joining against the original slice.
func BuildDependencyGraph(models []ModelNode) (map[string][]string, error) {
	graph := make(map[string][]string, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, m := range models {
		if strings.TrimSpace(m.ID) == "" {
			return nil, ErrEmptyModelID
		}
		if _, dup := seen[m.ID]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateModelID, m.ID)
		}
		seen[m.ID] = struct{}{}
		graph[m.ID] = nil
	}

	for i, producer := range models {
		if len(producer.OutputProperties) == 0 {
			continue
		}
		producerOutputs := make(map[string]struct{}, len(producer.OutputProperties))
		for _, p := range producer.OutputProperties {
			producerOutputs[p] = struct{}{}
		}
		for j, consumer := range models {
			if i == j {
				if shareAny(producerOutputs, consumer.InputProperties) {
					graph[producer.ID] = append(graph[producer.ID], consumer.ID)
				}
				continue
			}
			if shareAny(producerOutputs, consumer.InputProperties) {
				graph[producer.ID] = append(graph[producer.ID], consumer.ID)
			}
		}
	}

	for id, deps := range graph {
		sort.Strings(deps)
		graph[id] = deps
	}
	return graph, nil
}

func shareAny(producerOutputs map[string]struct{}, consumerInputs []string) bool {
	for _, p := range consumerInputs {
		if _, ok := producerOutputs[p]; ok {
			return true
		}
	}
	return false
}

// TopologicalLayers returns the layered topological order of `models`.
// Layer 0 contains all nodes with no upstream dependencies; layer N
// contains nodes whose dependencies are entirely satisfied by layers
// 0..N-1. A cycle (including a self-loop) yields *CycleError wrapping
// ErrCycleDetected, with the offending path on the Cycle field.
func TopologicalLayers(models []ModelNode) ([]Layer, error) {
	graph, err := BuildDependencyGraph(models)
	if err != nil {
		return nil, err
	}
	if len(graph) == 0 {
		return nil, nil
	}

	indegree := make(map[string]int, len(graph))
	for id := range graph {
		indegree[id] = 0
	}
	for _, deps := range graph {
		for _, dep := range deps {
			indegree[dep]++
		}
	}

	var layers []Layer
	remaining := len(graph)
	for remaining > 0 {
		var layer Layer
		for id, deg := range indegree {
			if deg == 0 {
				layer = append(layer, id)
			}
		}
		if len(layer) == 0 {
			cycle := findCycle(graph, indegree)
			return nil, &CycleError{Cycle: cycle}
		}
		sort.Strings(layer)
		for _, id := range layer {
			for _, dep := range graph[id] {
				indegree[dep]--
			}
			indegree[id] = -1
			remaining--
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

// findCycle returns a cycle path in the residual subgraph that
// remains after Kahn's algorithm fails to make progress. The path
// starts and ends on the same node so callers can render it as
// "m1 -> m2 -> m1" without further bookkeeping.
func findCycle(graph map[string][]string, indegree map[string]int) []string {
	var startID string
	for id, deg := range indegree {
		if deg <= 0 {
			continue
		}
		startID = id
		break
	}
	if startID == "" {
		// No surviving node — defensive fallback so we never return
		// an empty CycleError.Cycle (would defeat the consumer that
		// renders the path).
		for id := range graph {
			startID = id
			break
		}
	}

	const grey = 1
	const black = 2
	color := make(map[string]int, len(graph))
	parent := make(map[string]string, len(graph))
	var cycle []string

	var visit func(node string) bool
	visit = func(node string) bool {
		color[node] = grey
		for _, next := range graph[node] {
			if indegree[next] <= 0 && next != node {
				continue
			}
			switch color[next] {
			case 0:
				parent[next] = node
				if visit(next) {
					return true
				}
			case grey:
				cycle = []string{next}
				cur := node
				for cur != next && cur != "" {
					cycle = append(cycle, cur)
					cur = parent[cur]
				}
				cycle = append(cycle, next)
				reverse(cycle)
				return true
			}
		}
		color[node] = black
		return false
	}

	visit(startID)
	if len(cycle) == 0 {
		// Self-loop: visit() found no grey edge but the node points at
		// itself in graph[startID]. Render it as a 2-element path so
		// the message stays meaningful.
		for _, dep := range graph[startID] {
			if dep == startID {
				return []string{startID, startID}
			}
		}
		return []string{startID, startID}
	}
	return cycle
}

func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
