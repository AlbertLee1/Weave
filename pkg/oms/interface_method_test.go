package oms

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInterfaceMethod_Validate(t *testing.T) {
	tests := []struct {
		name    string
		im      InterfaceMethod
		wantErr string
	}{
		{
			name: "happy path",
			im: InterfaceMethod{
				RID:          "ri.ontology.main.interface-method.1",
				InterfaceRID: "ri.ontology.main.interface.1",
				Name:         "greet",
				Params: []InterfaceMethodParam{
					{Name: "greeting", Type: "string", Required: true},
				},
				Returns: InterfaceMethodReturns{Type: "string"},
			},
		},
		{
			name: "missing rid",
			im: InterfaceMethod{
				InterfaceRID: "ri.i",
				Name:         "greet",
			},
			wantErr: "requires rid",
		},
		{
			name: "missing interfaceRid",
			im: InterfaceMethod{
				RID:  "ri.m",
				Name: "greet",
			},
			wantErr: "requires interfaceRid",
		},
		{
			name: "missing name",
			im: InterfaceMethod{
				RID:          "ri.m",
				InterfaceRID: "ri.i",
			},
			wantErr: "requires name",
		},
		{
			name: "param missing name",
			im: InterfaceMethod{
				RID:          "ri.m",
				InterfaceRID: "ri.i",
				Name:         "greet",
				Params:       []InterfaceMethodParam{{Type: "string"}},
			},
			wantErr: "param[0] requires name",
		},
		{
			name: "param missing type",
			im: InterfaceMethod{
				RID:          "ri.m",
				InterfaceRID: "ri.i",
				Name:         "greet",
				Params:       []InterfaceMethodParam{{Name: "x"}},
			},
			wantErr: "requires type",
		},
		{
			name: "duplicate param",
			im: InterfaceMethod{
				RID:          "ri.m",
				InterfaceRID: "ri.i",
				Name:         "greet",
				Params: []InterfaceMethodParam{
					{Name: "x", Type: "string"},
					{Name: "x", Type: "integer"},
				},
			},
			wantErr: `param "x" declared twice`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.im.Validate()
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
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestInterfaceMethod_JSONRoundTrip(t *testing.T) {
	im := InterfaceMethod{
		RID:          "ri.m",
		InterfaceRID: "ri.i",
		Name:         "greet",
		Params: []InterfaceMethodParam{
			{Name: "x", Type: "string", Required: true, Default: json.RawMessage(`"hi"`)},
			{Name: "n", Type: "integer"},
		},
		Returns:     InterfaceMethodReturns{Type: "string"},
		Description: "say hello",
	}
	raw, err := json.Marshal(&im)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded InterfaceMethod
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Name != "greet" || decoded.Returns.Type != "string" {
		t.Fatalf("unexpected decoded: %+v", decoded)
	}
	if len(decoded.Params) != 2 || decoded.Params[0].Default == nil {
		t.Fatalf("unexpected params: %+v", decoded.Params)
	}
}
