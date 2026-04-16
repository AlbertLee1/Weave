package chinook_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Proposal stubs on inMemoryOmsRepo to satisfy oms.Repository.

func (r *inMemoryOmsRepo) CreateProposal(_ context.Context, _ *oms.OntologyProposal) error {
	return nil
}
func (r *inMemoryOmsRepo) GetProposal(_ context.Context, _ string) (*oms.OntologyProposal, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListProposals(_ context.Context, _ string) ([]oms.OntologyProposal, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) UpdateProposalStatus(_ context.Context, _, _ string) error { return nil }
func (r *inMemoryOmsRepo) CreateProposalReview(_ context.Context, _ *oms.ProposalReview) error {
	return nil
}
func (r *inMemoryOmsRepo) ListProposalReviews(_ context.Context, _ string) ([]oms.ProposalReview, error) {
	return nil, nil
}
