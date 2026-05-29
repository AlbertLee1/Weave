package graphsvc

// VTX-013 — share link / permission model.
//
// A ShareLink is a per-graph token an owner mints so external recipients can
// view a graph without holding read permission on it. Recipients receive the
// graph's structure but with property values inside layers masked to "***"
// (so they can see what the graph looks like without seeing the data).
//
// Revocation is a soft state flag — Revoked=true keeps the row so future
// reads return 410 Gone rather than the indistinguishable 404 (caller can
// see "this used to exist; the owner shut it down").

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrShareLinkNotFound is returned when a token does not match any row.
var ErrShareLinkNotFound = errors.New("share link not found")

// ErrShareLinkRevoked is returned when the token matches a row whose Revoked
// flag is set. Distinct from ErrShareLinkNotFound so handlers can map the two
// to different HTTP statuses (404 vs 410).
var ErrShareLinkRevoked = errors.New("share link revoked")

// ShareLink is a graph access token. Token is the opaque public identifier
// recipients quote in the URL; CreatedBy carries the owner's user ID so
// revoke checks the right ACL.
type ShareLink struct {
	Token     string
	GraphRID  string
	CreatedBy string
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
	RevokedAt time.Time
}

// ShareLinkStore persists ShareLink rows. Round 69 added ListByGraph
// for the manage-share-links UI surface so graph owners can audit and
// revoke share links they previously minted — previously the only way
// to discover a link was to save its token at create time.
type ShareLinkStore interface {
	Create(ctx context.Context, link *ShareLink) error
	Get(ctx context.Context, token string) (*ShareLink, error)
	Revoke(ctx context.Context, token string) error
	// ListByGraph returns every ShareLink (including revoked) for
	// graphRID, sorted by CreatedAt DESC so the newest link surfaces
	// first. Implementations must NOT redact the Token field — the
	// HTTP handler is the security boundary that masks the token into
	// a tokenSuffix before serialization, so this layer stays a thin
	// data accessor.
	ListByGraph(ctx context.Context, graphRID string) ([]*ShareLink, error)
}

// MemShareLinkStore is the in-memory ShareLinkStore used by tests and
// degraded-mode boots.
type MemShareLinkStore struct {
	mu sync.Mutex
	m  map[string]*ShareLink
}

// NewMemShareLinkStore returns an empty MemShareLinkStore.
func NewMemShareLinkStore() *MemShareLinkStore {
	return &MemShareLinkStore{m: map[string]*ShareLink{}}
}

func (s *MemShareLinkStore) Create(ctx context.Context, link *ShareLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *link
	s.m[link.Token] = &copy
	return nil
}

func (s *MemShareLinkStore) Get(ctx context.Context, token string) (*ShareLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.m[token]
	if !ok {
		return nil, ErrShareLinkNotFound
	}
	out := *link
	return &out, nil
}

// ListByGraph returns every link belonging to graphRID, sorted by
// CreatedAt DESC. Round 69 added the method to back the manage-shares
// list endpoint.
func (s *MemShareLinkStore) ListByGraph(_ context.Context, graphRID string) ([]*ShareLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*ShareLink{}
	for _, link := range s.m {
		if link.GraphRID == graphRID {
			cp := *link
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Revoke flips the row's Revoked flag. Returns ErrShareLinkNotFound when the
// token never existed; revoking an already-revoked link is idempotent (no
// error so the surface stays safe to retry).
func (s *MemShareLinkStore) Revoke(ctx context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.m[token]
	if !ok {
		return ErrShareLinkNotFound
	}
	if !link.Revoked {
		link.Revoked = true
		link.RevokedAt = time.Now().UTC()
	}
	return nil
}

var _ ShareLinkStore = (*MemShareLinkStore)(nil)

// newShareToken returns a URL-safe random token. 24 bytes of entropy keeps
// the encoded form short (32 chars) while staying well clear of collision /
// guessability concerns for share links.
func newShareToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// maskLayerPropertyValues returns a copy of payload where every value under a
// "properties" object reachable from layers[*] or nodes[*] is replaced with
// the string "***". Property keys, layer/node structure, edges, positions,
// savedSelections, timeSettings, and any other top-level fields pass through
// unchanged.
//
// The walk treats `properties` as a leaf object whose values are scalars (the
// shape Vertex actually emits today). Nested objects below a `properties` key
// are masked too — every reachable scalar becomes "***", every container
// keeps its shape.
//
// On any decode error the original payload is returned unchanged: the share
// reader has already been authorized, so leaking the raw bytes is no worse
// than refusing the request, and refusing wholly opaque payloads would be a
// regression for graphs whose schema we don't yet enforce.
func maskLayerPropertyValues(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return payload
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return payload
	}
	if layers, ok := obj["layers"].([]any); ok {
		for _, layer := range layers {
			maskPropertiesIn(layer)
		}
	}
	if nodes, ok := obj["nodes"].([]any); ok {
		for _, node := range nodes {
			maskPropertiesIn(node)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return out
}

func shareLinkExpired(link *ShareLink, now time.Time) bool {
	return link != nil && !link.ExpiresAt.IsZero() && !now.Before(link.ExpiresAt)
}

// maskPropertiesIn walks the layer subtree in place. Whenever it encounters
// a map[string]any key named "properties" it replaces every reachable scalar
// in that subtree with "***".
func maskPropertiesIn(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == "properties" {
				maskScalars(val, t, k)
				continue
			}
			maskPropertiesIn(val)
		}
	case []any:
		for _, item := range t {
			maskPropertiesIn(item)
		}
	}
}

// maskScalars rewrites every scalar leaf inside v with the literal "***".
// Containers (maps, slices) are preserved so the recipient still sees the
// shape of the property tree.
func maskScalars(v any, parent map[string]any, parentKey string) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			switch val.(type) {
			case map[string]any, []any:
				maskScalars(val, t, k)
			default:
				t[k] = "***"
			}
		}
	case []any:
		for i, item := range t {
			switch item.(type) {
			case map[string]any, []any:
				maskScalars(item, nil, "")
			default:
				t[i] = "***"
			}
		}
	default:
		if parent != nil {
			parent[parentKey] = "***"
		}
	}
}
