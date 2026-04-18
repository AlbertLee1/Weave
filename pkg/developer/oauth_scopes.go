package developer

import "errors"

// Canonical OAuth scope catalogue.
//
// The Weave platform publishes three coarse scopes for new applications.
// Existing custom scopes (e.g. "read:objects", "admin:ontology") continue
// to work — the catalogue is additive, not exclusive — but the canonical
// trio is the recommended starting point and the only set the discovery
// endpoint advertises.
const (
	// ScopeRead grants read-only access to ontology metadata and objects.
	ScopeRead = "read"

	// ScopeWrite grants read + write access (create / update / delete
	// objects via Action execution).
	ScopeWrite = "write"

	// ScopeAdmin grants administrative access (manage ontologies, users,
	// applications, system settings).
	ScopeAdmin = "admin"
)

// ErrScopeNotGranted is returned by NarrowScopes when the caller requests
// a scope that is not present in the original grant. Refresh / token
// endpoints surface this as `invalid_scope` per RFC 6749 §5.2.
var ErrScopeNotGranted = errors.New("requested scope was not granted")

// knownScopeSet is the membership set behind IsKnownScope. A separate map
// avoids an O(n) scan on every check (called per-request from the consent
// surface).
var knownScopeSet = map[string]struct{}{
	ScopeRead:  {},
	ScopeWrite: {},
	ScopeAdmin: {},
}

// KnownScopes returns the canonical scope catalogue in catalogue order
// (read → write → admin). Each call returns a fresh slice so callers can
// mutate without risk of cross-call corruption.
func KnownScopes() []string {
	return []string{ScopeRead, ScopeWrite, ScopeAdmin}
}

// IsKnownScope reports whether a scope string is one of the canonical
// entries. Case-sensitive — scope strings are lower-case by convention
// and the catalogue is the source of truth.
func IsKnownScope(s string) bool {
	_, ok := knownScopeSet[s]
	return ok
}

// NarrowScopes returns the subset of `requested` that intersects with
// `granted`, preserving the order of `requested` and collapsing
// duplicates. When `requested` is empty (no `scope` form param), the
// caller is keeping the original grant — return a copy of `granted`.
//
// When `requested` contains any scope absent from `granted`, the function
// returns ErrScopeNotGranted. Refresh / token-exchange paths cannot
// elevate a token's scope; the only legal scope move is narrowing.
func NarrowScopes(granted, requested []string) ([]string, error) {
	if len(requested) == 0 {
		out := make([]string, len(granted))
		copy(out, granted)
		return out, nil
	}
	allow := make(map[string]struct{}, len(granted))
	for _, s := range granted {
		allow[s] = struct{}{}
	}
	seen := make(map[string]struct{}, len(requested))
	out := make([]string, 0, len(requested))
	for _, s := range requested {
		if _, ok := allow[s]; !ok {
			return nil, ErrScopeNotGranted
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}
