package aggregation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oss/where"
)

func TestBDD_SELF605_EngineRejectsRegexWhereBeforeSearch(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	_, err := eng.Aggregate(idx, &AggregationRequest{
		Where: &where.WhereClause{
			Type:  "regex",
			Field: "department",
			Value: json.RawMessage(`"eng.*"`),
		},
		Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
	})
	if err == nil {
		t.Fatal("Aggregate returned nil error, want regex where rejection before Bleve search")
	}
	if !strings.Contains(err.Error(), "regex") {
		t.Fatalf("error = %q, want it to mention regex", err.Error())
	}
}
