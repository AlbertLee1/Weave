//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/rid"
)

// pgInterfaceResolver adapts *oms.PGRepository to the executor-level
// objectset.InterfaceResolver contract. Production main.go does not wire a
// resolver yet, so the integration test supplies one here to drive the
// interfaceBase code path end-to-end against a real PG-backed OMS.
type pgInterfaceResolver struct {
	repo        *oms.PGRepository
	ontologyRID string
}

func (r *pgInterfaceResolver) ResolveInterfaceObjectTypes(ctx context.Context, interfaceAPIName string) ([]string, error) {
	iface, err := r.repo.GetInterfaceByAPIName(ctx, r.ontologyRID, interfaceAPIName)
	if err != nil {
		return nil, fmt.Errorf("resolve interface %q: %w", interfaceAPIName, err)
	}
	ots, err := r.repo.ListInterfaceObjectTypes(ctx, iface.RID)
	if err != nil {
		return nil, fmt.Errorf("list implementing types: %w", err)
	}
	names := make([]string, 0, len(ots))
	for _, ot := range ots {
		names = append(names, ot.APIName)
	}
	return names, nil
}

// interfacePagingType describes one of the three Northwind ObjectTypes used
// by this integration test. All three get mapped onto the HasOwner interface
// and share an "id" + "name" shape in the Bleve index so a single select list
// works across every implementing type.
type interfacePagingType struct {
	apiName     string
	displayName string
	csvFile     string
	pkCol       string
	nameCol     string
}

var interfacePagingTypes = []interfacePagingType{
	{apiName: "customer", displayName: "Customer", csvFile: "customers.csv", pkCol: "customerID", nameCol: "companyName"},
	{apiName: "employee", displayName: "Employee", csvFile: "employees.csv", pkCol: "employeeID", nameCol: "lastName"},
	{apiName: "supplier", displayName: "Supplier", csvFile: "suppliers.csv", pkCol: "supplierID", nameCol: "companyName"},
}

// TestInterfacePaging_NorthwindHasOwner exercises US-008's acceptance
// scenario: a HasOwner interface implemented by customer + employee + supplier
// is paginated end-to-end through loadObjectsOrInterfaces with pageSize=7,
// asserting no dropped rows, no duplicates, and totalCount = 91 + 9 + 29.
func TestInterfacePaging_NorthwindHasOwner(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

	// Seed a minimal ontology — just enough metadata to back the interface
	// resolver. No properties are needed because the resolver only reads the
	// interface + object_type_interfaces rows, and the Bleve indexes carry
	// their own synthetic field mapping below.
	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     "northwind",
		DisplayName: "Northwind",
	}
	if err := repo.CreateOntology(ctx, ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	for _, fx := range interfacePagingTypes {
		ot := &oms.ObjectType{
			RID:         rid.NewObjectTypeRID(),
			OntologyRID: ont.RID,
			APIName:     fx.apiName,
			DisplayName: fx.displayName,
			PrimaryKey:  "id",
			Status:      "ACTIVE",
			Visibility:  "NORMAL",
		}
		if err := repo.CreateObjectType(ctx, ot); err != nil {
			t.Fatalf("create object type %q: %v", fx.apiName, err)
		}
	}

	iface := &oms.Interface{
		RID:         rid.NewInterfaceRID(),
		OntologyRID: ont.RID,
		APIName:     "HasOwner",
		DisplayName: "Has Owner",
	}
	if err := repo.CreateInterface(ctx, iface); err != nil {
		t.Fatalf("create interface: %v", err)
	}

	for _, fx := range interfacePagingTypes {
		ot, err := repo.GetObjectTypeByAPIName(ctx, ont.RID, fx.apiName)
		if err != nil {
			t.Fatalf("lookup %q: %v", fx.apiName, err)
		}
		if err := repo.AttachInterface(ctx, &oms.ObjectTypeInterface{
			ObjectTypeRID: ot.RID,
			InterfaceRID:  iface.RID,
		}); err != nil {
			t.Fatalf("attach %q to HasOwner: %v", fx.apiName, err)
		}
	}

	// Bleve indexes: one per implementing type, populated from the Northwind
	// seed CSVs. Every doc is projected onto two synthetic fields ("id" and
	// "name") so a single select list round-trips for every implementing
	// ObjectType — matching how polymorphic Foundry SDK queries would surface
	// the HasOwner shared contract.
	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	expectedByType := map[string]int{}
	total := 0
	for _, fx := range interfacePagingTypes {
		n := seedInterfacePagingType(t, mgr, fx)
		expectedByType[fx.apiName] = n
		total += n
	}
	if got := expectedByType["customer"]; got != 91 {
		t.Fatalf("northwind customer fixture drift: expected 91 rows, got %d", got)
	}
	if got := expectedByType["employee"]; got != 9 {
		t.Fatalf("northwind employee fixture drift: expected 9 rows, got %d", got)
	}
	if got := expectedByType["supplier"]; got != 29 {
		t.Fatalf("northwind supplier fixture drift: expected 29 rows, got %d", got)
	}
	if total != 129 {
		t.Fatalf("expected 129 total rows across interface, got %d", total)
	}

	// Wire the handler with a PG-backed interface resolver so the executor
	// can translate interfaceBase -> implementing ObjectType apiNames using
	// the object_type_interfaces table we just seeded.
	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, nil, store)
	executor.SetInterfaceResolver(&pgInterfaceResolver{repo: repo, ontologyRID: ont.RID})
	handler := objectset.NewHandler(executor, mgr, store)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	// Page through the full HasOwner result at pageSize=7. 129 / 7 = 18
	// full pages plus one 3-row tail, so the loop MUST terminate in exactly
	// 19 iterations and visit every seeded object exactly once.
	const pageSize = 7
	seen := map[string]string{} // "apiName|pk" -> apiName
	perType := map[string]int{}
	var lastTotalCount string
	var lastAccuracy string
	pageToken := ""
	pageCount := 0
	const maxPages = 30
	for {
		if pageCount >= maxPages {
			t.Fatalf("paging did not terminate after %d iterations", maxPages)
		}
		body := map[string]interface{}{
			"objectSet": map[string]interface{}{
				"type":          "interfaceBase",
				"interfaceType": "HasOwner",
			},
			"select":   []string{"id", "name"},
			"pageSize": pageSize,
		}
		if pageToken != "" {
			body["pageToken"] = pageToken
		}
		rawBody, _ := json.Marshal(body)

		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v2/ontologies/northwind/objectSets/loadObjectsOrInterfaces?preview=true",
			bytes.NewReader(rawBody),
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("page %d: expected 200, got %d: %s", pageCount, rr.Code, rr.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("page %d: unmarshal: %v", pageCount, err)
		}

		data, ok := resp["data"].([]interface{})
		if !ok {
			t.Fatalf("page %d: expected data array, got %T", pageCount, resp["data"])
		}

		for _, raw := range data {
			item, ok := raw.(map[string]interface{})
			if !ok {
				t.Fatalf("page %d: expected row object, got %T", pageCount, raw)
			}
			pk, _ := item["$primaryKey"].(string)
			ap, _ := item["$apiName"].(string)
			if pk == "" || ap == "" {
				t.Fatalf("page %d: missing $primaryKey/$apiName on row: %+v", pageCount, item)
			}
			key := ap + "|" + pk
			if prior, dup := seen[key]; dup {
				t.Fatalf("page %d: duplicate row %s (prior apiName=%s)", pageCount, key, prior)
			}
			seen[key] = ap
			perType[ap]++

			// Wire-level shape sanity: $rid must be present and the selected
			// fields must round-trip.
			if _, ok := item["$rid"].(string); !ok {
				t.Errorf("page %d: row %s missing $rid", pageCount, key)
			}
			if _, ok := item["id"]; !ok {
				t.Errorf("page %d: row %s missing selected id field", pageCount, key)
			}
		}

		if tc, ok := resp["totalCount"].(string); ok {
			lastTotalCount = tc
		}
		if ta, ok := resp["totalCountAccuracy"].(string); ok {
			lastAccuracy = ta
		}

		nextToken, _ := resp["nextPageToken"].(string)
		pageCount++
		if nextToken == "" {
			// Final page: must hold the tail (<= pageSize) and not advance.
			if len(data) == 0 {
				t.Fatalf("page %d: final page returned zero rows", pageCount-1)
			}
			break
		}
		if len(data) != pageSize {
			t.Fatalf("non-final page %d returned %d rows, expected %d", pageCount-1, len(data), pageSize)
		}
		if nextToken == pageToken {
			t.Fatalf("page %d: nextPageToken did not advance", pageCount-1)
		}
		pageToken = nextToken
	}

	if len(seen) != total {
		t.Fatalf("expected %d unique rows, got %d (perType=%v)", total, len(seen), perType)
	}
	for _, fx := range interfacePagingTypes {
		if perType[fx.apiName] != expectedByType[fx.apiName] {
			t.Errorf("type %s: expected %d rows, got %d", fx.apiName, expectedByType[fx.apiName], perType[fx.apiName])
		}
	}

	// ceil(129 / 7) = 19 pages with a final tail of 3 rows.
	expectedPages := (total + pageSize - 1) / pageSize
	if pageCount != expectedPages {
		t.Errorf("expected %d pages, got %d", expectedPages, pageCount)
	}

	// totalCount is a string in Foundry's wire format; it must reflect the
	// full cross-type count and be stable across every page.
	if lastTotalCount != strconv.Itoa(total) {
		t.Errorf("totalCount: got %q, want %q", lastTotalCount, strconv.Itoa(total))
	}
	if lastAccuracy != "EXACT" {
		t.Errorf("totalCountAccuracy: got %q, want %q", lastAccuracy, "EXACT")
	}
}

// seedInterfacePagingType ensures the Bleve index for fx.apiName exists and
// populates it by projecting each row of the Northwind CSV onto ("id", "name")
// so a single select list works across every implementing ObjectType.
// Returns the number of indexed rows so the caller can derive per-type
// expectations without re-reading the CSV.
func seedInterfacePagingType(t *testing.T, mgr *index.Manager, fx interfacePagingType) int {
	t.Helper()

	props := []index.Property{
		{APIName: "id", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
	}
	if _, err := mgr.EnsureIndex(fx.apiName, props); err != nil {
		t.Fatalf("ensure index %q: %v", fx.apiName, err)
	}

	f, err := os.Open(northwindFixturePath(t, fx.csvFile))
	if err != nil {
		t.Fatalf("open %s: %v", fx.csvFile, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read %s: %v", fx.csvFile, err)
	}
	if len(records) < 2 {
		t.Fatalf("%s has no data rows", fx.csvFile)
	}

	header := records[0]
	pkIdx, nameIdx := -1, -1
	for i, col := range header {
		if col == fx.pkCol {
			pkIdx = i
		}
		if col == fx.nameCol {
			nameIdx = i
		}
	}
	if pkIdx < 0 || nameIdx < 0 {
		t.Fatalf("%s missing %q or %q column", fx.csvFile, fx.pkCol, fx.nameCol)
	}

	count := 0
	for _, row := range records[1:] {
		if pkIdx >= len(row) {
			continue
		}
		// Namespace the document id with the type apiName so employee "1"
		// and supplier "1" don't collide in the shared paging map — the
		// heap merge then resolves the tie by ObjectType name, matching
		// the deterministic ordering documented in the US-007 parity
		// fixture.
		pk := fx.apiName + ":" + row[pkIdx]
		name := ""
		if nameIdx < len(row) {
			name = row[nameIdx]
		}
		doc := map[string]interface{}{
			"id":   pk,
			"name": name,
		}
		if err := mgr.IndexDocument(fx.apiName, pk, doc); err != nil {
			t.Fatalf("index %s/%s: %v", fx.apiName, pk, err)
		}
		count++
	}
	return count
}

// northwindFixturePath locates a CSV fixture under testdata/northwind from
// wherever `go test` was invoked. The integration package lives two levels
// below the repo root, but the go test working directory can be either the
// package dir or the module root depending on how the suite was launched.
func northwindFixturePath(t *testing.T, name string) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "testdata", "northwind", name),
		filepath.Join("..", "testdata", "northwind", name),
		filepath.Join("testdata", "northwind", name),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatalf("northwind fixture %q not found (tried %v)", name, candidates)
	return ""
}
