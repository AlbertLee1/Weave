package sqlqueries_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/sqlqueries"
)

// TestDefaultConfig_Given_NewConfig_When_Inspect_Then_Has5sTimeoutAnd10KRows pins
// the US-468 PRD defaults so future tweaks must be conscious.
func TestDefaultConfig_Given_NewConfig_When_Inspect_Then_Has5sTimeoutAnd10KRows(t *testing.T) {
	cfg := sqlqueries.DefaultConfig()
	if cfg.Timeout != 5*time.Second {
		t.Fatalf("Default Timeout = %v, want 5s", cfg.Timeout)
	}
	if cfg.MaxRows != 10000 {
		t.Fatalf("Default MaxRows = %d, want 10000", cfg.MaxRows)
	}
}

// TestNewPGEngineWithConfig_Given_OverrideConfig_When_Construct_Then_PreservesValues
// asserts the override constructor wires its values through Config().
func TestNewPGEngineWithConfig_Given_OverrideConfig_When_Construct_Then_PreservesValues(t *testing.T) {
	cfg := sqlqueries.Config{Timeout: 200 * time.Millisecond, MaxRows: 50}
	e := sqlqueries.NewPGEngineWithConfig(nil, cfg)
	if got := e.Config(); got != cfg {
		t.Fatalf("Config = %+v, want %+v", got, cfg)
	}
}

// TestNewPGEngineWithConfig_Given_ZeroFields_When_Construct_Then_FillsDefaults
// ensures partially-specified configs back-fill missing fields from defaults,
// so callers can override one knob without losing the other.
func TestNewPGEngineWithConfig_Given_ZeroFields_When_Construct_Then_FillsDefaults(t *testing.T) {
	def := sqlqueries.DefaultConfig()
	cases := []struct {
		name string
		in   sqlqueries.Config
		want sqlqueries.Config
	}{
		{"both zero", sqlqueries.Config{}, def},
		{"only timeout", sqlqueries.Config{Timeout: 1 * time.Second}, sqlqueries.Config{Timeout: 1 * time.Second, MaxRows: def.MaxRows}},
		{"only maxRows", sqlqueries.Config{MaxRows: 7}, sqlqueries.Config{Timeout: def.Timeout, MaxRows: 7}},
		{"negative treated as default", sqlqueries.Config{Timeout: -1, MaxRows: -1}, def},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := sqlqueries.NewPGEngineWithConfig(nil, tc.in)
			if got := e.Config(); got != tc.want {
				t.Fatalf("Config = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestExecute_Given_TimeoutSentinel_When_HandlerRunsEngine_Then_FailureReasonIsQueryTimeout
// exercises the wire-level mapping for the new ErrQueryTimeout sentinel.
func TestExecute_Given_TimeoutSentinel_When_HandlerRunsEngine_Then_FailureReasonIsQueryTimeout(t *testing.T) {
	engine := &fakeEngine{err: sqlqueries.ErrQueryTimeout}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]any{
		"query": "SELECT 1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sqlqueries.QueryStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "failed" {
		t.Fatalf("type = %q, want failed", resp.Type)
	}
	if resp.FailureReason != "QueryTimeout" {
		t.Fatalf("failureReason = %q, want QueryTimeout", resp.FailureReason)
	}
}

// TestExecute_Given_MaxRowsSentinel_When_HandlerRunsEngine_Then_FailureReasonIsMaxRowsExceeded
// pins ErrMaxRowsExceeded → MaxRowsExceeded mapping at the wire level.
func TestExecute_Given_MaxRowsSentinel_When_HandlerRunsEngine_Then_FailureReasonIsMaxRowsExceeded(t *testing.T) {
	engine := &fakeEngine{err: sqlqueries.ErrMaxRowsExceeded}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]any{
		"query": "SELECT * FROM big",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sqlqueries.QueryStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "failed" {
		t.Fatalf("type = %q, want failed", resp.Type)
	}
	if resp.FailureReason != "MaxRowsExceeded" {
		t.Fatalf("failureReason = %q, want MaxRowsExceeded", resp.FailureReason)
	}
}

// TestExecute_Given_WrappedTimeoutFromContext_When_HandlerRunsEngine_Then_FailureReasonIsQueryTimeout
// covers a PG driver that surfaces ctx.DeadlineExceeded without wrapping
// ErrQueryTimeout itself; the handler must still classify it.
func TestExecute_Given_WrappedTimeoutFromContext_When_HandlerRunsEngine_Then_FailureReasonIsQueryTimeout(t *testing.T) {
	engine := &fakeEngine{err: context.DeadlineExceeded}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]any{
		"query": "SELECT 1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sqlqueries.QueryStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.FailureReason != "QueryTimeout" {
		t.Fatalf("failureReason = %q, want QueryTimeout (DeadlineExceeded fallback)", resp.FailureReason)
	}
}

// TestSentinels_Given_NewSentinels_When_ErrorsIs_Then_Distinguishable verifies
// errors.Is wiring across the two new sentinels and prevents an accidental
// alias to existing sentinels.
func TestSentinels_Given_NewSentinels_When_ErrorsIs_Then_Distinguishable(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		isFn  func(error) bool
		other func(error) bool
	}{
		{
			"timeout",
			sqlqueries.ErrQueryTimeout,
			func(e error) bool { return errors.Is(e, sqlqueries.ErrQueryTimeout) },
			func(e error) bool { return errors.Is(e, sqlqueries.ErrMaxRowsExceeded) },
		},
		{
			"maxRows",
			sqlqueries.ErrMaxRowsExceeded,
			func(e error) bool { return errors.Is(e, sqlqueries.ErrMaxRowsExceeded) },
			func(e error) bool { return errors.Is(e, sqlqueries.ErrQueryTimeout) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.isFn(tc.err) {
				t.Fatalf("errors.Is sentinel mismatch: %v", tc.err)
			}
			if tc.other(tc.err) {
				t.Fatalf("sentinel %v aliases the other sentinel", tc.err)
			}
			if errors.Is(tc.err, sqlqueries.ErrForbiddenStatement) {
				t.Fatalf("sentinel %v must not alias ErrForbiddenStatement", tc.err)
			}
		})
	}
}

// recordingEngine and a stub httptest exist via handlers_test.go (fakeEngine,
// newRouter, doPost). The block below pins a typecheck-only assertion that
// PGEngine still satisfies the Engine interface after the refactor.
var _ sqlqueries.Engine = (*sqlqueries.PGEngine)(nil)

// keep httptest import in case future tests need a recorder directly.
var _ = httptest.NewRecorder
var _ = bytes.NewBufferString
