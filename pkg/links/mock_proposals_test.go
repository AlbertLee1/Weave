package links_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Proposal stubs on mockRepo to satisfy oms.Repository.

func (m *mockRepo) CreateProposal(_ context.Context, _ *oms.OntologyProposal) error { return nil }
func (m *mockRepo) GetProposal(_ context.Context, _ string) (*oms.OntologyProposal, error) {
	return nil, oms.ErrNotFound
}
func (m *mockRepo) ListProposals(_ context.Context, _ string) ([]oms.OntologyProposal, error) {
	return nil, nil
}
func (m *mockRepo) UpdateProposalStatus(_ context.Context, _, _ string) error { return nil }
func (m *mockRepo) CreateProposalReview(_ context.Context, _ *oms.ProposalReview) error { return nil }
func (m *mockRepo) ListProposalReviews(_ context.Context, _ string) ([]oms.ProposalReview, error) {
	return nil, nil
}
