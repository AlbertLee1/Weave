package objectset

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fakePropMapResolver is an in-package InterfacePropertyMappingResolver double
// used to drive the buildInterfaceToObjectTypeMappings unit tests without any
// OMS / PostgreSQL dependency. It also satisfies InterfaceResolver so it can
// stand in wherever the executor expects the base capability.
type fakePropMapResolver struct {
	// mappings: interfaceApiName -> objectTypeApiName -> sptApiName -> propApiName
	mappings map[string]map[string]map[string]string
	err      error
}

func (f *fakePropMapResolver) ResolveInterfaceObjectTypes(_ context.Context, iface string) ([]string, error) {
	out := make([]string, 0, len(f.mappings[iface]))
	for ot := range f.mappings[iface] {
		out = append(out, ot)
	}
	return out, nil
}

func (f *fakePropMapResolver) ResolveInterfacePropertyMappings(_ context.Context, iface string) (map[string]map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.mappings[iface], nil
}

func TestCollectInterfaceTypes(t *testing.T) {
	tests := []struct {
		name string
		def  *Definition
		want []string
	}{
		{
			name: "plain base objectType has no interface scope",
			def:  &Definition{Type: "base", ObjectType: "employee"},
			want: []string{},
		},
		{
			name: "interfaceBase yields its interface",
			def:  &Definition{Type: "interfaceBase", InterfaceType: "HasOwner"},
			want: []string{"HasOwner"},
		},
		{
			name: "filter over interfaceBase reaches the nested interface",
			def: &Definition{
				Type:      "filter",
				ObjectSet: &Definition{Type: "interfaceBase", InterfaceType: "HasOwner"},
			},
			want: []string{"HasOwner"},
		},
		{
			name: "union dedupes and sorts interfaces",
			def: &Definition{
				Type: "union",
				ObjectSets: []*Definition{
					{Type: "interfaceBase", InterfaceType: "Named"},
					{Type: "interfaceBase", InterfaceType: "HasOwner"},
					{Type: "interfaceBase", InterfaceType: "Named"},
				},
			},
			want: []string{"HasOwner", "Named"},
		},
		{
			name: "nil definition yields empty",
			def:  nil,
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectInterfaceTypes(tt.def)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("collectInterfaceTypes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildInterfaceToObjectTypeMappings(t *testing.T) {
	resolver := &fakePropMapResolver{
		mappings: map[string]map[string]map[string]string{
			"HasOwner": {
				"employee": {"ownerName": "manager", "ownerId": "empId"},
				"vehicle":  {"ownerName": "driver", "ownerId": "vin"},
			},
		},
	}

	t.Run("nil resolver returns nil", func(t *testing.T) {
		got := buildInterfaceToObjectTypeMappings(context.Background(), nil, []string{"HasOwner"})
		if got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("no interfaces returns nil", func(t *testing.T) {
		got := buildInterfaceToObjectTypeMappings(context.Background(), resolver, nil)
		if got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})

	t.Run("maps interface SPT to per-object-type local property", func(t *testing.T) {
		got := buildInterfaceToObjectTypeMappings(context.Background(), resolver, []string{"HasOwner"})
		want := map[string]map[string]map[string]string{
			"HasOwner": {
				"employee": {"ownerName": "manager", "ownerId": "empId"},
				"vehicle":  {"ownerName": "driver", "ownerId": "vin"},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("mappings = %#v, want %#v", got, want)
		}
	})

	t.Run("interface with no implementers is omitted", func(t *testing.T) {
		got := buildInterfaceToObjectTypeMappings(context.Background(), resolver, []string{"Unknown"})
		if got != nil {
			t.Fatalf("expected nil for interface with no mappings, got %v", got)
		}
	})

	t.Run("resolver error skips the interface", func(t *testing.T) {
		boom := &fakePropMapResolver{err: errors.New("boom")}
		got := buildInterfaceToObjectTypeMappings(context.Background(), boom, []string{"HasOwner"})
		if got != nil {
			t.Fatalf("expected nil when resolver errors, got %v", got)
		}
	})

	t.Run("returned inner maps are copies, not resolver-backed", func(t *testing.T) {
		got := buildInterfaceToObjectTypeMappings(context.Background(), resolver, []string{"HasOwner"})
		got["HasOwner"]["employee"]["ownerName"] = "MUTATED"
		if resolver.mappings["HasOwner"]["employee"]["ownerName"] != "manager" {
			t.Fatal("expected resolver backing map to be unaffected by caller mutation")
		}
	})
}
