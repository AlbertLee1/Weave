package rid

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// --- US-011 fixture helpers ---------------------------------------------------

// Two known-good lowercase canonical UUIDs we don't want to randomize across
// runs — keeps subtests deterministic.
const (
	v4Hex = "550e8400-e29b-41d4-a716-446655440000" // version digit (index 14) = '4'
	v7Hex = "017f22e2-79b0-7cc3-98c4-dc0c0c07398f" // version digit (index 14) = '7'
)

func mustParse(t *testing.T, s string) *RID {
	t.Helper()
	r, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", s, err)
	}
	return r
}

// --- TestRID_FormatValidation -------------------------------------------------

func TestRID_FormatValidation(t *testing.T) {
	good := "ri.ontology.main.object-type." + v4Hex

	t.Run("CanonicalRoundtripsThroughParseAndString", func(t *testing.T) {
		r := mustParse(t, good)
		if got := r.String(); got != good {
			t.Fatalf("String() = %q, want %q", got, good)
		}
		if r.Service != "ontology" || r.Realm != "main" ||
			r.ResourceType != "object-type" || r.ID != v4Hex {
			t.Fatalf("parsed fields wrong: %+v", r)
		}
	})

	t.Run("MissingRIPrefixRejected", func(t *testing.T) {
		_, err := Parse("xy.ontology.main.foo." + v4Hex)
		if err == nil {
			t.Fatal("expected error for non-ri prefix, got nil")
		}
	})

	t.Run("EmptyStringRejected", func(t *testing.T) {
		_, err := Parse("")
		if err == nil {
			t.Fatal("expected error for empty input, got nil")
		}
	})

	t.Run("TooFewSegmentsRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology.main.foo")
		if err == nil {
			t.Fatal("expected error for 4 segments, got nil")
		}
	})

	t.Run("ExtraSegmentsCollapseIntoUUIDAndFailUUIDValidation", func(t *testing.T) {
		// SplitN(n=5) keeps the tail in parts[4]; if the tail contains a dot
		// it should fail UUID pattern validation rather than silently truncate.
		_, err := Parse("ri.ontology.main.foo.bar." + v4Hex)
		if err == nil {
			t.Fatal("expected error for 6 segments, got nil")
		}
	})

	t.Run("UppercaseUUIDRejected", func(t *testing.T) {
		// Canonical UUIDs in this codebase are always lowercase (uuid.New().String()).
		// Reject uppercase to keep persisted RIDs byte-equal across read paths.
		_, err := Parse("ri.ontology.main.foo." + strings.ToUpper(v4Hex))
		if err == nil {
			t.Fatal("expected error for uppercase UUID, got nil")
		}
	})

	t.Run("NonUUIDIDRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology.main.foo.not-a-uuid")
		if err == nil {
			t.Fatal("expected error for non-UUID id, got nil")
		}
	})

	t.Run("UUIDMissingDashesRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology.main.foo.550e8400e29b41d4a716446655440000")
		if err == nil {
			t.Fatal("expected error for unhyphenated UUID, got nil")
		}
	})
}

// --- TestRID_RejectsControlAndEmptyFields ------------------------------------

func TestRID_RejectsControlAndEmptyFields(t *testing.T) {
	t.Run("EmptyServiceRejected", func(t *testing.T) {
		_, err := Parse("ri..main.foo." + v4Hex)
		if err == nil {
			t.Fatal("expected error for empty service, got nil")
		}
	})

	t.Run("EmptyRealmRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology..foo." + v4Hex)
		if err == nil {
			t.Fatal("expected error for empty realm, got nil")
		}
	})

	t.Run("EmptyResourceTypeRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology.main.." + v4Hex)
		if err == nil {
			t.Fatal("expected error for empty resource type, got nil")
		}
	})

	t.Run("EmptyIDRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology.main.foo.")
		if err == nil {
			t.Fatal("expected error for empty id, got nil")
		}
	})

	t.Run("CarriageReturnInRealmRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology.ma\rin.foo." + v4Hex)
		if err == nil {
			t.Fatal("expected error for CR in realm, got nil")
		}
	})

	t.Run("NewlineInServiceRejected", func(t *testing.T) {
		_, err := Parse("ri.onto\nlogy.main.foo." + v4Hex)
		if err == nil {
			t.Fatal("expected error for LF in service, got nil")
		}
	})

	t.Run("TabInResourceTypeRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology.main.fo\to." + v4Hex)
		if err == nil {
			t.Fatal("expected error for tab in resource type, got nil")
		}
	})

	t.Run("NullByteInServiceRejected", func(t *testing.T) {
		_, err := Parse("ri.on\x00tology.main.foo." + v4Hex)
		if err == nil {
			t.Fatal("expected error for NUL in service, got nil")
		}
	})

	t.Run("DELControlByteRejected", func(t *testing.T) {
		_, err := Parse("ri.ontology.ma\x7fin.foo." + v4Hex)
		if err == nil {
			t.Fatal("expected error for DEL in realm, got nil")
		}
	})
}

// --- TestRID_UUIDVersionDistinction ------------------------------------------

func TestRID_UUIDVersionDistinction(t *testing.T) {
	t.Run("V4Detected", func(t *testing.T) {
		r := mustParse(t, "ri.ontology.main.object-type."+v4Hex)
		if !r.IsUUIDv4() {
			t.Fatalf("IsUUIDv4() = false for known v4 %q", v4Hex)
		}
		if r.IsUUIDv7() {
			t.Fatalf("IsUUIDv7() = true for known v4 %q", v4Hex)
		}
		v, err := r.UUIDVersion()
		if err != nil || v != 4 {
			t.Fatalf("UUIDVersion() = (%d, %v), want (4, nil)", v, err)
		}
	})

	t.Run("V7Detected", func(t *testing.T) {
		r := mustParse(t, "ri.ontology.main.object-type."+v7Hex)
		if !r.IsUUIDv7() {
			t.Fatalf("IsUUIDv7() = false for known v7 %q", v7Hex)
		}
		if r.IsUUIDv4() {
			t.Fatalf("IsUUIDv4() = true for known v7 %q", v7Hex)
		}
		v, err := r.UUIDVersion()
		if err != nil || v != 7 {
			t.Fatalf("UUIDVersion() = (%d, %v), want (7, nil)", v, err)
		}
	})

	t.Run("NewGeneratesV4", func(t *testing.T) {
		// rid.New uses google/uuid.New() which is documented as v4 (random).
		for i := 0; i < 4; i++ {
			r := mustParse(t, New("ontology", "main", "object-type"))
			if !r.IsUUIDv4() {
				t.Fatalf("rid.New produced non-v4: %s", r)
			}
		}
	})

	t.Run("V7FromGoogleUUIDDetected", func(t *testing.T) {
		// Generate a fresh v7 via google/uuid to make sure the helper isn't
		// coupled to our v7Hex literal.
		u, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("uuid.NewV7: %v", err)
		}
		r := mustParse(t, "ri.ontology.main.object-type."+u.String())
		if !r.IsUUIDv7() {
			t.Fatalf("IsUUIDv7() = false for fresh v7 %q", u.String())
		}
	})

	t.Run("NewActionTypeRIDIsV4", func(t *testing.T) {
		// Sanity: every typed constructor in rid.go funnels through New.
		r := mustParse(t, NewActionTypeRID())
		if !r.IsUUIDv4() {
			t.Fatalf("NewActionTypeRID() not v4: %s", r)
		}
	})
}

// --- TestRID_EqualityAndHash --------------------------------------------------

func TestRID_EqualityAndHash(t *testing.T) {
	good := "ri.ontology.main.object-type." + v4Hex

	t.Run("SameFieldsEqualAndShareHash", func(t *testing.T) {
		a := mustParse(t, good)
		b := mustParse(t, good)
		if !a.Equal(b) {
			t.Fatalf("Equal() returned false for identical RIDs: %+v vs %+v", a, b)
		}
		if a.Hash() != b.Hash() {
			t.Fatalf("Hash mismatch for identical RIDs: %s vs %s", a.Hash(), b.Hash())
		}
	})

	t.Run("DifferentServiceUnequalAndDistinctHash", func(t *testing.T) {
		a := mustParse(t, "ri.ontology.main.object-type."+v4Hex)
		b := mustParse(t, "ri.foundry.main.object-type."+v4Hex)
		if a.Equal(b) {
			t.Fatalf("expected !Equal across services")
		}
		if a.Hash() == b.Hash() {
			t.Fatalf("expected distinct hashes across services, both = %s", a.Hash())
		}
	})

	t.Run("DifferentRealmUnequal", func(t *testing.T) {
		a := mustParse(t, "ri.ontology.main.object-type."+v4Hex)
		b := mustParse(t, "ri.ontology.staging.object-type."+v4Hex)
		if a.Equal(b) {
			t.Fatal("expected !Equal across realms")
		}
	})

	t.Run("DifferentResourceTypeUnequal", func(t *testing.T) {
		a := mustParse(t, "ri.ontology.main.object-type."+v4Hex)
		b := mustParse(t, "ri.ontology.main.action-type."+v4Hex)
		if a.Equal(b) {
			t.Fatal("expected !Equal across resource types")
		}
	})

	t.Run("DifferentIDUnequal", func(t *testing.T) {
		a := mustParse(t, "ri.ontology.main.object-type."+v4Hex)
		b := mustParse(t, "ri.ontology.main.object-type."+v7Hex)
		if a.Equal(b) {
			t.Fatal("expected !Equal across ids")
		}
	})

	t.Run("NilSafeEqual", func(t *testing.T) {
		var nilR *RID
		other := mustParse(t, good)
		if nilR.Equal(other) {
			t.Fatal("nil.Equal(other) should be false")
		}
		if other.Equal(nilR) {
			t.Fatal("other.Equal(nil) should be false")
		}
		if !nilR.Equal(nil) {
			t.Fatal("nil.Equal(nil) should be true")
		}
	})

	t.Run("HashStableAcrossCalls", func(t *testing.T) {
		r := mustParse(t, good)
		first := r.Hash()
		for i := 0; i < 5; i++ {
			if got := r.Hash(); got != first {
				t.Fatalf("hash drift on call %d: %s vs %s", i, got, first)
			}
		}
	})

	t.Run("HashEnablesMapKeyOnPointerValues", func(t *testing.T) {
		// Two distinct *RID with identical fields share Hash() and can dedupe.
		m := make(map[string]int)
		for i := 0; i < 3; i++ {
			m[mustParse(t, good).Hash()]++
		}
		if len(m) != 1 {
			t.Fatalf("expected 1 hash bucket, got %d: %#v", len(m), m)
		}
		if m[mustParse(t, good).Hash()] != 3 {
			t.Fatalf("expected 3 hits in the single bucket, got %d", m[mustParse(t, good).Hash()])
		}
	})

	t.Run("HashCollisionLowAcrossManyRIDs", func(t *testing.T) {
		seen := make(map[string]string, 100)
		for i := 0; i < 100; i++ {
			r := mustParse(t, New("ontology", "main", "object-type"))
			if prior, ok := seen[r.Hash()]; ok {
				t.Fatalf("hash collision: %s vs %s share hash %s", prior, r, r.Hash())
			}
			seen[r.Hash()] = r.String()
		}
	})

	t.Run("StringMatchesOriginalInput", func(t *testing.T) {
		for _, in := range []string{
			"ri.ontology.main.object-type." + v4Hex,
			"ri.ontology.staging.action-type." + v7Hex,
			"ri.ontology.main.value-type." + v4Hex,
		} {
			r := mustParse(t, in)
			if r.String() != in {
				t.Errorf("String(%+v) = %q, want %q", r, r.String(), in)
			}
		}
	})

	t.Run("NilReceiverStringEmpty", func(t *testing.T) {
		var nilR *RID
		if nilR.String() != "" {
			t.Fatalf("(*RID)(nil).String() = %q, want \"\"", nilR.String())
		}
		if nilR.Hash() != "" {
			t.Fatalf("(*RID)(nil).Hash() = %q, want \"\"", nilR.Hash())
		}
	})
}

// --- TestRID_ParseErrorWrappingAndSentinels ----------------------------------

func TestRID_ParseErrorContainsInput(t *testing.T) {
	// We don't need exotic error wrapping, but callers like realmFromRID
	// rely on Parse returning *some* error for malformed input; this regression
	// pins that error messages include the offending input substring for ops.
	cases := []struct {
		name string
		in   string
	}{
		{"non_ri_prefix", "garbage"},
		{"empty_realm", "ri.ontology..foo." + v4Hex},
		{"non_uuid", "ri.ontology.main.foo.bar"},
		{"carriage_return", "ri.onto\rlogy.main.foo." + v4Hex},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.in)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			// Use %q-formatted form so embedded control chars survive.
			wantSub := fmt.Sprintf("%q", tc.in)
			if !strings.Contains(err.Error(), wantSub) {
				t.Fatalf("error %q does not contain offending input %s", err.Error(), wantSub)
			}
		})
	}
}

// --- TestRID_AllTypedConstructorsRoundtrip ------------------------------------

func TestRID_AllTypedConstructorsRoundtrip(t *testing.T) {
	// Every typed constructor in rid.go funnels through `New("ontology", "main", "<type>")`.
	// Pin that each one emits a parseable, v4, IsRID-true canonical string.
	cases := []struct {
		name      string
		ctor      func() string
		wantType  string
		wantRealm string
	}{
		{"NewOntologyRID", NewOntologyRID, "ontology", "main"},
		{"NewObjectTypeRID", NewObjectTypeRID, "object-type", "main"},
		{"NewPropertyRID", NewPropertyRID, "property", "main"},
		{"NewLinkTypeRID", NewLinkTypeRID, "link-type", "main"},
		{"NewObjectRID", NewObjectRID, "object", "main"},
		{"NewActionTypeRID", NewActionTypeRID, "action-type", "main"},
		{"NewInterfaceRID", NewInterfaceRID, "interface", "main"},
		{"NewSharedPropertyRID", NewSharedPropertyRID, "shared-property", "main"},
		{"NewTypeGroupRID", NewTypeGroupRID, "type-group", "main"},
		{"NewValueTypeRID", NewValueTypeRID, "value-type", "main"},
		{"NewSecurityPolicyRID", NewSecurityPolicyRID, "security-policy", "main"},
		{"NewDatasourceBindingRID", NewDatasourceBindingRID, "datasource-binding", "main"},
		{"NewFunctionRID", NewFunctionRID, "function", "main"},
		{"NewQueryTypeRID", NewQueryTypeRID, "query-type", "main"},
		{"NewBranchRID", NewBranchRID, "branch", "main"},
		{"NewProposalRID", NewProposalRID, "proposal", "main"},
		{"NewProposalReviewRID", NewProposalReviewRID, "proposal-review", "main"},
		{"NewAutomationRuleRID", NewAutomationRuleRID, "automation-rule", "main"},
		{"NewAutomationExecutionRID", NewAutomationExecutionRID, "automation-execution", "main"},
		{"NewNotificationRID", NewNotificationRID, "notification", "main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.ctor()
			if !IsRID(s) {
				t.Fatalf("IsRID(%q) = false", s)
			}
			r, err := Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q): %v", s, err)
			}
			if r.Service != "ontology" || r.Realm != tc.wantRealm || r.ResourceType != tc.wantType {
				t.Fatalf("%s parsed as %+v, want service=ontology realm=%s type=%s",
					tc.name, r, tc.wantRealm, tc.wantType)
			}
			if !r.IsUUIDv4() {
				t.Fatalf("%s id %q is not v4", tc.name, r.ID)
			}
		})
	}

	t.Run("IsRID_NegativeCases", func(t *testing.T) {
		for _, s := range []string{"", "garbage", "rix.foo.bar.baz.qux", "  ri.foo.bar.baz.qux"} {
			if IsRID(s) {
				t.Errorf("IsRID(%q) = true, want false", s)
			}
		}
	})
}

// --- TestRID_PreservesExistingPermissiveCallerContract -----------------------

func TestRID_PermissiveCallerStillFallsBackToDefault(t *testing.T) {
	// Existing realmFromRID in pkg/oms/handlers_function.go does:
	//   parsed, err := rid.Parse(r); if err != nil || parsed.Realm == "" { return "main" }
	// This subtest pins the contract from rid's side: even after tightening,
	// Parse must return a non-nil error (never panic) for the legacy "loose"
	// inputs that quota enforcement explicitly handles.
	legacyInputs := []string{
		"",
		"loose-id-without-prefix",
		"ri.a.b",
		"ri..main.foo." + v4Hex,
	}
	for _, in := range legacyInputs {
		r, err := Parse(in)
		if err == nil {
			t.Errorf("Parse(%q) should error for legacy permissive input but returned %+v", in, r)
		}
		// Must not be a typed nil-ptr panic when callers ignore the error.
		_ = errors.Is(err, err)
	}
}
