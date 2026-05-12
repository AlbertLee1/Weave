//go:build bdd

package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/cellsec"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/masking"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// registerCellMaskingSteps wires the US-016 cell_masking_cel feature's
// step regex onto the scenario context. The harness drives the real
// chi-routed OSS GetObject endpoint against the cell-masking pipeline:
//
//   - pkg/oms.PGRepository owns ontology + objectType + property rows.
//   - pkg/index.Manager owns the Bleve per-(ontology, objectType) index.
//   - pkg/cellsec.MemoryStore + Engine evaluate the CEL Expression mask
//     per row.
//   - pkg/oss.ServiceImpl.GetObject runs the full read-path filter +
//     mask chain (applyPolicyFilter → applyMarkingFilter →
//     applyPropertyVisibility → applyColumnMasking → applyCellMasking).
//
// Assertions span three layers: HTTP status code, JSON response body
// property values (clear vs masked), and (implicitly) the underlying
// cellsec.Engine evaluation correctness — the body is the engine's
// observable contract on the wire.
func registerCellMaskingSteps(t testing.TB, sc *godog.ScenarioContext, state *suiteState) {
	// --- Given: seed ontology + customer ObjectType + 3 rows ---------

	sc.Given(
		`^the cell-masking ontology "([^"]+)" is seeded with one customer object type and three rows$`,
		func(ontologyAPIName string) error {
			if err := state.ensureContainer(t); err != nil {
				return err
			}
			return seedCellMaskingOntology(state, ontologyAPIName)
		},
	)

	// --- Given: install a CEL-expression cell mask ------------------

	sc.Given(
		`^a cell mask on "([^"]+)" "([^"]+)" "([^"]+)" property "([^"]+)" with strategy "([^"]+)" and expression '([^']+)'$`,
		func(ontologyAPIName, objectTypeAPIName, primaryKey, propertyAPIName, strategy, expression string) error {
			return installCellMaskExpression(state, ontologyAPIName, objectTypeAPIName,
				primaryKey, propertyAPIName, strategy, expression)
		},
	)
	// Allow double-quoted expression form too — single quotes inside Gherkin
	// are easier to author when the expression contains a literal " but the
	// Admin-Bypass scenario uses a bare "true" expression which reads more
	// naturally with double quotes.
	sc.Given(
		`^a cell mask on "([^"]+)" "([^"]+)" "([^"]+)" property "([^"]+)" with strategy "([^"]+)" and expression "([^"]+)"$`,
		func(ontologyAPIName, objectTypeAPIName, primaryKey, propertyAPIName, strategy, expression string) error {
			return installCellMaskExpression(state, ontologyAPIName, objectTypeAPIName,
				primaryKey, propertyAPIName, strategy, expression)
		},
	)

	// --- When: GET the object as a specific caller -------------------

	sc.When(
		`^user "([^"]+)" with roles "([^"]+)" reads "([^"]+)" "([^"]+)" from "([^"]+)"$`,
		func(userID, rolesCSV, objectTypeAPIName, primaryKey, ontologyAPIName string) error {
			roles := splitCSV(rolesCSV)
			caller := &auth.User{ID: userID, Roles: roles}
			ctx := auth.WithUser(context.Background(), caller)

			ontologyRID, ok := state.ontologyRIDFor(ontologyAPIName)
			if !ok {
				return fmt.Errorf("ontology %q not seeded — call the Background step first", ontologyAPIName)
			}

			req := httptest.NewRequest(http.MethodGet,
				"/api/v2/ontologies/"+ontologyRID+"/objects/"+objectTypeAPIName+"/"+primaryKey, nil)
			req = req.WithContext(ctx)
			rr := httptest.NewRecorder()
			state.cellMaskRouter.ServeHTTP(rr, req)
			state.lastCellMaskResponse = &cellMaskHTTPResult{
				statusCode: rr.Code,
				body:       rr.Body.Bytes(),
			}
			return nil
		},
	)

	// --- Then: HTTP layer + body assertions --------------------------

	sc.Then(`^the object GET HTTP status code is (\d+)$`, func(want int) error {
		if state.lastCellMaskResponse == nil {
			return errors.New("no cell-mask object response captured")
		}
		if state.lastCellMaskResponse.statusCode != want {
			return fmt.Errorf("object GET status code = %d, want %d; body=%s",
				state.lastCellMaskResponse.statusCode, want,
				state.lastCellMaskResponse.body)
		}
		return nil
	})

	sc.Then(`^the object response property "([^"]+)" equals "([^"]+)"$`,
		func(property, want string) error {
			if state.lastCellMaskResponse == nil {
				return errors.New("no cell-mask object response captured")
			}
			var body map[string]interface{}
			if err := json.Unmarshal(state.lastCellMaskResponse.body, &body); err != nil {
				return fmt.Errorf("decode object body: %w; body=%s",
					err, string(state.lastCellMaskResponse.body))
			}
			got, ok := body[property]
			if !ok {
				return fmt.Errorf("property %q absent from response body; keys=%v",
					property, keysOf(body))
			}
			gotStr, _ := got.(string)
			if gotStr != want {
				return fmt.Errorf("property %q = %v, want %q", property, got, want)
			}
			return nil
		})
}

// seedCellMaskingOntology lays down one ontology, one customer ObjectType
// with the four properties the feature exercises (customerId / name / ssn
// / country), and three rows (c1 = Alice/PII/US, c2 = Bob/-/CN, c3 =
// Charlie/-/JP). The rows are written to BOTH PG (so OMS resolution sees
// the ObjectType + properties) and the Bleve scoped index (so OSS reads
// can deserialise them on GetObject).
func seedCellMaskingOntology(state *suiteState, ontologyAPIName string) error {
	ctx := context.Background()

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     ontologyAPIName,
		DisplayName: "BDD US-016 Cell Masking",
	}
	if err := state.repo.CreateOntology(ctx, ont); err != nil {
		return fmt.Errorf("CreateOntology: %w", err)
	}
	state.rememberOntologyRID(ontologyAPIName, ont.RID)

	ot := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "customer",
		DisplayName: "Customer",
		PrimaryKey:  "customerId",
		PrimaryKeys: []string{"customerId"},
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := state.repo.CreateObjectType(ctx, ot); err != nil {
		return fmt.Errorf("CreateObjectType: %w", err)
	}
	state.rememberObjectTypeRID(ontologyAPIName, ot.APIName, ot.RID)

	props := []oms.Property{
		{APIName: "customerId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "ssn", BaseType: "string", IsSearchable: true},
		{APIName: "country", BaseType: "string", IsSearchable: true},
	}
	for _, p := range props {
		p.RID = rid.NewPropertyRID()
		p.ObjectTypeRID = ot.RID
		p.DisplayName = p.APIName
		p.Status = "ACTIVE"
		if err := state.repo.CreateProperty(ctx, &p); err != nil {
			return fmt.Errorf("CreateProperty(%s): %w", p.APIName, err)
		}
	}

	// Bleve index: build under the SCOPED key the OSS read path uses.
	scoped := index.ScopedKey(ont.RID, ot.APIName)
	indexProps := []index.Property{
		{APIName: "customerId", BaseType: "string", IsSearchable: true},
		{APIName: "name", BaseType: "string", IsSearchable: true},
		{APIName: "ssn", BaseType: "string", IsSearchable: true},
		{APIName: "country", BaseType: "string", IsSearchable: true},
	}
	if _, err := state.indexMgr.EnsureIndex(scoped, indexProps); err != nil {
		return fmt.Errorf("EnsureIndex(%s): %w", scoped, err)
	}
	docs := []struct {
		pk  string
		row map[string]interface{}
	}{
		{"c1", map[string]interface{}{"customerId": "c1", "name": "Alice", "ssn": "111-22-3333", "country": "US"}},
		{"c2", map[string]interface{}{"customerId": "c2", "name": "Bob", "ssn": "222-33-4444", "country": "CN"}},
		{"c3", map[string]interface{}{"customerId": "c3", "name": "Charlie", "ssn": "333-44-5555", "country": "JP"}},
	}
	for _, d := range docs {
		if err := state.indexMgr.IndexDocument(scoped, d.pk, d.row); err != nil {
			return fmt.Errorf("IndexDocument(%s/%s): %w", scoped, d.pk, err)
		}
	}
	// Bleve indexes asynchronously settle a small batch; the OSS tests use
	// the same 200ms grace window. Without it, the very next GetObject
	// against c1 occasionally returns ErrNotFound on a cold index.
	time.Sleep(200 * time.Millisecond)
	return nil
}

// installCellMaskExpression registers a CEL-expression cell mask via the
// MemoryStore and reloads the engine. The mask targets one (objectType
// RID, primaryKey, propertyApiName) cell with the supplied MaskStrategy
// (REDACT / HASH / NULL / PARTIAL) and CEL predicate. Trim quotes
// defensively in case Gherkin's regex captures grabbed surrounding
// punctuation.
func installCellMaskExpression(state *suiteState, ontologyAPIName, objectTypeAPIName,
	primaryKey, propertyAPIName, strategy, expression string,
) error {
	otRID, ok := state.objectTypeRIDFor(ontologyAPIName, objectTypeAPIName)
	if !ok {
		return fmt.Errorf("objectType %q not seeded under ontology %q",
			objectTypeAPIName, ontologyAPIName)
	}
	mask := &cellsec.CellMask{
		RID:             rid.New("cellsec", "main", "cell-mask"),
		ObjectTypeRID:   otRID,
		PrimaryKey:      primaryKey,
		PropertyAPIName: propertyAPIName,
		MaskStrategy:    masking.MaskStrategy(strings.TrimSpace(strategy)),
		Expression:      strings.TrimSpace(expression),
		AppliesTo:       masking.AppliesTo{},
	}
	if err := state.cellMaskStore.Create(context.Background(), mask); err != nil {
		return fmt.Errorf("cellMaskStore.Create: %w", err)
	}
	if err := state.cellMaskEngine.Reload(context.Background()); err != nil {
		return fmt.Errorf("cellMaskEngine.Reload: %w", err)
	}
	return nil
}

// splitCSV splits a comma-separated list of role strings, trimming
// surrounding whitespace. Used so feature files can write
// 'roles "viewer, finance"' without worrying about literal spaces.
func splitCSV(in string) []string {
	parts := strings.Split(in, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// keysOf returns the top-level JSON object keys for an error diagnostic.
func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
