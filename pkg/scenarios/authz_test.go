package scenarios_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/scenarios"
)

// TestAuthorizeRead covers the owner / admin / foreign-user matrix for read
// access to a Scenario. The contract: only the CreatedBy user and admins
// (PermUserManage) may read; everyone else sees ErrForbidden. A nil user
// produces ErrUnauthenticated; a nil scenario produces ErrScenarioNotFound.
func TestAuthorizeRead_Given_OwnerOrAdmin_When_Called_Then_AllowsOtherwiseForbid(t *testing.T) {
	s := &scenarios.Scenario{RID: "ri.vertex.main.scenario.s1", CreatedBy: "alice"}

	cases := []struct {
		name    string
		user    *auth.User
		scen    *scenarios.Scenario
		wantErr error
	}{
		{"nil user is unauthenticated", nil, s, scenarios.ErrUnauthenticated},
		{"nil scenario is not-found", &auth.User{ID: "alice"}, nil, scenarios.ErrScenarioNotFound},
		{"owner reads own scenario", &auth.User{ID: "alice"}, s, nil},
		{"foreign user is forbidden", &auth.User{ID: "mallory"}, s, scenarios.ErrForbidden},
		{"foreign user with viewer role still forbidden", &auth.User{ID: "mallory", Roles: []string{auth.RoleViewer}}, s, scenarios.ErrForbidden},
		{"foreign user with editor role still forbidden", &auth.User{ID: "mallory", Roles: []string{auth.RoleEditor}}, s, scenarios.ErrForbidden},
		{"foreign user with ontology-owner role still forbidden", &auth.User{ID: "mallory", Roles: []string{auth.RoleOntologyOwner}}, s, scenarios.ErrForbidden},
		{"admin can read any scenario", &auth.User{ID: "root", Roles: []string{auth.RoleAdmin}}, s, nil},
		{"empty user ID falls through to forbidden", &auth.User{ID: ""}, s, scenarios.ErrForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := scenarios.AuthorizeRead(tc.user, tc.scen)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AuthorizeRead = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestAuthorizeWrite mirrors the read matrix. The contract differs slightly:
// admins may write any scenario (audit covered separately); foreign users
// remain forbidden even when they carry write-related roles like editor.
func TestAuthorizeWrite_Given_OwnerOrAdmin_When_Called_Then_AllowsOtherwiseForbid(t *testing.T) {
	s := &scenarios.Scenario{RID: "ri.vertex.main.scenario.s1", CreatedBy: "alice"}

	cases := []struct {
		name    string
		user    *auth.User
		scen    *scenarios.Scenario
		wantErr error
	}{
		{"nil user is unauthenticated", nil, s, scenarios.ErrUnauthenticated},
		{"nil scenario is not-found", &auth.User{ID: "alice"}, nil, scenarios.ErrScenarioNotFound},
		{"owner writes own scenario", &auth.User{ID: "alice"}, s, nil},
		{"foreign editor cannot write someone else's scenario", &auth.User{ID: "mallory", Roles: []string{auth.RoleEditor}}, s, scenarios.ErrForbidden},
		{"foreign ontology-owner cannot write someone else's scenario", &auth.User{ID: "mallory", Roles: []string{auth.RoleOntologyOwner}}, s, scenarios.ErrForbidden},
		{"admin can write any scenario", &auth.User{ID: "root", Roles: []string{auth.RoleAdmin}}, s, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := scenarios.AuthorizeWrite(tc.user, tc.scen)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AuthorizeWrite = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestMaskEditsForUser covers the marking-driven diff masking semantics:
// scenario edits referencing objects whose markings the caller cannot see
// must be returned with NewValue replaced by the redaction sentinel. Edits
// whose objects the caller can see pass through unchanged.
func TestMaskEditsForUser_Given_MissingMarkings_When_Masking_Then_NewValueRedacted(t *testing.T) {
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "createObject", ObjectType: "Airport", ObjectID: "JFK", NewValue: json.RawMessage(`{"capacity":100}`)},
		{Seq: 2, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "SFO", Property: "capacity", NewValue: json.RawMessage(`120`)},
		{Seq: 3, Op: "deleteObject", ObjectType: "Airport", ObjectID: "JFK"},
		{Seq: 4, Op: "addLink", LinkType: "served_by", SrcID: "JFK", DstID: "AA"},
		{Seq: 5, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "LAX", Property: "capacity", NewValue: json.RawMessage(`200`)},
	}

	// JFK requires SECRET (caller lacks); SFO requires INTERNAL (caller has);
	// LAX is unmarked.
	markings := map[scenarios.ObjectKey][]string{
		{ObjectType: "Airport", ObjectID: "JFK"}: {"SECRET"},
		{ObjectType: "Airport", ObjectID: "SFO"}: {"INTERNAL"},
	}

	bob := &auth.User{
		ID: "bob",
		Attributes: map[string]any{
			auth.MarkingsAttributeKey: []string{"INTERNAL"},
		},
	}

	got := scenarios.MaskEditsForUser(bob, edits, markings)
	if len(got) != len(edits) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(edits))
	}

	// JFK createObject: NewValue must be redacted, op preserved
	if got[0].Op != "createObject" || string(got[0].NewValue) != scenarios.RedactedValueLiteral {
		t.Errorf("JFK createObject: got NewValue=%q, want redacted literal", string(got[0].NewValue))
	}
	// SFO modifyProperty: caller holds INTERNAL → unchanged
	if string(got[1].NewValue) != `120` {
		t.Errorf("SFO modifyProperty: got NewValue=%q, want untouched 120", string(got[1].NewValue))
	}
	// JFK deleteObject: no NewValue, structure unchanged
	if got[2].Op != "deleteObject" || len(got[2].NewValue) != 0 {
		t.Errorf("JFK deleteObject: got op=%q NewValue=%q, want delete with empty value", got[2].Op, string(got[2].NewValue))
	}
	// addLink: no NewValue, structure unchanged (link masking would need its own pass)
	if got[3].Op != "addLink" {
		t.Errorf("addLink: op mutated to %q", got[3].Op)
	}
	// LAX modifyProperty: unmarked object → unchanged
	if string(got[4].NewValue) != `200` {
		t.Errorf("LAX modifyProperty: got NewValue=%q, want untouched 200", string(got[4].NewValue))
	}
}

// TestMaskEditsForUser_Given_NoUser_When_Masking_Then_AllMarkedRedacted asserts
// that an anonymous caller cannot see any marked object's value. This is a
// negative test for the "user with no markings" branch.
func TestMaskEditsForUser_Given_NoUser_When_Masking_Then_AllMarkedRedacted(t *testing.T) {
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: json.RawMessage(`100`)},
	}
	markings := map[scenarios.ObjectKey][]string{
		{ObjectType: "Airport", ObjectID: "JFK"}: {"INTERNAL"},
	}
	got := scenarios.MaskEditsForUser(nil, edits, markings)
	if len(got) != 1 || string(got[0].NewValue) != scenarios.RedactedValueLiteral {
		t.Fatalf("nil user must see marked values masked, got NewValue=%q", string(got[0].NewValue))
	}
}

// TestMaskEditsForUser_Given_AdminUser_When_Masking_Then_NothingRedacted asserts
// admins bypass marking masks — matches engine.go / rls.Engine convention.
func TestMaskEditsForUser_Given_AdminUser_When_Masking_Then_NothingRedacted(t *testing.T) {
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: json.RawMessage(`100`)},
	}
	markings := map[scenarios.ObjectKey][]string{
		{ObjectType: "Airport", ObjectID: "JFK"}: {"SECRET"},
	}
	root := &auth.User{ID: "root", Roles: []string{auth.RoleAdmin}}
	got := scenarios.MaskEditsForUser(root, edits, markings)
	if string(got[0].NewValue) != `100` {
		t.Fatalf("admin must see clear value, got %q", string(got[0].NewValue))
	}
}

// TestMaskEditsForUser_Given_PartialMarkingsMatch_When_Masking_Then_AllRequiredNeeded
// verifies AND semantics: holding one of two required markings still masks.
func TestMaskEditsForUser_Given_PartialMarkingsMatch_When_Masking_Then_AllRequiredNeeded(t *testing.T) {
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Doc", ObjectID: "D1", Property: "body", NewValue: json.RawMessage(`"secret"`)},
	}
	markings := map[scenarios.ObjectKey][]string{
		{ObjectType: "Doc", ObjectID: "D1"}: {"INTERNAL", "PII"},
	}
	bob := &auth.User{
		ID: "bob",
		Attributes: map[string]any{
			auth.MarkingsAttributeKey: []string{"INTERNAL"}, // missing PII
		},
	}
	got := scenarios.MaskEditsForUser(bob, edits, markings)
	if string(got[0].NewValue) != scenarios.RedactedValueLiteral {
		t.Fatalf("partial coverage must still mask, got %q", string(got[0].NewValue))
	}
}

// TestMaskEditsForUser_Given_EmptyInput_When_Masking_Then_ReturnsEmpty covers
// the degenerate cases: nil edits, empty markings index.
func TestMaskEditsForUser_Given_EmptyInput_When_Masking_Then_ReturnsEmpty(t *testing.T) {
	bob := &auth.User{ID: "bob"}

	if out := scenarios.MaskEditsForUser(bob, nil, nil); len(out) != 0 {
		t.Errorf("nil edits → empty slice, got len=%d", len(out))
	}
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "modifyProperty", ObjectType: "Airport", ObjectID: "JFK", Property: "capacity", NewValue: json.RawMessage(`100`)},
	}
	got := scenarios.MaskEditsForUser(bob, edits, nil)
	if string(got[0].NewValue) != `100` {
		t.Fatalf("unmarked objects with nil markings index must pass through, got %q", string(got[0].NewValue))
	}
}

// TestMaskEditsForUser_Given_MaskingResult_When_Mutated_Then_OriginalUnchanged
// verifies the function returns a copy — caller mutations must not alter the
// caller's input slice (defensive copy contract).
func TestMaskEditsForUser_Given_MaskingResult_When_Mutated_Then_OriginalUnchanged(t *testing.T) {
	original := json.RawMessage(`{"capacity":100}`)
	edits := []scenarios.ScenarioEdit{
		{Seq: 1, Op: "createObject", ObjectType: "Airport", ObjectID: "JFK", NewValue: original},
	}
	markings := map[scenarios.ObjectKey][]string{
		{ObjectType: "Airport", ObjectID: "JFK"}: {"SECRET"},
	}
	got := scenarios.MaskEditsForUser(&auth.User{ID: "bob"}, edits, markings)
	// Mutate the returned slice
	got[0].NewValue = json.RawMessage(`"poisoned"`)
	if string(edits[0].NewValue) != `{"capacity":100}` {
		t.Fatalf("input edits mutated by caller-side write: got %q", string(edits[0].NewValue))
	}
}
