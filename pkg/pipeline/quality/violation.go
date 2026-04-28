package quality

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Violation is one row that failed a Rule. The wire shape mirrors the
// quality_violations PG table column-for-column (US-296 migration
// 000071) so the PG store is a thin INSERT and the in-memory store
// stays trivial. JSON tags are camelCase to match the rest of the
// HTTP API surface.
type Violation struct {
	ID         string    `json:"id"`
	PipelineID string    `json:"pipelineId,omitempty"`
	RunID      string    `json:"runId,omitempty"`
	NodeName   string    `json:"nodeName,omitempty"`
	RuleName   string    `json:"ruleName"`
	RuleType   RuleType  `json:"ruleType"`
	Field      string    `json:"field,omitempty"`
	RowIndex   int64     `json:"rowIndex"`
	RowKey     string    `json:"rowKey,omitempty"`
	Reason     string    `json:"reason"`
	Value      string    `json:"value,omitempty"`
	DetectedAt time.Time `json:"detectedAt"`
}

// stringifyValue renders v in the canonical TEXT form persisted in
// quality_violations.value. Mirrors the AIP tool stringifier (US-285)
// so admin tooling that reads both surfaces sees identical formatting:
//
//   - nil / absent → "" (the column DEFAULT ”)
//   - string passthrough
//   - int / float / bool via strconv (no JSON quoting)
//   - everything else via json.Marshal, fallback to fmt.Sprintf("%v")
//
// Specific-before-general ordering matters: int / float64 / bool are
// checked before the JSON fallback so 42 becomes "42" not "\"42\"".
func stringifyValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return string(x)
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}
