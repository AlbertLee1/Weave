package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// TestJWTMarkings_SignVerifyRoundTrip verifies US-053: user markings are
// carried end-to-end through the JWT signer. A token minted with a set of
// markings must round-trip through Verify and return the same slice on
// claims.Weave.Markings. This is the transport layer for Foundry-style
// mandatory access control: downstream policy evaluation (US-054
// middleware, pkg/auth.EvaluateMarkings, row-level policy engine) reads
// the verified claim and feeds it into the marking subset check.
func TestJWTMarkings_SignVerifyRoundTrip(t *testing.T) {
	s := newTestSigner(t)

	want := []string{"PUBLIC", "INTERNAL", "PII"}
	tok, err := s.Sign(SignInput{
		UserID:   "user:alice@example.com",
		Email:    "alice@example.com",
		Name:     "Alice",
		Markings: want,
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	claims, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	got := claims.Weave.Markings
	if len(got) != len(want) {
		t.Fatalf("markings length: got %d want %d (%v)", len(got), len(want), got)
	}
	for i, m := range want {
		if got[i] != m {
			t.Errorf("markings[%d]: got %q want %q", i, got[i], m)
		}
	}
}

// TestJWTMarkings_EmptyMarkingsOmitted verifies that users with no
// markings produce a compact token: the "markings" field is omitted
// from the "weave" claim JSON via the omitempty tag rather than
// serialised as null or []. This keeps token size down for the common
// unauthenticated / unprivileged case and matches how Roles and
// OntologyRoles already behave.
func TestJWTMarkings_EmptyMarkingsOmitted(t *testing.T) {
	s := newTestSigner(t)

	tok, err := s.Sign(SignInput{UserID: "user:bob"})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("unexpected token shape: %d parts", len(parts))
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var decoded struct {
		Weave map[string]json.RawMessage `json:"weave"`
	}
	if err := json.Unmarshal(payloadBytes, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := decoded.Weave["markings"]; ok {
		t.Errorf("expected omitempty to drop markings from %v", decoded.Weave)
	}

	claims, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(claims.Weave.Markings) != 0 {
		t.Errorf("decoded markings: got %v want empty", claims.Weave.Markings)
	}
}

// TestJWTMarkings_PreservesOrderAndDuplicates verifies that marking
// encoding is transparent: the signer does not deduplicate or sort. The
// downstream EvaluateMarkings is duplicate-tolerant, so preserving the
// wire shape keeps signer behaviour straightforward and auditable.
func TestJWTMarkings_PreservesOrderAndDuplicates(t *testing.T) {
	s := newTestSigner(t)
	want := []string{"SECRET", "PUBLIC", "PUBLIC", "INTERNAL"}
	tok, err := s.Sign(SignInput{UserID: "user:carol", Markings: want})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	claims, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	got := claims.Weave.Markings
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("markings[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}
