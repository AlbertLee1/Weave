package oms

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// callTargetRegex is the conservative static scanner for weave.callFunction
// references. It matches `weave.callFunction("ref", ...)` /
// `weave.callFunction('ref', ...)` and the bare `callFunction("ref", ...)`
// shape (in case the shim is destructured into a local) where the first
// argument is a single-quoted or double-quoted literal. A computed first
// argument (`weave.callFunction(name, ...)`) is invisible to the scanner —
// runtime guards (depth limit + visited-stack cycle check from US-220) catch
// the dynamic case at execute time, so the static scan errs on the side of
// false negatives rather than rejecting legitimate dynamic dispatch.
//
// The pattern also tolerates whitespace + comments between the function name
// and the opening paren so multi-line invocations register. Backticked
// template literals are intentionally ignored — they signal templated refs
// the static scanner cannot reason about.
var callTargetRegex = regexp.MustCompile(
	`(?:weave\s*\.\s*)?callFunction\s*\(\s*(?:"([^"\\]*)"|'([^'\\]*)')`,
)

// stripJSCommentsAndStrings rewrites JS source so that string literals,
// template literals, and comments are replaced with whitespace of the same
// length. Keeping the layout means any byte offset reported by a matcher on
// the stripped source aligns with the original — useful for future error
// surfacing — and prevents the call-target regex from matching the literal
// "weave.callFunction(\"ref\")" inside a comment or a string.
func stripJSCommentsAndStrings(source string) string {
	out := make([]byte, len(source))
	copy(out, source)

	i := 0
	for i < len(out) {
		c := out[i]
		switch {
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			// Line comment until newline.
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			// Block comment until */.
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i+1 < len(out) && !(out[i] == '*' && out[i+1] == '/') {
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			if i+1 < len(out) {
				out[i], out[i+1] = ' ', ' '
				i += 2
			}
		case c == '`':
			// Template literal — wipe contents but keep the position so the
			// regex can match the raw `callFunction(...)` form anywhere
			// outside a backtick block.
			out[i] = ' '
			i++
			for i < len(out) && out[i] != '`' {
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			if i < len(out) {
				out[i] = ' '
				i++
			}
		default:
			i++
		}
	}
	return string(out)
}

// ExtractCallTargets parses the function source code and returns the
// distinct, deduped list of static `weave.callFunction("ref", ...)` targets
// it can identify. Order is the first-occurrence order in the source so a
// future error surfacer can quote the line that introduced the cycle.
//
// Refs that depend on a variable / template / runtime computation are
// invisible to the static scanner — the runtime depth + cycle guards in
// US-220 catch the dynamic case at execute time.
func ExtractCallTargets(source string) []string {
	if source == "" {
		return nil
	}
	scrubbed := stripJSCommentsAndStrings(source)
	matches := callTargetRegex.FindAllStringSubmatch(scrubbed, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		ref := m[1]
		if ref == "" {
			ref = m[2]
		}
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

// FunctionCallGraphLookup is the narrow Repository surface DetectCallCycle
// reads from. The interface is satisfied by oms.Repository (production wiring)
// AND by an in-memory map shim that test cases construct directly without
// pulling in the PG implementation, so the scanner can be unit-tested without
// a live database.
type FunctionCallGraphLookup interface {
	GetFunctionByName(ctx context.Context, ontologyRID, name string) (*Function, error)
	GetFunctionByNameVersion(ctx context.Context, ontologyRID, name, version string) (*Function, error)
	GetFunction(ctx context.Context, rid string) (*Function, error)
}

// FunctionCallCycleError is returned by DetectCallCycle when the call graph
// rooted at the published function would close a cycle. Cycle is the dotted
// path of refs as encountered during DFS — its first element is the starting
// function's identifier (RID, name, or name@version) and the last element is
// the ref whose re-entry closes the loop.
type FunctionCallCycleError struct {
	StartRef string
	Cycle    []string
}

// Error implements error.
func (e *FunctionCallCycleError) Error() string {
	return fmt.Sprintf("function call cycle detected: %s", strings.Join(e.Cycle, " → "))
}

// resolveFunctionRef looks up a `name`, `name@version`, or RID-shaped ref via
// the lookup. Returns (nil, nil) for refs that can't be resolved — an unknown
// callee is not a cycle, just a dangling reference the runtime will surface
// as a "function not found" error at execute time.
func resolveFunctionRef(ctx context.Context, lookup FunctionCallGraphLookup, ontologyRID, ref string) (*Function, error) {
	if ref == "" {
		return nil, nil
	}
	if name, version, ok := splitFunctionRef(ref); ok {
		fn, err := lookup.GetFunctionByNameVersion(ctx, ontologyRID, name, version)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return fn, err
	}
	if strings.HasPrefix(ref, "ri.") {
		fn, err := lookup.GetFunction(ctx, ref)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return fn, err
	}
	fn, err := lookup.GetFunctionByName(ctx, ontologyRID, ref)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return fn, err
}

// canonicalRefForFunction is the identity DetectCallCycle uses on the visited
// stack. We collapse to `name@version` so a callee referenced by RID and the
// same callee referenced by name still register as the same node — without
// this, A(rid)→B(name)→A(name) would slip through.
func canonicalRefForFunction(fn *Function) string {
	if fn == nil {
		return ""
	}
	if fn.Name != "" {
		return fn.Name + "@" + fn.NormalisedVersion()
	}
	return fn.RID
}

// DetectCallCycle walks the static call graph rooted at the supplied function
// and returns a *FunctionCallCycleError when the graph re-enters a node
// already on the active DFS path. The function being published is treated as
// already-resolved: callers pass in the in-flight Function struct so the scan
// works for both create and update paths (where the row may not yet be in the
// repository or its source code may differ from the persisted copy).
//
// Behaviour:
//   - The supplied `root` function's source is scanned via ExtractCallTargets.
//     Each callee ref is resolved through the lookup and recursively walked.
//   - Cycle detection uses canonicalRefForFunction so name + RID + name@version
//     all collapse to the same identity.
//   - Unknown / unresolved callees are skipped (the runtime surfaces them at
//     execute time as "function not found").
//   - Already-fully-walked subtrees are memoised to keep the DFS O(N+E) over
//     the reachable graph.
func DetectCallCycle(ctx context.Context, lookup FunctionCallGraphLookup, ontologyRID string, root *Function) error {
	if root == nil {
		return nil
	}
	visited := make(map[string]bool) // identity → on current DFS stack
	walked := make(map[string]bool)  // identity → fully drained
	rootIdentity := canonicalRefForFunction(root)
	stack := []string{rootIdentity}
	visited[rootIdentity] = true

	var dfs func(fn *Function) error
	dfs = func(fn *Function) error {
		targets := ExtractCallTargets(fn.SourceCode)
		for _, ref := range targets {
			callee, err := resolveFunctionRef(ctx, lookup, ontologyRID, ref)
			if err != nil {
				return fmt.Errorf("resolve callee %q: %w", ref, err)
			}
			calleeIdentity := canonicalRefForFunction(callee)
			if calleeIdentity == "" {
				// Fall back to the raw ref so a self-referential dangling
				// callee (the function references itself by a name that's
				// not yet in the repo) still flags as a cycle.
				if ref == rootIdentity {
					calleeIdentity = ref
				} else {
					continue
				}
			}
			if visited[calleeIdentity] {
				cycle := append([]string{}, stack...)
				cycle = append(cycle, calleeIdentity)
				return &FunctionCallCycleError{
					StartRef: rootIdentity,
					Cycle:    cycle,
				}
			}
			if walked[calleeIdentity] {
				continue
			}
			if callee == nil {
				continue
			}
			visited[calleeIdentity] = true
			stack = append(stack, calleeIdentity)
			if err := dfs(callee); err != nil {
				return err
			}
			stack = stack[:len(stack)-1]
			visited[calleeIdentity] = false
			walked[calleeIdentity] = true
		}
		return nil
	}
	return dfs(root)
}
