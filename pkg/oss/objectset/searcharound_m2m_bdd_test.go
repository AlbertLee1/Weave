package objectset_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
)

type m2mObjectSetRepo struct {
	oms.Repository

	ontology         *oms.Ontology
	objectTypes      map[string]*oms.ObjectType
	objectTypesByAPI map[string]*oms.ObjectType
	linkTypes        map[string]*oms.LinkType
	outgoing         map[string][]oms.LinkType
	incoming         map[string][]oms.LinkType
}

func (r *m2mObjectSetRepo) GetOntology(_ context.Context, key string) (*oms.Ontology, error) {
	if r.ontology != nil && (key == r.ontology.RID || key == r.ontology.APIName) {
		return r.ontology, nil
	}
	return nil, fmt.Errorf("ontology %q not found", key)
}

func (r *m2mObjectSetRepo) GetObjectType(_ context.Context, rid string) (*oms.ObjectType, error) {
	ot, ok := r.objectTypes[rid]
	if !ok {
		return nil, fmt.Errorf("object type %q not found", rid)
	}
	return ot, nil
}

func (r *m2mObjectSetRepo) GetObjectTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
	if r.ontology == nil || ontologyRID != r.ontology.RID {
		return nil, fmt.Errorf("ontology %q not found", ontologyRID)
	}
	ot, ok := r.objectTypesByAPI[apiName]
	if !ok {
		return nil, fmt.Errorf("object type %q not found", apiName)
	}
	return ot, nil
}

func (r *m2mObjectSetRepo) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	lt, ok := r.linkTypes[rid]
	if !ok {
		return nil, fmt.Errorf("link type %q not found", rid)
	}
	return lt, nil
}

func (r *m2mObjectSetRepo) ListOutgoingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	return r.outgoing[objectTypeRID], nil
}

func (r *m2mObjectSetRepo) ListIncomingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	return r.incoming[objectTypeRID], nil
}

type m2mObjectSetEdgeRepo struct {
	edges map[string][][2]string
}

func (r *m2mObjectSetEdgeRepo) ListEdgeTargets(_ context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	allowed := make(map[string]bool, len(sourcePKs))
	for _, pk := range sourcePKs {
		allowed[pk] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, edge := range r.edges[linkTypeRID] {
		if allowed[edge[0]] && !seen[edge[1]] {
			seen[edge[1]] = true
			out = append(out, edge[1])
		}
	}
	return out, nil
}

func (r *m2mObjectSetEdgeRepo) ListEdgeSources(_ context.Context, linkTypeRID string, targetPKs []string) ([]string, error) {
	allowed := make(map[string]bool, len(targetPKs))
	for _, pk := range targetPKs {
		allowed[pk] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, edge := range r.edges[linkTypeRID] {
		if allowed[edge[1]] && !seen[edge[0]] {
			seen[edge[0]] = true
			out = append(out, edge[0])
		}
	}
	return out, nil
}

func TestBDD_ObjectSetSearchAround_M2MJoinEdges(t *testing.T) {
	// Given an ObjectSet executor wired to the production link resolver and an
	// M2M LinkType backed by link_edges-style rows.
	mgr := index.NewManager(t.TempDir())
	t.Cleanup(func() { mgr.Close() })

	props := []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}
	for _, objectType := range []string{"orders", "products"} {
		if _, err := mgr.EnsureIndex(objectType, props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", objectType, err)
		}
	}
	for _, orderID := range []string{"10249", "10250"} {
		if err := mgr.IndexDocument("orders", orderID, map[string]interface{}{"id": orderID}); err != nil {
			t.Fatalf("IndexDocument order %s: %v", orderID, err)
		}
	}
	if err := mgr.IndexDocument("products", "51", map[string]interface{}{"id": "51"}); err != nil {
		t.Fatalf("IndexDocument product 51: %v", err)
	}

	orderOT := &oms.ObjectType{RID: "ri.ot.orders", OntologyRID: "ri.ont.northwind", APIName: "orders", PrimaryKey: "id"}
	productOT := &oms.ObjectType{RID: "ri.ot.products", OntologyRID: "ri.ont.northwind", APIName: "products", PrimaryKey: "id"}
	linkType := oms.LinkType{
		RID:              "ri.lt.order-products",
		OntologyRID:      "ri.ont.northwind",
		APIName:          "orderProducts",
		SourceObjectType: orderOT.RID,
		TargetObjectType: productOT.RID,
		Cardinality:      "MANY_TO_MANY",
	}
	repo := &m2mObjectSetRepo{
		ontology:         &oms.Ontology{RID: "ri.ont.northwind", APIName: "northwind"},
		objectTypes:      map[string]*oms.ObjectType{orderOT.RID: orderOT, productOT.RID: productOT},
		objectTypesByAPI: map[string]*oms.ObjectType{orderOT.APIName: orderOT, productOT.APIName: productOT},
		linkTypes:        map[string]*oms.LinkType{linkType.RID: &linkType},
		outgoing:         map[string][]oms.LinkType{orderOT.RID: {linkType}},
		incoming:         map[string][]oms.LinkType{productOT.RID: {linkType}},
	}
	edgeRepo := &m2mObjectSetEdgeRepo{
		edges: map[string][][2]string{
			linkType.RID: {
				{"10249", "14"},
				{"10249", "51"},
				{"10249", "51"},
				{"10250", "41"},
				{"10250", "51"},
				{"10250", "65"},
			},
		},
	}
	resolver := links.NewResolverWithEdges(repo, mgr, edgeRepo)
	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	ctx := objectset.WithOntologyScope(context.Background(), "northwind")

	t.Run("forward returns deduped target objects with target object type", func(t *testing.T) {
		// When searchAround walks orders -> products, Then converging edges
		// produce a deduped product ObjectSet.
		result, err := executor.Execute(ctx, &objectset.Definition{
			Type:      "searchAround",
			ObjectSet: &objectset.Definition{Type: "base", ObjectType: "orders"},
			Link:      "orderProducts",
		})
		if err != nil {
			t.Fatalf("Execute forward: %v", err)
		}
		if result.ObjectType != "products" {
			t.Fatalf("ObjectType: want products, got %q", result.ObjectType)
		}
		got := sorted(result.PrimaryKeys)
		want := []string{"14", "41", "51", "65"}
		if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
			t.Fatalf("PrimaryKeys: want %v, got %v", want, got)
		}
	})

	t.Run("reverse returns deduped source objects with source object type", func(t *testing.T) {
		// When searchAround walks products -> orders in reverse, Then the same
		// M2M LinkType returns the source-side ObjectSet.
		result, err := executor.Execute(ctx, &objectset.Definition{
			Type:      "searchAround",
			ObjectSet: &objectset.Definition{Type: "base", ObjectType: "products"},
			Link:      "orderProducts",
			Direction: "reverse",
		})
		if err != nil {
			t.Fatalf("Execute reverse: %v", err)
		}
		if result.ObjectType != "orders" {
			t.Fatalf("ObjectType: want orders, got %q", result.ObjectType)
		}
		got := sorted(result.PrimaryKeys)
		want := []string{"10249", "10250"}
		if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
			t.Fatalf("PrimaryKeys: want %v, got %v", want, got)
		}
	})
}
