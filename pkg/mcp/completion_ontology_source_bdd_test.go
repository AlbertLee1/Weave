package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_OntologyCompletionSource covers PRD-V2 Gap-D4 round 47
// — production wiring of the round-46 MCP completion/complete
// protocol method. The protocol handler can now resolve real
// autocomplete suggestions instead of always returning empty
// envelopes.
//
// Five source paths verified:
//   - prompt argument "objectType" / "objectTypeApiName" → OMS
//     ObjectType apiNames within the prompt's ontology
//   - prompt argument "actionType" → ActionType apiNames
//   - prompt argument "linkType" → outgoing LinkType apiNames
//     (deduped across the ontology's ObjectTypes)
//   - resource URI "weave://objecttype/<ontology>/" → ObjectType
//     apiNames within that ontology
//   - resource URI "weave://ontology/" → ontology apiNames
//
// Plus defensive behaviors: nil repo, unknown ontology, unknown
// prompt-name shape, unknown argument, malformed URI all yield
// nil (handler maps to empty completion envelope — never errors).
func TestBDD_OntologyCompletionSource(t *testing.T) {
	repo := &fakeOMSCompletionRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.onto.main.northwind", APIName: "northwind"},
			{RID: "ri.onto.main.chinook", APIName: "chinook"},
		},
		objectTypesByOntologyRID: map[string][]oms.ObjectType{
			"ri.onto.main.northwind": {
				{RID: "ri.ot.cust", APIName: "Customer"},
				{RID: "ri.ot.ord", APIName: "Order"},
				{RID: "ri.ot.empl", APIName: "Employee"},
			},
		},
		actionTypesByOntologyRID: map[string][]oms.ActionType{
			"ri.onto.main.northwind": {
				{RID: "ri.at.create", APIName: "createOrder"},
				{RID: "ri.at.cancel", APIName: "cancelOrder"},
			},
		},
		outgoingLinkTypesByObjectTypeRID: map[string][]oms.LinkType{
			"ri.ot.cust": {{APIName: "placedOrders"}},
			"ri.ot.ord":  {{APIName: "items"}, {APIName: "customer"}},
		},
	}
	src := NewOntologyCompletionSource(repo)

	t.Run("ref/prompt arg=objectType yields ObjectType apiNames", func(t *testing.T) {
		out, err := src.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "northwind__createCustomer"},
			CompletionArgument{Name: "objectType", Value: ""},
			100)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		// PrefixFilter sorts alphabetically.
		want := []string{"Customer", "Employee", "Order"}
		assertStringsEqual(t, out, want)
	})

	t.Run("ref/prompt arg=objectType with prefix narrows results", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "northwind__x"},
			CompletionArgument{Name: "objectType", Value: "C"},
			100)
		assertStringsEqual(t, out, []string{"Customer"})
	})

	t.Run("ref/prompt arg=actionType yields ActionType apiNames", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "northwind__createCustomer"},
			CompletionArgument{Name: "actionType", Value: ""},
			100)
		assertStringsEqual(t, out, []string{"cancelOrder", "createOrder"})
	})

	t.Run("ref/prompt arg=linkType yields deduped outgoing links across types", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "northwind__x"},
			CompletionArgument{Name: "linkType", Value: ""},
			100)
		assertStringsEqual(t, out, []string{"customer", "items", "placedOrders"})
	})

	t.Run("ref/prompt arg name is case-insensitive", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "northwind__x"},
			CompletionArgument{Name: "ObjectTypeApiName", Value: ""},
			100)
		if len(out) == 0 {
			t.Errorf("expected non-empty completions for ObjectTypeApiName, got empty")
		}
	})

	t.Run("ref/resource weave://objecttype/<ontology>/ yields ObjectType names", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/resource", URI: "weave://objecttype/northwind/"},
			CompletionArgument{Name: "objectType", Value: ""},
			100)
		assertStringsEqual(t, out, []string{"Customer", "Employee", "Order"})
	})

	t.Run("ref/resource weave://ontology/ yields ontology apiNames", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/resource", URI: "weave://ontology/"},
			CompletionArgument{Name: "ontology", Value: ""},
			100)
		assertStringsEqual(t, out, []string{"chinook", "northwind"})
	})

	t.Run("unknown ontology in prompt name yields empty (no error)", func(t *testing.T) {
		out, err := src.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "ghost__x"},
			CompletionArgument{Name: "objectType", Value: ""},
			100)
		if err != nil {
			t.Errorf("err = %v, want nil (unknown ontology should not error)", err)
		}
		if len(out) != 0 {
			t.Errorf("len = %d, want 0 for unknown ontology", len(out))
		}
	})

	t.Run("malformed prompt name (no separator) yields empty", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "no-separator-here"},
			CompletionArgument{Name: "objectType", Value: ""},
			100)
		if len(out) != 0 {
			t.Errorf("len = %d, want 0 for malformed prompt name", len(out))
		}
	})

	t.Run("unknown argument name yields empty", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "northwind__x"},
			CompletionArgument{Name: "ghost_field", Value: ""},
			100)
		if len(out) != 0 {
			t.Errorf("len = %d, want 0 for unknown argument name", len(out))
		}
	})

	t.Run("URI without trailing slash does not match", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/resource", URI: "weave://objecttype/northwind"},
			CompletionArgument{Name: "objectType", Value: ""},
			100)
		if len(out) != 0 {
			t.Errorf("len = %d, want 0 — trailing slash drives the 'completing next segment' semantic", len(out))
		}
	})

	t.Run("URI with deeper segments does not match (already-resolved)", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/resource", URI: "weave://objecttype/northwind/Customer/extra/"},
			CompletionArgument{Name: "x", Value: ""},
			100)
		if len(out) != 0 {
			t.Errorf("len = %d, want 0 — full URI already specifies the ObjectType", len(out))
		}
	})

	t.Run("nil repo source is a no-op (never panics)", func(t *testing.T) {
		nilSrc := NewOntologyCompletionSource(nil)
		out, err := nilSrc.Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "northwind__x"},
			CompletionArgument{Name: "objectType", Value: ""},
			100)
		if err != nil || len(out) != 0 {
			t.Errorf("nil-repo: got (%v, %v), want (nil, nil)", out, err)
		}
	})

	t.Run("unsupported ref.type yields empty", func(t *testing.T) {
		out, _ := src.Complete(context.Background(),
			CompletionRef{Type: "ref/never-heard-of-this", Name: "x"},
			CompletionArgument{Name: "objectType", Value: ""},
			100)
		if len(out) != 0 {
			t.Errorf("len = %d, want 0 for unknown ref.type (handler validates upstream; source is permissive)", len(out))
		}
	})

	t.Run("ListObjectTypes error yields empty (not error)", func(t *testing.T) {
		failingRepo := &fakeOMSCompletionRepo{
			ontologies:                      []oms.Ontology{{RID: "ri.x", APIName: "x"}},
			listObjectTypesErrByOntologyRID: map[string]error{"ri.x": errors.New("pg: unreachable")},
		}
		out, err := NewOntologyCompletionSource(failingRepo).Complete(context.Background(),
			CompletionRef{Type: "ref/prompt", Name: "x__y"},
			CompletionArgument{Name: "objectType", Value: ""},
			100)
		if err != nil {
			t.Errorf("err = %v, want nil (DB failure must not block typing UX)", err)
		}
		if len(out) != 0 {
			t.Errorf("len = %d, want 0 on DB failure", len(out))
		}
	})
}

// ----------------------------------------------------------------------
// fakeOMSCompletionRepo — minimal oms.Repository fake for the source.
// Only implements the 4 methods OntologyCompletionSource actually
// calls (GetOntology, ListOntologies, ListObjectTypes, ListAction
// Types, ListOutgoingLinkTypes); every other Repository method is
// satisfied by embedding `unusedOmsRepo` whose methods panic so test
// drift surfaces immediately.
// ----------------------------------------------------------------------

type fakeOMSCompletionRepo struct {
	// Embed the interface so we only implement the 5 methods
	// OntologyCompletionSource actually calls. Any unintended call
	// to another Repository method panics with a clear nil-method
	// crash so test drift surfaces immediately.
	oms.Repository

	ontologies                       []oms.Ontology
	objectTypesByOntologyRID         map[string][]oms.ObjectType
	actionTypesByOntologyRID         map[string][]oms.ActionType
	outgoingLinkTypesByObjectTypeRID map[string][]oms.LinkType
	listObjectTypesErrByOntologyRID  map[string]error
}

func (f *fakeOMSCompletionRepo) GetOntology(_ context.Context, apiName string) (*oms.Ontology, error) {
	for i := range f.ontologies {
		if f.ontologies[i].APIName == apiName || f.ontologies[i].RID == apiName {
			return &f.ontologies[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (f *fakeOMSCompletionRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	out := make([]oms.Ontology, len(f.ontologies))
	copy(out, f.ontologies)
	return out, nil
}

func (f *fakeOMSCompletionRepo) ListObjectTypes(_ context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	if err, ok := f.listObjectTypesErrByOntologyRID[ontologyRID]; ok {
		return nil, err
	}
	return append([]oms.ObjectType(nil), f.objectTypesByOntologyRID[ontologyRID]...), nil
}

func (f *fakeOMSCompletionRepo) ListActionTypes(_ context.Context, ontologyRID string) ([]oms.ActionType, error) {
	return append([]oms.ActionType(nil), f.actionTypesByOntologyRID[ontologyRID]...), nil
}

func (f *fakeOMSCompletionRepo) ListOutgoingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	return append([]oms.LinkType(nil), f.outgoingLinkTypesByObjectTypeRID[objectTypeRID]...), nil
}

func assertStringsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("len = %d, want %d; got=%v want=%v", len(got), len(want), got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got=%q want=%q (full got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}
}

// Force-use json so future test extensions that need it don't have
// to re-add the import.
var _ = json.Marshal
