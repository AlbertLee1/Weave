package oms_test

import (
	"context"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// Proposal stubs on mockRepo to satisfy oms.Repository.

func (m *mockRepo) CreateProposal(_ context.Context, p *oms.OntologyProposal) error {
	if m.createErr != nil {
		return m.createErr
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	m.proposals = append(m.proposals, *p)
	return nil
}

func (m *mockRepo) GetProposal(_ context.Context, id string) (*oms.OntologyProposal, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.proposals {
		if m.proposals[i].ID == id {
			return &m.proposals[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) ListProposals(_ context.Context, ontologyRID string) ([]oms.OntologyProposal, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.OntologyProposal
	for _, p := range m.proposals {
		if p.OntologyRID == ontologyRID {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockRepo) UpdateProposalStatus(_ context.Context, id, status string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.proposals {
		if m.proposals[i].ID == id {
			m.proposals[i].Status = status
			m.proposals[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) CreateProposalReview(_ context.Context, r *oms.ProposalReview) error {
	r.CreatedAt = time.Now()
	m.proposalReviews = append(m.proposalReviews, *r)
	return nil
}

func (m *mockRepo) ListProposalReviews(_ context.Context, proposalID string) ([]oms.ProposalReview, error) {
	var result []oms.ProposalReview
	for _, r := range m.proposalReviews {
		if r.ProposalID == proposalID {
			result = append(result, r)
		}
	}
	return result, nil
}

// Proposal stubs on noopRepo to satisfy oms.Repository.

func (n *noopRepo) CreateProposal(_ context.Context, _ *oms.OntologyProposal) error { return nil }
func (n *noopRepo) GetProposal(_ context.Context, _ string) (*oms.OntologyProposal, error) {
	return nil, nil
}
func (n *noopRepo) ListProposals(_ context.Context, _ string) ([]oms.OntologyProposal, error) {
	return nil, nil
}
func (n *noopRepo) UpdateProposalStatus(_ context.Context, _, _ string) error { return nil }
func (n *noopRepo) CreateProposalReview(_ context.Context, _ *oms.ProposalReview) error {
	return nil
}
func (n *noopRepo) ListProposalReviews(_ context.Context, _ string) ([]oms.ProposalReview, error) {
	return nil, nil
}
