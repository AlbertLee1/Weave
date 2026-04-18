package oms

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestObjectSetSnapshot_Validate(t *testing.T) {
	cases := []struct {
		name    string
		snap    ObjectSetSnapshot
		wantErr string
	}{
		{
			name: "happy path",
			snap: ObjectSetSnapshot{
				RID:             "ri.objectsets.main.snapshot.abc",
				OntologyAPIName: "myOntology",
				ObjectType:      "employee",
				Definition:      json.RawMessage(`{"type":"base","objectType":"employee"}`),
				PrimaryKeys:     []string{"e1", "e2"},
			},
		},
		{
			name:    "missing rid",
			snap:    ObjectSetSnapshot{OntologyAPIName: "x", ObjectType: "y", Definition: json.RawMessage(`{}`)},
			wantErr: "rid",
		},
		{
			name:    "missing ontology api name",
			snap:    ObjectSetSnapshot{RID: "r", ObjectType: "y", Definition: json.RawMessage(`{}`)},
			wantErr: "ontologyApiName",
		},
		{
			name:    "missing object type",
			snap:    ObjectSetSnapshot{RID: "r", OntologyAPIName: "o", Definition: json.RawMessage(`{}`)},
			wantErr: "objectType",
		},
		{
			name:    "missing definition",
			snap:    ObjectSetSnapshot{RID: "r", OntologyAPIName: "o", ObjectType: "y"},
			wantErr: "definition",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.snap.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}
