// Package sharelinks implements the "Get quick share link" feature for
// Vertex graphs. It owns three concerns:
//
//   - GenerateToken: cryptographically random URL-safe token used as the
//     primary key of a share_links row.
//   - EvaluateAccess: pure decision function that turns a Link + AccessRequest
//     into one of {Gone, ReadOnly, Masked}.
//   - MaskGraphPayload: redacts node property values for cross-org viewers,
//     leaving structural fields (id, name) intact so they still see the
//     graph layout, just not the data.
//
// Persistence (share_links table) and HTTP handler binding live elsewhere;
// this package keeps the rules testable in isolation.
package sharelinks

import (
	"crypto/rand"
	"encoding/base64"
	"time"
)

// Decision enumerates the access outcomes.
type Decision int

const (
	// DecisionGone covers revoked + expired links (HTTP 410).
	DecisionGone Decision = iota
	// DecisionReadOnly: same-org member, full read access (HTTP 200, mode=readonly).
	DecisionReadOnly
	// DecisionMasked: cross-org or unauthenticated viewer, see structure
	// but property values are redacted to "•••".
	DecisionMasked
)

// Link is the persisted share-link state.
type Link struct {
	Token     string
	GraphRID  string
	OwnerOrg  string
	ExpiresAt time.Time
	Revoked   bool
}

// AccessRequest is the incoming request context.
type AccessRequest struct {
	Now       time.Time
	ViewerOrg string
}

// AccessResult is the EvaluateAccess output.
type AccessResult struct {
	Decision Decision
}

// GenerateToken returns a URL-safe random token (24 bytes → 32 char
// base64-url string without padding).
func GenerateToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// EvaluateAccess: revoked / expired → Gone; same-org → ReadOnly;
// everything else (including unauthenticated viewers) → Masked.
func EvaluateAccess(link Link, req AccessRequest) AccessResult {
	if link.Revoked || (!link.ExpiresAt.IsZero() && req.Now.After(link.ExpiresAt)) {
		return AccessResult{Decision: DecisionGone}
	}
	if req.ViewerOrg != "" && req.ViewerOrg == link.OwnerOrg {
		return AccessResult{Decision: DecisionReadOnly}
	}
	return AccessResult{Decision: DecisionMasked}
}

const maskedValue = "•••"

// MaskGraphPayload walks a graph JSON-like payload and replaces every
// property value on every node with "•••". Structural fields preserved.
// Returns a NEW payload — the input is not mutated.
func MaskGraphPayload(graph map[string]any) map[string]any {
	out := make(map[string]any, len(graph))
	for k, v := range graph {
		out[k] = v
	}
	rawNodes, ok := out["nodes"].([]map[string]any)
	if !ok {
		return out
	}
	maskedNodes := make([]map[string]any, 0, len(rawNodes))
	for _, n := range rawNodes {
		clone := make(map[string]any, len(n))
		for k, v := range n {
			if k == "properties" {
				if props, ok := v.(map[string]any); ok {
					mp := make(map[string]any, len(props))
					for pk := range props {
						mp[pk] = maskedValue
					}
					clone[k] = mp
					continue
				}
			}
			clone[k] = v
		}
		maskedNodes = append(maskedNodes, clone)
	}
	out["nodes"] = maskedNodes
	return out
}
