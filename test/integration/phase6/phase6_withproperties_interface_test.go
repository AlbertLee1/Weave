//go:build integration

package phase6_test

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
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
	"github.com/liyang/weave/pkg/oss/pagination"
	"github.com/liyang/weave/pkg/rid"
)

// pgInterfaceResolver mirrors the helper in test/integration so the Phase 6
// suite can run independently. Walks the PG-backed OMS to translate an
// interface API name into its implementing ObjectType API names.
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

// perPKLinkResolver returns per-source-PK target lists for a named link. It
// ignores source ObjectType because the mock data maps link API names onto a
// single flat edge set — good enough to drive withProperties count metrics.
type perPKLinkResolver struct {
	edges map[string]map[string][]string
}

func (m *perPKLinkResolver) ResolveLinked(ctx context.Context, linkTypeKey string, pks []string, dir links.Direction) ([]string, error) {
	edges := m.edges[linkTypeKey]
	var out []string
	for _, pk := range pks {
		out = append(out, edges[pk]...)
	}
	return out, nil
}

func (m *perPKLinkResolver) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	return m.ResolveLinked(ctx, linkTypeRID, sourcePKs, links.DirectionForward)
}

func (m *perPKLinkResolver) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceOTAPIName, linkAPIName string, sourcePKs []string) ([]string, error) {
	return m.ResolveLinked(ctx, linkAPIName, sourcePKs, links.DirectionForward)
}

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

// TestWithPropertiesInterface_HasOwnerReportCount combines Phase 6's two
// orthogonal features — withProperties derived values and interfaceBase
// polymorphic paging — into one end-to-end scenario. It seeds the Northwind
// HasOwner interface (customer + employee + supplier, 129 rows), attaches a
// synthetic `reportCount` derived property via a mock link resolver, and then
// pages the polymorphic result through loadObjectsOrInterfaces at pageSize=13.
//
// Assertions:
//   - every emitted row carries an int64 reportCount
//   - expected non-zero coverage: at least half of the seeded rows have
//     reportCount > 0
//   - all 129 rows are visited exactly once across the page iteration
//   - the nextPageToken is a MultiTypeCursor (interface composite form), not
//     the flat-offset Cursor used by non-polymorphic preview responses
//   - totalCount reflects the full cross-type count and stays stable
func TestWithPropertiesInterface_HasOwnerReportCount(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := oms.NewPGRepository(pg.Pool)

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

	tmpDir := t.TempDir()
	mgr := index.NewManager(tmpDir)
	t.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			t.Logf("index manager close: %v", err)
		}
	})

	// Index each implementing type and remember every indexed PK so the mock
	// link resolver can fabricate a deterministic number of outgoing edges
	// per base object. The same reportCount distribution is applied to all
	// three types so the per-type buckets populate the composite cursor.
	var allIndexedPKs []string
	perTypePKs := map[string][]string{}
	for _, fx := range interfacePagingTypes {
		pks := seedInterfacePagingType(t, mgr, fx)
		perTypePKs[fx.apiName] = pks
		allIndexedPKs = append(allIndexedPKs, pks...)
	}
	if total := len(allIndexedPKs); total < 60 {
		t.Fatalf("need ≥ 60 seeded rows to exercise paging, got %d", total)
	}
	if got := len(perTypePKs["customer"]); got != 91 {
		t.Fatalf("northwind customer fixture drift: expected 91 rows, got %d", got)
	}
	if got := len(perTypePKs["employee"]); got != 9 {
		t.Fatalf("northwind employee fixture drift: expected 9 rows, got %d", got)
	}
	if got := len(perTypePKs["supplier"]); got != 29 {
		t.Fatalf("northwind supplier fixture drift: expected 29 rows, got %d", got)
	}

	// Deterministic reportCount plan: every 2nd base row gets 1 target, every
	// 3rd gets 2 targets, every 5th gets 3 targets, the rest get 0. Exact
	// counts depend on the ordering of allIndexedPKs but each type sees a
	// mix of zero and non-zero buckets so the test exercises the empty-link
	// path as well as the non-empty path within a single paging run.
	edges := map[string]map[string][]string{"ownerReports": {}}
	nonZero := 0
	for i, pk := range allIndexedPKs {
		count := 0
		if i%2 == 0 {
			count++
		}
		if i%3 == 0 {
			count++
		}
		if i%5 == 0 {
			count++
		}
		if count == 0 {
			continue
		}
		targets := make([]string, 0, count)
		for k := 0; k < count; k++ {
			targets = append(targets, fmt.Sprintf("rpt-%s-%d", pk, k))
		}
		edges["ownerReports"][pk] = targets
		nonZero++
	}
	if nonZero < len(allIndexedPKs)/2 {
		t.Fatalf("reportCount fixture too sparse: only %d/%d rows have non-zero reports", nonZero, len(allIndexedPKs))
	}

	linkResolver := &perPKLinkResolver{edges: edges}

	store := objectset.NewStore(1 * time.Hour)
	executor := objectset.NewExecutor(mgr, linkResolver, store)
	executor.SetInterfaceResolver(&pgInterfaceResolver{repo: repo, ontologyRID: ont.RID})
	handler := objectset.NewHandler(executor, mgr, store)

	router := chi.NewRouter()
	router.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces", handler.LoadObjectsOrInterfaces)

	const pageSize = 13
	seen := map[string]string{}
	perTypeSeen := map[string]int{}
	nonZeroSeen := 0
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
				"type": "withProperties",
				"objectSet": map[string]interface{}{
					"type":          "interfaceBase",
					"interfaceType": "HasOwner",
				},
				"derivedProperties": []map[string]interface{}{
					{
						"name":      "reportCount",
						"link":      "ownerReports",
						"direction": "forward",
						"metric":    "count",
					},
				},
			},
			"select":   []string{"id", "name", "reportCount"},
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
			perTypeSeen[ap]++

			rcRaw, present := item["reportCount"]
			if !present {
				t.Fatalf("page %d: row %s missing reportCount derived property: %+v", pageCount, key, item)
			}
			rcFloat, ok := rcRaw.(float64)
			if !ok {
				t.Fatalf("page %d: row %s reportCount: want float64 (from JSON), got %T (%v)", pageCount, key, rcRaw, rcRaw)
			}
			if rcFloat < 0 {
				t.Errorf("page %d: row %s negative reportCount %v", pageCount, key, rcFloat)
			}
			if rcFloat > 0 {
				nonZeroSeen++
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
			if len(data) == 0 {
				t.Fatalf("page %d: final page returned zero rows", pageCount-1)
			}
			break
		}

		// The token MUST be a MultiTypeCursor (composite, per-sub-type). If
		// DecodeMultiTypeCursor fails or returns zero sub-cursors, the
		// handler fell through to the flat-offset path — which would break
		// polymorphic paging entirely once derived values enter the picture.
		mc, err := pagination.DecodeMultiTypeCursor(nextToken)
		if err != nil {
			t.Fatalf("page %d: nextPageToken is not a MultiTypeCursor: %v (raw=%q)", pageCount-1, err, nextToken)
		}
		if len(mc.SubCursors) == 0 {
			t.Fatalf("page %d: MultiTypeCursor had zero live sub-cursors but token was non-empty", pageCount-1)
		}
		if len(data) != pageSize {
			t.Fatalf("non-final page %d returned %d rows, expected %d", pageCount-1, len(data), pageSize)
		}
		if nextToken == pageToken {
			t.Fatalf("page %d: nextPageToken did not advance", pageCount-1)
		}
		pageToken = nextToken
	}

	total := len(allIndexedPKs)
	if len(seen) != total {
		t.Fatalf("expected %d unique rows across pagination, got %d (perType=%v)", total, len(seen), perTypeSeen)
	}
	for _, fx := range interfacePagingTypes {
		if perTypeSeen[fx.apiName] != len(perTypePKs[fx.apiName]) {
			t.Errorf("type %s: expected %d rows, got %d", fx.apiName, len(perTypePKs[fx.apiName]), perTypeSeen[fx.apiName])
		}
	}
	if nonZeroSeen < total/2 {
		t.Errorf("expected ≥ %d rows with non-zero reportCount, got %d", total/2, nonZeroSeen)
	}
	if lastTotalCount != strconv.Itoa(total) {
		t.Errorf("totalCount: got %q, want %q", lastTotalCount, strconv.Itoa(total))
	}
	if lastAccuracy != "EXACT" {
		t.Errorf("totalCountAccuracy: got %q, want %q", lastAccuracy, "EXACT")
	}
}

// seedInterfacePagingType populates a Bleve index for fx.apiName from the
// Northwind CSV and returns the full list of indexed primary keys so the
// caller can build deterministic link-resolver edges on top. PKs are
// namespaced as "<apiName>:<row>" so employee "1" and supplier "1" do not
// collide in the shared paging map.
func seedInterfacePagingType(t *testing.T, mgr *index.Manager, fx interfacePagingType) []string {
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

	pks := make([]string, 0, len(records)-1)
	for _, row := range records[1:] {
		if pkIdx >= len(row) {
			continue
		}
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
		pks = append(pks, pk)
	}
	return pks
}

// northwindFixturePath locates a Northwind CSV from a Phase 6 test package.
// `go test` may run from either the package directory or the repo root, so
// all likely relative paths are tried.
func northwindFixturePath(t *testing.T, name string) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "..", "testdata", "northwind", name),
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
