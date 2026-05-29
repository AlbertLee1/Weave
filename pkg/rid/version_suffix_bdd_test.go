package rid

import (
	"strings"
	"testing"
)

// TestBDD_RIDVersionSuffix covers US-070 step 1 — Foundry-parity
// RID version suffix support. Round 91 PIVOTS away from the 8-of-8
// batch-symmetry recipe to the next unaddressed gap category
// (Gap-T4 from PRD-V2). This round lands ONLY the parser /
// round-trip / equality semantics; snapshot-load-by-version
// (US-070 acceptance #2-3) is a separate, larger lift that follows
// once consumers can carry version data through code paths.
//
// Wire shape:
//
//	ri.{service}.{realm}.{resourceType}.{uuid}        -> Version=""
//	ri.{service}.{realm}.{resourceType}.{uuid}@v3     -> Version="3"
//	ri.{service}.{realm}.{resourceType}.{uuid}@v123   -> Version="123"
//
// Invariants:
//   - Backwards-compatible: every existing un-versioned RID still
//     parses, with Version=="".
//   - Additive: adding @vN suffix to a valid RID parses to the same
//     {service, realm, resourceType, ID} with Version=="N".
//   - String() round-trips: Parse(s).String() == s for any valid s
//     (with or without the suffix).
//   - Equal() compares Version too: ri.X@v1 != ri.X@v2 != ri.X.
//   - Malformed suffixes rejected: @, @v, @vabc, @v0123 (leading
//     zero), multiple @ — each errors at parse time so we don't
//     persist garbage.
func TestBDD_RIDVersionSuffix(t *testing.T) {
	const baseUUID = "550e8400-e29b-41d4-a716-446655440000"
	base := "ri.ontology.main.object-type." + baseUUID

	t.Run("Un-versioned RID parses with empty Version", func(t *testing.T) {
		r, err := Parse(base)
		if err != nil {
			t.Fatalf("Parse(%q) err=%v, want nil", base, err)
		}
		if r.Version != "" {
			t.Errorf("Version=%q, want empty for un-versioned RID", r.Version)
		}
		if r.ID != baseUUID {
			t.Errorf("ID=%q, want %q", r.ID, baseUUID)
		}
	})

	t.Run("@vN suffix populates Version", func(t *testing.T) {
		cases := map[string]string{
			base + "@v3":   "3",
			base + "@v1":   "1",
			base + "@v123": "123",
		}
		for in, wantVer := range cases {
			r, err := Parse(in)
			if err != nil {
				t.Errorf("Parse(%q) err=%v, want nil", in, err)
				continue
			}
			if r.Version != wantVer {
				t.Errorf("Parse(%q).Version = %q, want %q", in, r.Version, wantVer)
			}
			if r.ID != baseUUID {
				t.Errorf("Parse(%q).ID = %q, want %q (suffix must not bleed into ID)",
					in, r.ID, baseUUID)
			}
		}
	})

	t.Run("String round-trips with and without Version", func(t *testing.T) {
		cases := []string{base, base + "@v1", base + "@v42"}
		for _, in := range cases {
			r, err := Parse(in)
			if err != nil {
				t.Fatalf("Parse(%q) err=%v", in, err)
			}
			got := r.String()
			if got != in {
				t.Errorf("Parse->String round-trip broken: %q -> %q", in, got)
			}
		}
	})

	t.Run("Equal compares Version too", func(t *testing.T) {
		a, _ := Parse(base + "@v3")
		b, _ := Parse(base + "@v3")
		c, _ := Parse(base + "@v4")
		d, _ := Parse(base)
		if !a.Equal(b) {
			t.Errorf("identical @v3 RIDs should be Equal")
		}
		if a.Equal(c) {
			t.Errorf("@v3 != @v4 (different versions must not be Equal)")
		}
		if a.Equal(d) {
			t.Errorf("@v3 must not Equal un-versioned (different semantics — explicit vs latest)")
		}
	})

	t.Run("Malformed @v suffix rejected", func(t *testing.T) {
		malformed := []string{
			base + "@",      // bare @
			base + "@v",     // @v with no version digits
			base + "@vabc",  // non-numeric version
			base + "@v0",    // zero version not valid (versions start at 1)
			base + "@v0123", // leading-zero version (canonical form forbidden)
			base + "@v3@v4", // double suffix
			base + "@x3",    // wrong prefix (must be @v)
		}
		for _, in := range malformed {
			_, err := Parse(in)
			if err == nil {
				t.Errorf("Parse(%q) succeeded, want error", in)
			}
		}
	})

	t.Run("Suffix on invalid base RID still errors on base", func(t *testing.T) {
		// A bad base shouldn't be hidden by a syntactically-ok suffix —
		// callers should see the base error first.
		_, err := Parse("not.a.rid.format@v3")
		if err == nil {
			t.Errorf("Parse with invalid base should error even when @v3 looks valid")
		}
		if !strings.Contains(err.Error(), "invalid RID") {
			t.Errorf("error %q should mention invalid RID, not version-suffix-specific", err)
		}
	})
}
