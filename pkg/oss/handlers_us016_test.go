package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// setupSearchRouter wires an OSS handler in front of a real Bleve index so the
// body `select` projection contract can be exercised end-to-end. The widget
// type carries a primary key plus three ordinary properties so a subset
// `select` can be distinguished from a full-property response.
func setupSearchRouter(t *testing.T) chi.Router {
	t.Helper()

	dir := t.TempDir()
	mgr := index.NewManager(dir)

	props := []index.Property{
		{APIName: "widgetId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "color", BaseType: "string", IsSearchable: true},
		{APIName: "weight", BaseType: "integer", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex("widget", props); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	docs := map[string]map[string]interface{}{
		"w1": {"widgetId": "w1", "name": "alpha", "color": "red", "weight": float64(10)},
		"w2": {"widgetId": "w2", "name": "beta", "color": "blue", "weight": float64(20)},
	}
	for id, d := range docs {
		if err := mgr.IndexDocument("widget", id, d); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	repo := newMockOmsRepo()
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.objectType.widget",
		OntologyRID: "ri.ontology.main.ontology.test",
		APIName:     "widget",
		PrimaryKey:  "widgetId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})

	svc := oss.NewService(repo, mgr, &mockLinkResolver{results: make(map[string][]string)})
	h := oss.NewHandler(svc)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func widgetSearchURL() string {
	return "/api/v2/ontologies/ri.ontology.main.ontology.test/objects/widget/search"
}

// postWidgetSearch issues a search POST and returns the status plus decoded
// `data` rows (nil rows on a non-200 response).
func postWidgetSearch(t *testing.T, r chi.Router, body string) (int, []map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("POST", widgetSearchURL(), strings.NewReader(body)).
		WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		return rr.Code, nil
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; raw=%s", err, rr.Body.String())
	}
	return rr.Code, resp.Data
}

// TestSearchObjects_SelectOptional verifies the Foundry-aligned relaxation:
// `select` is OPTIONAL. A missing / empty / null select must NOT 400; it
// returns every property (plus the reserved system keys).
func TestSearchObjects_SelectOptional(t *testing.T) {
	r := setupSearchRouter(t)

	cases := []struct {
		name string
		body string
	}{
		{"missing select", `{"where":{"type":"eq","field":"widgetId","value":"w1"}}`},
		{"empty select array", `{"where":{"type":"eq","field":"widgetId","value":"w1"},"select":[]}`},
		{"null select", `{"where":{"type":"eq","field":"widgetId","value":"w1"},"select":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, rows := postWidgetSearch(t, r, tc.body)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200", code)
			}
			if len(rows) != 1 {
				t.Fatalf("expected 1 row, got %d", len(rows))
			}
			row := rows[0]
			for _, k := range []string{"widgetId", "name", "color", "weight", "__primaryKey", "__apiName"} {
				if _, ok := row[k]; !ok {
					t.Errorf("optional-select response missing %q; row=%v", k, row)
				}
			}
		})
	}
}

// TestSearchObjects_SelectProjection_SubsetOnly verifies that a subset select
// narrows the response to the selected properties, retaining the primary key
// and the reserved system fields but dropping every unselected property.
func TestSearchObjects_SelectProjection_SubsetOnly(t *testing.T) {
	r := setupSearchRouter(t)

	body := `{"where":{"type":"eq","field":"widgetId","value":"w1"},"select":["name"]}`
	code, rows := postWidgetSearch(t, r, body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]

	// Selected property + primary key + reserved keys present.
	for _, k := range []string{"name", "widgetId", "__primaryKey", "__apiName"} {
		if _, ok := row[k]; !ok {
			t.Errorf("subset select missing %q; row=%v", k, row)
		}
	}
	if row["name"] != "alpha" {
		t.Errorf("name = %v, want alpha", row["name"])
	}
	// Unselected properties omitted.
	for _, k := range []string{"color", "weight"} {
		if _, ok := row[k]; ok {
			t.Errorf("subset select must omit %q, got %v", k, row[k])
		}
	}
}

// TestSearchObjects_SelectProjection_NonexistentProperty verifies that an
// apiName in `select` that no object carries is silently ignored: it neither
// errors nor materializes a null key. The valid selection still projects.
func TestSearchObjects_SelectProjection_NonexistentProperty(t *testing.T) {
	r := setupSearchRouter(t)

	body := `{"where":{"type":"eq","field":"widgetId","value":"w1"},"select":["name","doesNotExist"]}`
	code, rows := postWidgetSearch(t, r, body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; unknown select property must not error", code)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if _, ok := row["doesNotExist"]; ok {
		t.Errorf("nonexistent select property must not appear in response, got %v", row["doesNotExist"])
	}
	if row["name"] != "alpha" {
		t.Errorf("name = %v, want alpha", row["name"])
	}
	if _, ok := row["color"]; ok {
		t.Errorf("subset select must omit color, got %v", row["color"])
	}
}
