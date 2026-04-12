package auth_test

import (
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestMarkingAndSemantics verifies EvaluateMarkings follows Foundry's AND
// semantics: a user may see an object only if they hold every marking the
// object carries. Missing any marking fails closed; holding extras does
// not help or hurt; an object with no markings is visible to everyone.
func TestMarkingAndSemantics(t *testing.T) {
	tests := []struct {
		name           string
		userMarkings   []string
		objectMarkings []string
		want           bool
	}{
		{
			name:           "empty object markings are always visible",
			userMarkings:   nil,
			objectMarkings: nil,
			want:           true,
		},
		{
			name:           "empty-slice object markings are always visible",
			userMarkings:   []string{"PUBLIC"},
			objectMarkings: []string{},
			want:           true,
		},
		{
			name:           "user holds every object marking",
			userMarkings:   []string{"ACME", "ACME2", "PUBLIC"},
			objectMarkings: []string{"ACME", "ACME2"},
			want:           true,
		},
		{
			name:           "user holds exactly the required set",
			userMarkings:   []string{"ACME"},
			objectMarkings: []string{"ACME"},
			want:           true,
		},
		{
			name:           "user missing one required marking",
			userMarkings:   []string{"ACME"},
			objectMarkings: []string{"ACME", "ACME2"},
			want:           false,
		},
		{
			name:           "user has no markings but object requires one",
			userMarkings:   nil,
			objectMarkings: []string{"ACME"},
			want:           false,
		},
		{
			name:           "user has unrelated markings",
			userMarkings:   []string{"PUBLIC", "INTERNAL"},
			objectMarkings: []string{"ACME"},
			want:           false,
		},
		{
			name:           "duplicate user markings still grant",
			userMarkings:   []string{"ACME", "ACME"},
			objectMarkings: []string{"ACME"},
			want:           true,
		},
		{
			name:           "duplicate object markings are deduped",
			userMarkings:   []string{"ACME"},
			objectMarkings: []string{"ACME", "ACME"},
			want:           true,
		},
		{
			name:           "marking names are case sensitive",
			userMarkings:   []string{"acme"},
			objectMarkings: []string{"ACME"},
			want:           false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.EvaluateMarkings(tc.userMarkings, tc.objectMarkings)
			if got != tc.want {
				t.Errorf("EvaluateMarkings(%v, %v) = %v, want %v",
					tc.userMarkings, tc.objectMarkings, got, tc.want)
			}
		})
	}
}
