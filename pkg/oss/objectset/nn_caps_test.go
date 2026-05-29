package objectset_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// mkNN builds a minimally-valid nearestNeighbors Definition (objectSet +
// propertyIdentifier present) with an optional NumNeighbors (k > 0) and an
// optional raw query vector of the given dimension (dim > 0).
func mkNN(k, dim int) *objectset.Definition {
	d := &objectset.Definition{
		Type:      "nearestNeighbors",
		ObjectSet: &objectset.Definition{Type: "base", ObjectType: "employee"},
	}
	pi := objectset.PropertyIdentifier{}
	pi.Property.APIName = "embedding"
	d.PropertyIdentifier = &pi
	if k > 0 {
		kk := k
		d.NumNeighbors = &kk
	}
	if dim > 0 {
		d.Query = &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: make([]float64, dim)}}
	}
	return d
}

// TestDefinition_ValidateNearestNeighbors_Caps locks the Foundry-documented
// ceilings K ≤ 100 and vector dimension ≤ 2048 (docs/Palantir ObjectSet &
// OntologyAggregation 完整语法参考.md L115). At-limit requests validate; over
// the limit is rejected at Definition.Validate.
func TestDefinition_ValidateNearestNeighbors_Caps(t *testing.T) {
	// At the limits → valid.
	if err := mkNN(objectset.MaxNearestNeighbors, objectset.MaxVectorDimension).Validate(); err != nil {
		t.Errorf("nearestNeighbors at K=%d / dim=%d limits: unexpected error %v",
			objectset.MaxNearestNeighbors, objectset.MaxVectorDimension, err)
	}

	// K over the limit → rejected.
	err := mkNN(objectset.MaxNearestNeighbors+1, 0).Validate()
	if err == nil {
		t.Fatalf("K=%d: expected numNeighbors cap rejection, got nil", objectset.MaxNearestNeighbors+1)
	}
	if !strings.Contains(err.Error(), "numNeighbors") {
		t.Errorf("error %q should mention numNeighbors", err.Error())
	}

	// Vector dimension over the limit → rejected.
	err = mkNN(0, objectset.MaxVectorDimension+1).Validate()
	if err == nil {
		t.Fatalf("dim=%d: expected vector dimension cap rejection, got nil", objectset.MaxVectorDimension+1)
	}
	if !strings.Contains(err.Error(), "dimension") {
		t.Errorf("error %q should mention dimension", err.Error())
	}
}

// TestBDD_NearestNeighbors_OverLimitK_Rejected is the HTTP contract for the
// K ≤ 100 ceiling.
//
//	Given a loadObjects request whose nearestNeighbors numNeighbors is 101
//	When  the handler runs
//	Then  it returns HTTP 400 InvalidObjectSet mentioning numNeighbors — the
//	      over-large kNN never reaches the vector store.
func TestBDD_NearestNeighbors_OverLimitK_Rejected(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)

	body := `{"objectSet":{"type":"nearestNeighbors","objectSet":{"type":"base","objectType":"employee"},` +
		`"propertyIdentifier":{"property":{"apiName":"embedding"}},"numNeighbors":101},"select":["id"]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/myOntology/objectSets/loadObjects",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assertExecutorSentinel(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT",
		"InvalidObjectSet", "numNeighbors")
}
