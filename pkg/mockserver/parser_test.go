package mockserver

import (
	"strings"
	"testing"
)

const sampleSpec = `
openapi: 3.0.3
info:
  title: Sample
  version: 1.0.0
paths:
  /health:
    get:
      operationId: getHealth
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/HealthResponse"
  /api/v2/ontologies/{ontologyApiName}:
    get:
      operationId: getOntology
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Ontology"
        "404":
          $ref: "#/components/responses/NotFoundError"
    delete:
      operationId: deleteOntology
      responses:
        "204":
          description: deleted
  /api/v2/ontologies/{ontologyApiName}/widgets:
    post:
      operationId: createWidget
      responses:
        "201":
          description: created
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Widget"
components:
  schemas:
    HealthResponse:
      type: object
      properties:
        status:
          type: string
          example: ok
    Ontology:
      type: object
      required: [rid, apiName, currentVersion]
      properties:
        rid:
          type: string
          example: ri.ontology.main.ontology.northwind
        apiName:
          type: string
          example: northwind
        currentVersion:
          type: integer
        tags:
          type: array
          items:
            type: string
        active:
          type: boolean
    Widget:
      type: object
      properties:
        id:
          type: string
        kind:
          type: string
          enum: [primary, secondary]
    ErrorResponse:
      type: object
      properties:
        errorCode:
          type: string
          example: NOT_FOUND
  responses:
    NotFoundError:
      description: Missing.
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/ErrorResponse"
`

func TestParseSpec_ExtractsOperations(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleSpec))
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}

	want := map[string]int{
		"GET /health":                                       200,
		"GET /api/v2/ontologies/{ontologyApiName}":          200,
		"DELETE /api/v2/ontologies/{ontologyApiName}":       204,
		"POST /api/v2/ontologies/{ontologyApiName}/widgets": 201,
	}
	if len(spec.Operations) != len(want) {
		t.Fatalf("operation count = %d, want %d (got %#v)", len(spec.Operations), len(want), spec.Operations)
	}
	for _, op := range spec.Operations {
		key := op.Method + " " + op.Path
		w, ok := want[key]
		if !ok {
			t.Fatalf("unexpected operation %s", key)
		}
		if op.Status != w {
			t.Errorf("%s status = %d, want %d", key, op.Status, w)
		}
	}
}

func TestParseSpec_PicksLowest2xxStatus(t *testing.T) {
	const spec = `
openapi: 3.0.3
info:
  title: t
  version: 1.0.0
paths:
  /x:
    get:
      operationId: getX
      responses:
        "204":
          description: empty
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
`
	parsed, err := ParseSpec([]byte(spec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Operations) != 1 {
		t.Fatalf("operations = %d", len(parsed.Operations))
	}
	if parsed.Operations[0].Status != 200 {
		t.Errorf("status = %d, want 200 (lowest 2xx wins over 204)", parsed.Operations[0].Status)
	}
}

func TestParseSpec_RejectsInvalidYAML(t *testing.T) {
	// Unbalanced bracket; valid YAML parsers reject this outright.
	if _, err := ParseSpec([]byte("openapi: 3.0\npaths: {")); err == nil {
		t.Fatal("expected error parsing malformed spec")
	}
}

func TestParseSpec_RejectsMissingPaths(t *testing.T) {
	if _, err := ParseSpec([]byte("openapi: 3.0.3\ninfo:\n  title: t\n  version: 1.0.0\n")); err == nil {
		t.Fatal("expected error when paths missing")
	} else if !strings.Contains(err.Error(), "paths") {
		t.Errorf("error %q should mention paths", err)
	}
}
