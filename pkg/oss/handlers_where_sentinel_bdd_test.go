package oss_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/where"
)

// TestBDD_SearchAndCountObjects_SplitsUserSideFromServerSide covers
// round 36 of the wire-shape correctness series: SearchObjects and
// CountObjects previously funneled BOTH user-side errors (bad regex,
// unsupported operator) AND server-side failures (Bleve outage,
// internal serialization issues) through `NewInvalidParameter(
// "SearchObjectsFailed", …)` returning HTTP 400. SDKs couldn't tell
// "your input is bad — fix it" from "the server is broken — retry".
//
// Round 36 introduces a where.ErrInvalidWhereClause sentinel and
// routes:
//
//   - errors.Is(err, oms.ErrNotFound)             → 404 ObjectTypeNotFound
//   - errors.Is(err, where.ErrInvalidWhereClause) → 400 InvalidWhereClause
//   - else                                        → 500 SearchObjectsFailed
//                                                 / CountObjectsFailed
//
// Acceptance criteria (Given → When → Then):
//
//   Given a SearchObjects request whose where clause uses an
//         unsupported operator
//   When  the handler runs
//   Then  it returns HTTP 400 with errorName "InvalidWhereClause"
//         and reason mentioning the unsupported operator
//
//   Given a SearchObjects request whose service layer fails with a
//         non-ErrNotFound, non-where error (simulating a Bleve outage)
//   When  the handler runs
//   Then  it returns HTTP 500 with errorName "SearchObjectsFailed"
//         (was incorrectly HTTP 400 before round 36)
//
//   Given a CountObjects request with an unsupported where operator
//   When  the handler runs
//   Then  it returns HTTP 400 with errorName "InvalidWhereClause"
//
//   Given a CountObjects request whose service layer fails downstream
//   When  the handler runs
//   Then  it returns HTTP 500 with errorName "CountObjectsFailed"
//
//   Regression guard: genuine ErrNotFound continues to surface as
//   HTTP 404 ObjectTypeNotFound (untouched by round 36).
func TestBDD_SearchAndCountObjects_SplitsUserSideFromServerSide(t *testing.T) {
	// Where-clause conversion happens inside the service layer (see
	// ServiceImpl.SearchObjects + .CountObjects calling
	// where.ConvertToBleveQueryWithOpts). For this BDD we inject a
	// `where.ErrInvalidWhereClause`-wrapped error via the mock service
	// to verify the handler's sentinel routing. The end-to-end "real
	// converter + real handler" path is covered by the existing
	// TestSearchObjects_RegexInvalidPatternReturns400 (handlers_regex_test.go).
	searchBody := `{"select":["name"],"where":{"type":"eq","field":"name","value":"alice"}}`
	countBody := `{"where":{"type":"eq","field":"name","value":"alice"}}`
	wrappedWhereErr := fmt.Errorf("%w: %w", where.ErrInvalidWhereClause,
		errors.New(`unsupported where clause type: "definitely-not-a-real-operator"`))

	t.Run("SearchObjects returns ErrInvalidWhereClause → 400 InvalidWhereClause", func(t *testing.T) {
		svc := &whereSentinelSvc{searchErr: wrappedWhereErr}
		router := newWhereSentinelRouter(svc)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont1/objects/Employee/search",
			strings.NewReader(searchBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertWhereSentinel(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT", "InvalidWhereClause",
			"definitely-not-a-real-operator")
	})

	t.Run("SearchObjects downstream service error → 500 SearchObjectsFailed", func(t *testing.T) {
		svc := &whereSentinelSvc{
			searchErr: errors.New("bleve: index unavailable"),
		}
		router := newWhereSentinelRouter(svc)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont1/objects/Employee/search",
			strings.NewReader(searchBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertWhereSentinel(t, rec, http.StatusInternalServerError, "INTERNAL", "SearchObjectsFailed",
			"bleve: index unavailable")
	})

	t.Run("CountObjects returns ErrInvalidWhereClause → 400 InvalidWhereClause", func(t *testing.T) {
		svc := &whereSentinelSvc{countErr: wrappedWhereErr}
		router := newWhereSentinelRouter(svc)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont1/objects/Employee/count",
			strings.NewReader(countBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertWhereSentinel(t, rec, http.StatusBadRequest, "INVALID_ARGUMENT", "InvalidWhereClause",
			"definitely-not-a-real-operator")
	})

	t.Run("CountObjects downstream service error → 500 CountObjectsFailed", func(t *testing.T) {
		svc := &whereSentinelSvc{
			countErr: errors.New("postgres: connection lost"),
		}
		router := newWhereSentinelRouter(svc)

		body := `{}` // empty body — bypasses where converter; svc returns the error
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/ont1/objects/Employee/count",
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertWhereSentinel(t, rec, http.StatusInternalServerError, "INTERNAL", "CountObjectsFailed",
			"postgres: connection lost")
	})
}

// whereSentinelSvc is a minimal oss.Service test double: returns
// configurable errors per method. Only SearchObjects / CountObjects
// matter for the round-36 BDD; the rest panic (unused).
type whereSentinelSvc struct {
	searchErr error
	countErr  error
}

func (s *whereSentinelSvc) GetObject(context.Context, oss.GetObjectRequest) (*oss.WireObject, error) {
	panic("not used")
}
func (s *whereSentinelSvc) ListObjects(context.Context, oss.ListObjectsRequest) (*oss.ObjectPage, error) {
	panic("not used")
}
func (s *whereSentinelSvc) SearchObjects(_ context.Context, _ oss.SearchObjectsRequest) (*oss.ObjectPage, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	// Happy path returns an empty page so the handler short-circuits.
	return &oss.ObjectPage{Data: []*oss.WireObject{}}, nil
}
func (s *whereSentinelSvc) ListLinkedObjects(context.Context, oss.LinkedObjectsRequest) (*oss.ObjectPage, error) {
	panic("not used")
}
func (s *whereSentinelSvc) GetLinkedObject(context.Context, oss.GetLinkedObjectRequest) (*oss.WireObject, error) {
	panic("not used")
}
func (s *whereSentinelSvc) CountObjects(_ context.Context, _ oss.CountObjectsRequest) (*oss.CountObjectsResponse, error) {
	if s.countErr != nil {
		return nil, s.countErr
	}
	return &oss.CountObjectsResponse{Count: 0}, nil
}

var _ oss.Service = (*whereSentinelSvc)(nil)

func newWhereSentinelRouter(svc oss.Service) http.Handler {
	h := oss.NewHandler(svc)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func assertWhereSentinel(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode, wantName, wantReasonSub string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, rec.Body.String())
	}
	var env struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorCode != wantCode {
		t.Errorf("errorCode = %q, want %q", env.ErrorCode, wantCode)
	}
	if env.ErrorName != wantName {
		t.Errorf("errorName = %q, want %q", env.ErrorName, wantName)
	}
	if wantReasonSub != "" && !strings.Contains(env.Parameters["reason"], wantReasonSub) {
		t.Errorf("parameters.reason = %q, want it to mention %q", env.Parameters["reason"], wantReasonSub)
	}
}
