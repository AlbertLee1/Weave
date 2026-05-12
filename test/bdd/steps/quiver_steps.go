//go:build bdd

package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rid"
)

// registerQuiverSteps wires the US-018 quiver_aggregation feature's step
// regex onto the scenario context. The harness drives the real
// chi-routed OSS AggregateObjects endpoint against three concrete
// Quiver-style panels:
//
//   - Sum-by-1h: groupBy "duration" {HOURS, 1} on a timestamp field +
//     sum metric on a numeric value field.
//   - Percentile: approximatePercentile with [p50, p95, p99] in
//     AccuracyRequireAccurate mode so the response is byte-exact.
//   - Cardinality: approximateDistinct on a keyword host field in
//     AccuracyRequireAccurate mode so exactDistinct runs under the
//     same metric name (the engine promotes approximate→exact
//     transparently).
//
// Assertions span three layers: HTTP status code, response data shape
// (bucket count / row count), and the metric values themselves (sum,
// percentile map[string]float64, distinct count).
func registerQuiverSteps(t testing.TB, sc *godog.ScenarioContext, state *suiteState) {
	// --- Given: seed ontology + metric_point ObjectType + 6 rows ----

	sc.Given(
		`^the quiver ontology "([^"]+)" is seeded with one metric_point object type and six time-series rows$`,
		func(ontologyAPIName string) error {
			if err := state.ensureContainer(t); err != nil {
				return err
			}
			return seedQuiverOntology(state, ontologyAPIName)
		},
	)

	// --- When: aggregate sum-by-1h ----------------------------------

	sc.When(
		`^the analyst aggregates "([^"]+)" "([^"]+)" with sum on "([^"]+)" bucketed by (\d+) hour on "([^"]+)"$`,
		func(ontologyAPIName, objectTypeAPIName, sumField string,
			hours int, timestampField string,
		) error {
			body := map[string]interface{}{
				"accuracy": "REQUIRE_ACCURATE",
				"aggregation": []map[string]interface{}{
					{"type": "sum", "field": sumField},
				},
				"groupBy": []map[string]interface{}{
					{
						"type":  "duration",
						"field": timestampField,
						"value": map[string]interface{}{
							"unit":  "HOURS",
							"value": hours,
						},
					},
				},
			}
			return postAggregate(state, ontologyAPIName, objectTypeAPIName, body)
		},
	)

	// --- When: aggregate exact percentile ---------------------------

	sc.When(
		`^the analyst aggregates "([^"]+)" "([^"]+)" exact percentile on "([^"]+)" at "([^"]+)"$`,
		func(ontologyAPIName, objectTypeAPIName, field, percentilesCSV string) error {
			pcts, err := parsePercentileList(percentilesCSV)
			if err != nil {
				return err
			}
			body := map[string]interface{}{
				"accuracy": "REQUIRE_ACCURATE",
				"aggregation": []map[string]interface{}{
					{
						"type":        "approximatePercentile",
						"field":       field,
						"percentiles": pcts,
					},
				},
			}
			return postAggregate(state, ontologyAPIName, objectTypeAPIName, body)
		},
	)

	// --- When: aggregate exact distinct (cardinality) ---------------

	sc.When(
		`^the analyst aggregates "([^"]+)" "([^"]+)" exact distinct on "([^"]+)"$`,
		func(ontologyAPIName, objectTypeAPIName, field string) error {
			body := map[string]interface{}{
				"accuracy": "REQUIRE_ACCURATE",
				"aggregation": []map[string]interface{}{
					{"type": "approximateDistinct", "field": field},
				},
			}
			return postAggregate(state, ontologyAPIName, objectTypeAPIName, body)
		},
	)

	// --- Then: HTTP status code -------------------------------------

	sc.Then(`^the aggregate HTTP status code is (\d+)$`, func(want int) error {
		if state.lastQuiverResponse == nil {
			return errors.New("no aggregate response captured")
		}
		if state.lastQuiverResponse.statusCode != want {
			return fmt.Errorf("aggregate status code = %d, want %d; body=%s",
				state.lastQuiverResponse.statusCode, want,
				state.lastQuiverResponse.body)
		}
		return nil
	})

	// --- Then: response data shape ----------------------------------

	sc.Then(`^the aggregate response has (\d+) buckets$`, func(want int) error {
		data, err := decodeAggregateData(state)
		if err != nil {
			return err
		}
		if len(data) != want {
			return fmt.Errorf("aggregate data length = %d, want %d; body=%s",
				len(data), want, state.lastQuiverResponse.body)
		}
		return nil
	})

	sc.Then(`^the aggregate response has (\d+) row$`, func(want int) error {
		data, err := decodeAggregateData(state)
		if err != nil {
			return err
		}
		if len(data) != want {
			return fmt.Errorf("aggregate data length = %d, want %d; body=%s",
				len(data), want, state.lastQuiverResponse.body)
		}
		return nil
	})

	// --- Then: per-bucket sum metric assertion ----------------------

	sc.Then(
		`^the aggregate bucket "([^"]+)" sum metric "([^"]+)" equals (-?\d+(?:\.\d+)?)$`,
		func(bucketKey, metricName string, want float64) error {
			data, err := decodeAggregateData(state)
			if err != nil {
				return err
			}
			for _, row := range data {
				key, _ := row.Group["timestamp"].(string)
				if key != bucketKey {
					continue
				}
				got, ok := findAggMetric(row.Metrics, metricName)
				if !ok {
					return fmt.Errorf("metric %q missing on bucket %q; metrics=%v",
						metricName, bucketKey, row.Metrics)
				}
				gotF, ok := toFloat64(got)
				if !ok {
					return fmt.Errorf("metric %q on bucket %q not numeric: %v (%T)",
						metricName, bucketKey, got, got)
				}
				if math.Abs(gotF-want) > 1e-6 {
					return fmt.Errorf("metric %q on bucket %q = %v, want %v",
						metricName, bucketKey, gotF, want)
				}
				return nil
			}
			keys := make([]string, 0, len(data))
			for _, row := range data {
				keys = append(keys, fmt.Sprintf("%v", row.Group["timestamp"]))
			}
			return fmt.Errorf("bucket %q absent from response; got %v", bucketKey, keys)
		},
	)

	// --- Then: per-row percentile metric assertion -------------------

	sc.Then(
		`^the aggregate row (\d+) percentile metric "([^"]+)" at "([^"]+)" equals (-?\d+(?:\.\d+)?)$`,
		func(rowIdx int, metricName, pctKey string, want float64) error {
			data, err := decodeAggregateData(state)
			if err != nil {
				return err
			}
			if rowIdx < 0 || rowIdx >= len(data) {
				return fmt.Errorf("row index %d out of range [0,%d); body=%s",
					rowIdx, len(data), state.lastQuiverResponse.body)
			}
			got, ok := findAggMetric(data[rowIdx].Metrics, metricName)
			if !ok {
				return fmt.Errorf("metric %q missing on row %d; metrics=%v",
					metricName, rowIdx, data[rowIdx].Metrics)
			}
			pctMap, ok := got.(map[string]interface{})
			if !ok {
				return fmt.Errorf("metric %q on row %d not a percentile map: %v (%T)",
					metricName, rowIdx, got, got)
			}
			rawPct, ok := pctMap[pctKey]
			if !ok {
				keys := make([]string, 0, len(pctMap))
				for k := range pctMap {
					keys = append(keys, k)
				}
				return fmt.Errorf("percentile %q missing on metric %q row %d; keys=%v",
					pctKey, metricName, rowIdx, keys)
			}
			gotF, ok := toFloat64(rawPct)
			if !ok {
				return fmt.Errorf("percentile %q on metric %q row %d not numeric: %v (%T)",
					pctKey, metricName, rowIdx, rawPct, rawPct)
			}
			if math.Abs(gotF-want) > 1e-6 {
				return fmt.Errorf("percentile %q on metric %q row %d = %v, want %v",
					pctKey, metricName, rowIdx, gotF, want)
			}
			return nil
		},
	)

	// --- Then: scalar row metric assertion --------------------------

	sc.Then(
		`^the aggregate row (\d+) metric "([^"]+)" equals (-?\d+(?:\.\d+)?)$`,
		func(rowIdx int, metricName string, want float64) error {
			data, err := decodeAggregateData(state)
			if err != nil {
				return err
			}
			if rowIdx < 0 || rowIdx >= len(data) {
				return fmt.Errorf("row index %d out of range [0,%d); body=%s",
					rowIdx, len(data), state.lastQuiverResponse.body)
			}
			got, ok := findAggMetric(data[rowIdx].Metrics, metricName)
			if !ok {
				return fmt.Errorf("metric %q missing on row %d; metrics=%v",
					metricName, rowIdx, data[rowIdx].Metrics)
			}
			gotF, ok := toFloat64(got)
			if !ok {
				return fmt.Errorf("metric %q on row %d not numeric: %v (%T)",
					metricName, rowIdx, got, got)
			}
			if math.Abs(gotF-want) > 1e-6 {
				return fmt.Errorf("metric %q on row %d = %v, want %v",
					metricName, rowIdx, gotF, want)
			}
			return nil
		},
	)
}

// quiverMetric mirrors aggregation.MetricValue but with a JSON-friendly
// interface{} Value so step assertions can branch on the dynamic type
// (number, percentile map, distinct count) without importing the
// aggregation package's typed sum-result helpers.
type quiverMetric struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// quiverRow mirrors aggregation.AggregationRow on the wire.
type quiverRow struct {
	Group   map[string]interface{} `json:"group"`
	Metrics []quiverMetric         `json:"metrics"`
}

// quiverResponse mirrors the subset of aggregation.AggregationResponse the
// US-018 BDD scenarios need. Untyped because the aggregation package's
// MetricValue.Value is an empty interface that JSON-decodes into one of
// (float64, map[string]interface{}, string) depending on the metric type.
type quiverResponse struct {
	Data []quiverRow `json:"data"`
}

// decodeAggregateData returns the data rows from the most recent
// aggregate response or a diagnostic error if the response is missing
// or malformed. Centralised so every Then-step shares one decode pass.
func decodeAggregateData(state *suiteState) ([]quiverRow, error) {
	if state.lastQuiverResponse == nil {
		return nil, errors.New("no aggregate response captured")
	}
	var resp quiverResponse
	if err := json.Unmarshal(state.lastQuiverResponse.body, &resp); err != nil {
		return nil, fmt.Errorf("decode aggregate body: %w; body=%s",
			err, string(state.lastQuiverResponse.body))
	}
	return resp.Data, nil
}

// findAggMetric is the wire-shape counterpart of findMetric in the
// aggregation tests — values are JSON-decoded so the type assertion is
// done at the call site rather than here.
func findAggMetric(metrics []quiverMetric, name string) (interface{}, bool) {
	for _, m := range metrics {
		if m.Name == name {
			return m.Value, true
		}
	}
	return nil, false
}

// toFloat64 normalises numeric values JSON-decoded as either float64
// (Go's default for JSON numbers) or json.Number / int (rare paths).
// Returns ok=false for non-numeric inputs.
func toFloat64(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// parsePercentileList parses a CSV list of percentile values ("50,95,99")
// into []float64. Empty entries are skipped so leading/trailing commas
// are tolerated; out-of-range values are surfaced as a step error.
func parsePercentileList(csv string) ([]float64, error) {
	parts := strings.Split(csv, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("parse percentile %q: %w", s, err)
		}
		if f < 0 || f > 100 {
			return nil, fmt.Errorf("percentile %v out of range [0,100]", f)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, errors.New("percentile list is empty")
	}
	return out, nil
}

// postAggregate marshals body, POSTs it through the quiver chi router,
// and stashes the response on suiteState for Then-step assertions.
func postAggregate(state *suiteState, ontologyAPIName, objectTypeAPIName string,
	body map[string]interface{},
) error {
	ontologyRID, ok := state.ontologyRIDFor(ontologyAPIName)
	if !ok {
		return fmt.Errorf("ontology %q not seeded — call the Background step first",
			ontologyAPIName)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal aggregate body: %w", err)
	}
	url := "/api/v2/ontologies/" + ontologyRID +
		"/objects/" + objectTypeAPIName + "/aggregate"
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	state.quiverRouter.ServeHTTP(rr, req)
	state.lastQuiverResponse = &quiverHTTPResult{
		statusCode: rr.Code,
		body:       rr.Body.Bytes(),
	}
	return nil
}

// seedQuiverOntology lays down one ontology, one metric_point ObjectType
// with the four properties the feature exercises (pointId / timestamp /
// value / host), and six time-series rows distributed across three
// 1-hour buckets and three distinct hosts:
//
//   - p1: 2026-05-13T00:15:00Z, value=10, host=host-a   (bucket 00:00)
//   - p2: 2026-05-13T00:45:00Z, value=20, host=host-b   (bucket 00:00)
//   - p3: 2026-05-13T01:10:00Z, value=30, host=host-a   (bucket 01:00)
//   - p4: 2026-05-13T01:30:00Z, value=40, host=host-c   (bucket 01:00)
//   - p5: 2026-05-13T02:05:00Z, value=50, host=host-b   (bucket 02:00)
//   - p6: 2026-05-13T02:50:00Z, value=60, host=host-a   (bucket 02:00)
//
// Values sorted ascending are [10,20,30,40,50,60] so the exact-percentile
// (nearest-rank) assertions land on deterministic targets: p50 = 30,
// p95 = 60, p99 = 60. The hosts {host-a, host-b, host-c} give a distinct
// cardinality of 3.
func seedQuiverOntology(state *suiteState, ontologyAPIName string) error {
	ctx := context.Background()

	ont := &oms.Ontology{
		RID:         rid.NewOntologyRID(),
		APIName:     ontologyAPIName,
		DisplayName: "BDD US-018 Quiver Aggregation",
	}
	if err := state.repo.CreateOntology(ctx, ont); err != nil {
		return fmt.Errorf("CreateOntology: %w", err)
	}
	state.rememberOntologyRID(ontologyAPIName, ont.RID)

	ot := &oms.ObjectType{
		RID:         rid.NewObjectTypeRID(),
		OntologyRID: ont.RID,
		APIName:     "metric_point",
		DisplayName: "Metric Point",
		PrimaryKey:  "pointId",
		PrimaryKeys: []string{"pointId"},
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	}
	if err := state.repo.CreateObjectType(ctx, ot); err != nil {
		return fmt.Errorf("CreateObjectType: %w", err)
	}
	state.rememberObjectTypeRID(ontologyAPIName, ot.APIName, ot.RID)

	props := []oms.Property{
		{APIName: "pointId", BaseType: "string", IsSearchable: true},
		{APIName: "timestamp", BaseType: "timestamp", IsSearchable: true},
		{APIName: "value", BaseType: "double", IsSearchable: true},
		{APIName: "host", BaseType: "string", IsSearchable: true,
			TypeConfig: []byte(`{"analyzer":"not_analyzed"}`)},
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

	scoped := index.ScopedKey(ont.RID, ot.APIName)
	indexProps := []index.Property{
		{APIName: "pointId", BaseType: "string", IsSearchable: true},
		{APIName: "timestamp", BaseType: "timestamp", IsSearchable: true},
		{APIName: "value", BaseType: "double", IsSearchable: true},
		{APIName: "host", BaseType: "string", IsSearchable: true, Analyzer: "not_analyzed"},
	}
	if _, err := state.indexMgr.EnsureIndex(scoped, indexProps); err != nil {
		return fmt.Errorf("EnsureIndex(%s): %w", scoped, err)
	}

	docs := []struct {
		pk        string
		timestamp string
		value     float64
		host      string
	}{
		{"p1", "2026-05-13T00:15:00Z", 10, "host-a"},
		{"p2", "2026-05-13T00:45:00Z", 20, "host-b"},
		{"p3", "2026-05-13T01:10:00Z", 30, "host-a"},
		{"p4", "2026-05-13T01:30:00Z", 40, "host-c"},
		{"p5", "2026-05-13T02:05:00Z", 50, "host-b"},
		{"p6", "2026-05-13T02:50:00Z", 60, "host-a"},
	}
	for _, d := range docs {
		row := map[string]interface{}{
			"pointId":   d.pk,
			"timestamp": d.timestamp,
			"value":     d.value,
			"host":      d.host,
		}
		if err := state.indexMgr.IndexDocument(scoped, d.pk, row); err != nil {
			return fmt.Errorf("IndexDocument(%s/%s): %w", scoped, d.pk, err)
		}
	}
	// Bleve indexes asynchronously settle a small batch; the OSS tests use
	// the same 200ms grace window. Without it, the very next aggregate
	// search occasionally returns a stale facet on a cold index.
	time.Sleep(200 * time.Millisecond)
	return nil
}
