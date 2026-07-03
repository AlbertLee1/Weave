package objectset_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// propMappingInterfaceResolver is a test double that mirrors the production
// cmd/server *pgInterfaceResolver: it resolves an interface to its implementing
// object types AND exposes the per-object-type SharedPropertyType -> local
// property mapping sourced (in production) from OMS
// ObjectTypeInterface.PropertyMapping. Wiring it via SetInterfaceResolver lets
// the loadObjectsMultipleObjectTypes handler assemble interfaceToObjectTypeMappings.
type propMappingInterfaceResolver struct {
	// perInterface: interfaceApiName -> objectTypeApiName -> sptApiName -> propApiName
	perInterface map[string]map[string]map[string]string
}

func (r *propMappingInterfaceResolver) ResolveInterfaceObjectTypes(_ context.Context, iface string) ([]string, error) {
	out := make([]string, 0, len(r.perInterface[iface]))
	for ot := range r.perInterface[iface] {
		out = append(out, ot)
	}
	return out, nil
}

func (r *propMappingInterfaceResolver) ResolveInterfacePropertyMappings(_ context.Context, iface string) (map[string]map[string]string, error) {
	return r.perInterface[iface], nil
}

// TestBDD_LoadObjectsMultipleObjectTypes_ReturnsInterfaceToObjectTypeMappings
//
// Given an interface "HasOwner" implemented by two object types (employee and
//
//	vehicle), each with a PropertyMapping from the interface's
//	SharedPropertyTypes (ownerName, ownerId) to local property apiNames,
//
// When a client POSTs loadObjectsMultipleObjectTypes with an interfaceBase
//
//	ObjectSet over HasOwner,
//
// Then the response carries an interfaceToObjectTypeMappings field mapping
//
//	HasOwner -> {employee,vehicle} -> {sptApiName: localPropertyApiName},
//	so a polymorphic OSDK client can rehydrate interface properties.
func TestBDD_LoadObjectsMultipleObjectTypes_ReturnsInterfaceToObjectTypeMappings(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { _ = mgr.Close() })

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "manager", BaseType: "string", IsSearchable: true},
		{APIName: "empId", BaseType: "string", IsSearchable: true},
		{APIName: "driver", BaseType: "string", IsSearchable: true},
		{APIName: "vin", BaseType: "string", IsSearchable: true},
	}
	for _, ot := range []string{"employee", "vehicle"} {
		if _, err := mgr.EnsureIndex(ot, props); err != nil {
			t.Fatalf("EnsureIndex %s: %v", ot, err)
		}
	}
	if err := mgr.IndexDocument("employee", "e1", map[string]interface{}{"id": "e1", "manager": "alice", "empId": "E-1"}); err != nil {
		t.Fatalf("IndexDocument employee: %v", err)
	}
	if err := mgr.IndexDocument("vehicle", "v1", map[string]interface{}{"id": "v1", "driver": "bob", "vin": "VIN-9"}); err != nil {
		t.Fatalf("IndexDocument vehicle: %v", err)
	}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	executor.SetInterfaceResolver(&propMappingInterfaceResolver{
		perInterface: map[string]map[string]map[string]string{
			"HasOwner": {
				"employee": {"ownerName": "manager", "ownerId": "empId"},
				"vehicle":  {"ownerName": "driver", "ownerId": "vin"},
			},
		},
	})
	handler := objectset.NewHandler(executor, mgr, store)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":          "interfaceBase",
			"interfaceType": "HasOwner",
		},
		"select": []string{"id"},
	}
	bodyBytes, _ := json.Marshal(body)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes", handler.LoadObjectsMultipleObjectTypes)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsMultipleObjectTypes?preview=true", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data                          []map[string]interface{}                `json:"data"`
		InterfaceToObjectTypeMappings map[string]map[string]map[string]string `json:"interfaceToObjectTypeMappings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v — body: %s", err, w.Body.String())
	}

	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 data rows, got %d: %s", len(resp.Data), w.Body.String())
	}

	mappings := resp.InterfaceToObjectTypeMappings
	if mappings == nil {
		t.Fatalf("expected interfaceToObjectTypeMappings in response, got none: %s", w.Body.String())
	}
	hasOwner, ok := mappings["HasOwner"]
	if !ok {
		t.Fatalf("expected HasOwner key in mappings, got %v", mappings)
	}
	empMap := hasOwner["employee"]
	if empMap["ownerName"] != "manager" || empMap["ownerId"] != "empId" {
		t.Errorf("employee mapping wrong: %v", empMap)
	}
	vehMap := hasOwner["vehicle"]
	if vehMap["ownerName"] != "driver" || vehMap["ownerId"] != "vin" {
		t.Errorf("vehicle mapping wrong: %v", vehMap)
	}
}

// TestBDD_LoadObjectsMultipleObjectTypes_PlainObjectTypeOmitsMappings asserts
// the Foundry convention that a pure object-type (non-interface) type scope
// carries no interfaceToObjectTypeMappings field.
func TestBDD_LoadObjectsMultipleObjectTypes_PlainObjectTypeOmitsMappings(t *testing.T) {
	handler, _, _ := setupHandlerTest(t)

	body := map[string]interface{}{
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"select": []string{"id", "name"},
	}
	bodyBytes, _ := json.Marshal(body)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes", handler.LoadObjectsMultipleObjectTypes)

	req := httptest.NewRequest("POST", "/api/v2/ontologies/myOntology/objectSets/loadObjectsMultipleObjectTypes?preview=true", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := resp["interfaceToObjectTypeMappings"]; present {
		t.Errorf("plain objectType response must omit interfaceToObjectTypeMappings; got: %s", w.Body.String())
	}
}
