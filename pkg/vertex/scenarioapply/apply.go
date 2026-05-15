// Package scenarioapply orchestrates the "Apply Scenario" flow — turning
// a draft scenario's edits into real writes on the main ontology inside a
// single PG transaction, marking the scenario applied, and publishing the
// resulting EditBatch to NATS.
//
// This package owns the *control flow* and is HTTP-free. It depends on
// three interfaces that real callers wire to pkg/scenarios, pkg/actions,
// pkg/funnel, and an internal/database tx runner; tests stub all three.
package scenarioapply

import (
	"context"
	"errors"
)

// Sentinel errors.
var (
	ErrScenarioNotFound = errors.New("scenario not found")
	ErrAlreadyApplied   = errors.New("scenario already applied")
)

// ScenarioStatus tracks the lifecycle. Mirrors pkg/scenarios's status
// values without importing that package so this layer stays standalone.
type ScenarioStatus string

const (
	ScenarioStatusDraft   ScenarioStatus = "draft"
	ScenarioStatusApplied ScenarioStatus = "applied"
)

// Scenario is the minimum shape required to decide apply eligibility.
type Scenario struct {
	RID    string
	Status ScenarioStatus
}

// ScenarioEdit is the minimum shape Apply needs. The real edit will be
// richer — Apply only cares about ordering, not contents.
type ScenarioEdit struct {
	Op string
}

// ScenarioReader loads a scenario + its edits.
type ScenarioReader interface {
	GetScenario(ctx context.Context, rid string) (Scenario, error)
	ListEdits(ctx context.Context, rid string) ([]ScenarioEdit, error)
}

// MainWriter mutates the main ontology + scenario state + publishes the
// resulting NATS event.
type MainWriter interface {
	ApplyEditToMain(ctx context.Context, e ScenarioEdit) error
	MarkScenarioApplied(ctx context.Context, rid string) error
	PublishEditBatch(ctx context.Context, rid string) error
}

// TxRunner runs fn inside a database transaction. fn-returns-error rolls
// back; nil commits.
type TxRunner interface {
	RunTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service composes the three dependencies. Construct with NewService.
type Service struct {
	reader ScenarioReader
	writer MainWriter
	tx     TxRunner
}

// NewService panics on any nil dep — they are required, and a nil here
// is always a programmer error.
func NewService(r ScenarioReader, w MainWriter, tx TxRunner) *Service {
	if r == nil || w == nil || tx == nil {
		panic("scenarioapply: nil dependency")
	}
	return &Service{reader: r, writer: w, tx: tx}
}

// Apply turns scenario `rid` into writes against main. Failure midway
// rolls back the entire transaction; on success the scenario is marked
// applied and an EditBatch is published.
func (s *Service) Apply(ctx context.Context, rid string) error {
	sc, err := s.reader.GetScenario(ctx, rid)
	if err != nil {
		return err
	}
	if sc.Status == ScenarioStatusApplied {
		return ErrAlreadyApplied
	}
	edits, err := s.reader.ListEdits(ctx, rid)
	if err != nil {
		return err
	}
	return s.tx.RunTx(ctx, func(ctx context.Context) error {
		for _, e := range edits {
			if err := s.writer.ApplyEditToMain(ctx, e); err != nil {
				return err
			}
		}
		if err := s.writer.MarkScenarioApplied(ctx, rid); err != nil {
			return err
		}
		return s.writer.PublishEditBatch(ctx, rid)
	})
}
