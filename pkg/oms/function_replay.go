package oms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// HashFunctionCode is the canonical hash of a Function's source code at
// publish time (US-370). The empty string hashes to the empty-string sha256
// digest, which is fine — replay never re-runs an empty body.
func HashFunctionCode(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

// HashFunctionSignature returns the sha256 hex of the canonical-JSON form
// of the signature. Empty / null / "{}" all collapse to the canonical empty
// object so legacy rows pre-US-215 round-trip to a stable hash regardless
// of which empty-shape the row was written with.
func HashFunctionSignature(raw json.RawMessage) string {
	canonical, err := canonicaliseSignature(raw)
	if err != nil {
		// Last-resort fallback so we never panic on a malformed row —
		// the hash is consistent for any (well-formed-or-not) byte
		// sequence so replay still works against the original write.
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// canonicaliseSignature renders a signature as canonical JSON: empty / null
// / "{}" inputs become the literal `{}`; otherwise the bytes are decoded
// and re-emitted with sorted map keys so two semantically identical
// signatures hash to the same value regardless of source ordering.
func canonicaliseSignature(raw json.RawMessage) ([]byte, error) {
	trimmed := trimASCIISpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "{}" {
		return []byte("{}"), nil
	}
	var anyVal interface{}
	if err := json.Unmarshal(trimmed, &anyVal); err != nil {
		return nil, err
	}
	return canonicaliseJSON(anyVal)
}

// HashFunctionInput hashes the (canonicalised) parameter map for replay
// equality. Keys are sorted, primitive numbers normalise via Go's standard
// JSON encoding so `1` and `1.0` collide in shape — same as the Foundry
// "request signature" notion.
func HashFunctionInput(input map[string]interface{}) string {
	if input == nil {
		input = map[string]interface{}{}
	}
	canonical, err := canonicaliseJSON(input)
	if err != nil {
		// Fallback: best-effort marshal then hash.
		raw, _ := json.Marshal(input)
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// HashFunctionOutput hashes a result value with the same canonical-JSON
// strategy as inputs. nil and "null" hash to the same value so a function
// that returns either is treated as deterministic.
func HashFunctionOutput(output interface{}) string {
	if output == nil {
		sum := sha256.Sum256([]byte("null"))
		return hex.EncodeToString(sum[:])
	}
	canonical, err := canonicaliseJSON(output)
	if err != nil {
		raw, _ := json.Marshal(output)
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// canonicaliseJSON walks the value, sorting map keys at every level, and
// re-emits canonical JSON. Slices keep their original order — only objects
// are normalised.
func canonicaliseJSON(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]json.RawMessage, 0, len(keys))
		for _, k := range keys {
			child, err := canonicaliseJSON(val[k])
			if err != nil {
				return nil, err
			}
			keyBytes, _ := json.Marshal(k)
			entry := append([]byte{}, keyBytes...)
			entry = append(entry, ':')
			entry = append(entry, child...)
			parts = append(parts, entry)
		}
		out := []byte("{")
		for i, p := range parts {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, p...)
		}
		out = append(out, '}')
		return out, nil
	case []interface{}:
		parts := make([]json.RawMessage, 0, len(val))
		for _, item := range val {
			child, err := canonicaliseJSON(item)
			if err != nil {
				return nil, err
			}
			parts = append(parts, child)
		}
		out := []byte("[")
		for i, p := range parts {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, p...)
		}
		out = append(out, ']')
		return out, nil
	default:
		return json.Marshal(v)
	}
}

// FunctionExecution is one persisted invocation of a Function (US-370).
// Replay compares Output hashes between the original row and a fresh run
// at the same version with the same inputs.
type FunctionExecution struct {
	ExecutionID     string          `json:"executionId"`
	FunctionRID     string          `json:"functionRid"`
	FunctionName    string          `json:"functionName,omitempty"`
	FunctionVersion string          `json:"functionVersion"`
	OntologyRID     string          `json:"ontologyRid,omitempty"`
	InputHash       string          `json:"inputHash"`
	OutputHash      string          `json:"outputHash"`
	InputJSON       json.RawMessage `json:"input,omitempty"`
	OutputJSON      json.RawMessage `json:"output,omitempty"`
	ErrorMessage    string          `json:"errorMessage,omitempty"`
	RequestedBy     string          `json:"requestedBy,omitempty"`
	IsReplay        bool            `json:"isReplay,omitempty"`
	ReplayOf        string          `json:"replayOf,omitempty"`
	ExecutedAt      time.Time       `json:"executedAt"`
}

// FunctionExecutionStore is the narrow persistence interface the function
// replay subsystem needs. The handler at POST /functions/{rid}/replay both
// reads (to find the original execution) and writes (to record the replay
// outcome). A nil store disables persistence — execute still runs, but
// replay surfaces a 503 NotConfigured.
type FunctionExecutionStore interface {
	RecordExecution(ctx context.Context, exec *FunctionExecution) error
	GetExecution(ctx context.Context, executionID string) (*FunctionExecution, error)
	FindByInputHash(ctx context.Context, functionRID, version, inputHash string) (*FunctionExecution, error)
	ListExecutions(ctx context.Context, functionRID, version string, limit int) ([]*FunctionExecution, error)
}

// ErrExecutionNotFound is the typed error a FunctionExecutionStore returns
// when no row matches the lookup. Mirrors oms.ErrNotFound semantics so
// callers can branch with errors.Is.
var ErrExecutionNotFound = errors.New("function execution not found")

// NewFunctionExecutionID generates a deterministic identifier for the
// execution row keyed by function rid + version + input hash + executed
// timestamp. The shape mirrors the existing rid format so the row reads
// like a first-class resource.
func NewFunctionExecutionID(functionRID, version, inputHash string, ts time.Time) string {
	body := fmt.Sprintf("%s|%s|%s|%d", functionRID, version, inputHash, ts.UnixNano())
	sum := sha256.Sum256([]byte(body))
	return "fnx-" + hex.EncodeToString(sum[:8])
}
