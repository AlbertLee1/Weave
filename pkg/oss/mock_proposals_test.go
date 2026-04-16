package oss_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Proposal stubs on mockOmsRepo to satisfy oms.Repository.

func (m *mockOmsRepo) CreateProposal(_ context.Context, _ *oms.OntologyProposal) error { return nil }
func (m *mockOmsRepo) GetProposal(_ context.Context, _ string) (*oms.OntologyProposal, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) ListProposals(_ context.Context, _ string) ([]oms.OntologyProposal, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateProposalStatus(_ context.Context, _, _ string) error { return nil }
func (m *mockOmsRepo) CreateProposalReview(_ context.Context, _ *oms.ProposalReview) error {
	return nil
}
func (m *mockOmsRepo) ListProposalReviews(_ context.Context, _ string) ([]oms.ProposalReview, error) {
	return nil, nil
}
