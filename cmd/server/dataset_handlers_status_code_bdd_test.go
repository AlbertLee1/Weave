package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// errorInjectingOntologyResolver always returns the configured error
// (or nil ontology if err is nil) for GetOntology, and likewise for
// GetObjectType. Lets the BDD scenarios drive the "non-ErrNotFound"
// branch of the dataset handlers' error ladder.
type errorInjectingOntologyResolver struct {
	getOntErr error
	getOntRet *oms.Ontology
	getObjErr error
	getObjRet *oms.ObjectType
}

func (r *errorInjectingOntologyResolver) GetOntology(_ context.Context, _ string) (*oms.Ontology, error) {
	if r.getOntErr != nil {
		return nil, r.getOntErr
	}
	return r.getOntRet, nil
}

func (r *errorInjectingOntologyResolver) GetObjectType(_ context.Context, _ string) (*oms.ObjectType, error) {
	if r.getObjErr != nil {
		return nil, r.getObjErr
	}
	return r.getObjRet, nil
}

// errorInjectingTxStore returns the configured error on every read or
// write to drive the rollback / history handlers' downstream-failure
// branches. nil err yields empty results so happy paths can also run.
type errorInjectingTxStore struct {
	getErr       error
	latestErr    error
	listByErr    error
	recordErr    error
	listAfterErr error
	markErr      error
}

func (s *errorInjectingTxStore) GetDatasetTransaction(_ context.Context, _ string) (*oms.DatasetTransaction, error) {
	return nil, s.getErr
}

func (s *errorInjectingTxStore) LatestForOntology(_ context.Context, _ string) (*oms.DatasetTransaction, error) {
	return nil, s.latestErr
}

func (s *errorInjectingTxStore) ListByOntology(_ context.Context, _ string, _ int) ([]oms.DatasetTransaction, error) {
	if s.listByErr != nil {
		return nil, s.listByErr
	}
	return []oms.DatasetTransaction{}, nil
}

func (s *errorInjectingTxStore) RecordDatasetTransaction(_ context.Context, _ *oms.DatasetTransaction) error {
	return s.recordErr
}

func (s *errorInjectingTxStore) ListAfterCommittedAt(_ context.Context, _ string, _ time.Time) ([]oms.DatasetTransaction, error) {
	if s.listAfterErr != nil {
		return nil, s.listAfterErr
	}
	return []oms.DatasetTransaction{}, nil
}

func (s *errorInjectingTxStore) MarkRolledBack(_ context.Context, _, _ string, _ time.Time) error {
	return s.markErr
}

// TestBDD_DatasetHandlers_DownstreamErrorsReturnHTTP500 covers a
// systematic wire-shape correctness bug across the cmd/server dataset
// handlers — 11 call sites used `apierror.NewInvalidParameter(
// "Dataset…Failed", …)` for what are actually downstream PG / store
// failures. That returned HTTP 400 INVALID_ARGUMENT — telling Foundry
// SDK clients "you sent a bad request, fix your input" when the
// caller's input (dataset RID from URL, optional JSON body) was
// already validated and the actual failure was server-side.
//
// Sites fixed (file:line pre-commit):
//
//	dataset_transaction_handler.go:110  DatasetHistoryFailed (GetOntology err)
//	dataset_transaction_handler.go:127  DatasetHistoryFailed (ListByOntology err)
//	dataset_rollback_handler.go:128     DatasetRollbackFailed (GetOntology err on CreateTransaction)
//	dataset_rollback_handler.go:154     DatasetRollbackFailed (LatestForOntology err)
//	dataset_rollback_handler.go:172     DatasetRollbackFailed (RecordDatasetTransaction err)
//	dataset_rollback_handler.go:246     DatasetRollbackFailed (GetOntology err on Rollback)
//	dataset_rollback_handler.go:263     DatasetRollbackFailed (GetDatasetTransaction non-NotFound err)
//	dataset_rollback_handler.go:281     DatasetRollbackFailed (ListAfterCommittedAt err)
//	dataset_rollback_handler.go:301     DatasetRollbackFailed (replayObjects err)
//	dataset_rollback_handler.go:315     DatasetRollbackFailed (MarkRolledBack err)
//	dataset_rollback_handler.go:335     DatasetRollbackFailed (bookkeeping RecordDatasetTransaction err)
//
// All 11 sites are clean wins: each sits in the "non-ErrNotFound" branch
// of an error-classification ladder where the sentinel ErrNotFound case
// is already handled correctly above with NewNotFound (HTTP 404).
//
// This BDD covers 5 representative scenarios:
//
//  1. DatasetHistory GetOntology downstream error → 500
//     DatasetHistoryFailed
//  2. DatasetHistory ListByOntology downstream error → 500
//     DatasetHistoryFailed (covers line 127)
//  3. CreateTransaction RecordDatasetTransaction error → 500
//     DatasetRollbackFailed (covers line 172)
//  4. Rollback ListAfterCommittedAt error → 500 DatasetRollbackFailed
//     (covers line 281)
//  5. Regression guard: genuine ErrNotFound on GetOntology STILL surfaces
//     as 404 DatasetNotFound on the history path
//
// The 7 remaining same-shape sites ship with the same one-line fix;
// promoting them to BDD would just duplicate plumbing (each uses the
// same error-injection mock pattern with a different error field set).
func TestBDD_DatasetHandlers_DownstreamErrorsReturnHTTP500(t *testing.T) {
	const ontologyRID = "ri.ontology.main.ontology.test"
	const ontologyAPIName = "test"

	t.Run("DatasetHistory GetOntology error returns HTTP 500 INTERNAL", func(t *testing.T) {
		repo := &errorInjectingOntologyResolver{
			getOntErr: errors.New("postgres: connection lost"),
		}
		store := &errorInjectingTxStore{}
		router := newDatasetHistoryRouter(repo, store)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/datasets/"+ontologyRID+"/history", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertDatasetInternalError(t, rec, "DatasetHistoryFailed", "postgres: connection lost")
	})

	t.Run("DatasetHistory ListByOntology error returns HTTP 500 INTERNAL", func(t *testing.T) {
		repo := &errorInjectingOntologyResolver{
			getOntRet: &oms.Ontology{RID: ontologyRID, APIName: ontologyAPIName},
		}
		store := &errorInjectingTxStore{listByErr: errors.New("postgres: deadlock detected")}
		router := newDatasetHistoryRouter(repo, store)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/datasets/"+ontologyRID+"/history", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertDatasetInternalError(t, rec, "DatasetHistoryFailed", "postgres: deadlock detected")
	})

	t.Run("CreateTransaction RecordDatasetTransaction error returns HTTP 500 INTERNAL", func(t *testing.T) {
		repo := &errorInjectingOntologyResolver{
			getOntRet: &oms.Ontology{RID: ontologyRID, APIName: ontologyAPIName},
		}
		store := &errorInjectingTxStore{recordErr: errors.New("postgres: disk full")}
		router := newDatasetRollbackRouter(repo, store, nil, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/api/v2/datasets/"+ontologyRID+"/transactions", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertDatasetInternalError(t, rec, "DatasetRollbackFailed", "postgres: disk full")
	})

	t.Run("Rollback ListAfterCommittedAt error returns HTTP 500 INTERNAL", func(t *testing.T) {
		// Seed the txStore with the rollback target so the GetDatasetTransaction
		// step succeeds; force the subsequent ListAfterCommittedAt to error.
		txStore := newFakeDatasetTxWriter()
		const targetTxID = "tx-target-1"
		txStore.seed(&oms.DatasetTransaction{
			TxID:            targetTxID,
			OntologyAPIName: ontologyAPIName,
			CommittedAt:     time.Now().UTC().Add(-time.Hour),
		})
		// Wrap with our error-injecting decorator: pass through Get / Latest /
		// Record but force ListAfterCommittedAt to fail.
		wrapped := &listAfterErrorTxStore{
			datasetTransactionWriter: txStore,
			err:                      errors.New("postgres: query canceled"),
		}

		repo := &errorInjectingOntologyResolver{
			getOntRet: &oms.Ontology{RID: ontologyRID, APIName: ontologyAPIName},
		}
		router := newDatasetRollbackRouter(repo, wrapped, nil, nil, nil)

		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/datasets/"+ontologyRID+"/rollback?to="+targetTxID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertDatasetInternalError(t, rec, "DatasetRollbackFailed", "postgres: query canceled")
	})

	t.Run("DatasetHistory genuine ErrNotFound regression guard still returns HTTP 404", func(t *testing.T) {
		// Sentinel path: GetOntology returns oms.ErrNotFound → handler must
		// surface HTTP 404 DatasetNotFound, NOT the new 500 envelope.
		repo := &errorInjectingOntologyResolver{getOntErr: oms.ErrNotFound}
		store := &errorInjectingTxStore{}
		router := newDatasetHistoryRouter(repo, store)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/datasets/"+ontologyRID+"/history", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("genuine ErrNotFound: status = %d, want 404; body = %s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorCode string `json:"errorCode"`
			ErrorName string `json:"errorName"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&env)
		if env.ErrorCode != "NOT_FOUND" {
			t.Errorf("errorCode = %q, want NOT_FOUND", env.ErrorCode)
		}
		if env.ErrorName != "DatasetNotFound" {
			t.Errorf("errorName = %q, want DatasetNotFound", env.ErrorName)
		}
	})
}

// listAfterErrorTxStore wraps the happy-path fakeDatasetTxWriter and
// forces just the ListAfterCommittedAt call to fail. Lets the Rollback
// BDD scenario reach step 2 (newer-tx enumeration) where the
// downstream-error branch under test lives.
type listAfterErrorTxStore struct {
	datasetTransactionWriter
	err error
}

func (s *listAfterErrorTxStore) ListAfterCommittedAt(_ context.Context, _ string, _ time.Time) ([]oms.DatasetTransaction, error) {
	return nil, s.err
}

func assertDatasetInternalError(t *testing.T, rec *httptest.ResponseRecorder, wantErrorName, wantErrorSubstring string) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorCode != "INTERNAL" {
		t.Errorf("errorCode = %q, want INTERNAL (400 INVALID_ARGUMENT would mislead the SDK into a 'fix your input' branch when the caller's input is fine and the dataset transaction store failed)", env.ErrorCode)
	}
	if env.ErrorName != wantErrorName {
		t.Errorf("errorName = %q, want %q", env.ErrorName, wantErrorName)
	}
	if wantErrorSubstring != "" {
		got := env.Parameters["error"]
		if !strings.Contains(got, wantErrorSubstring) {
			t.Errorf("parameters.error = %q, want it to mention %q", got, wantErrorSubstring)
		}
	}
}
