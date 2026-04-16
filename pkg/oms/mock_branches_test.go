package oms_test

import (
	"context"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// Branch stubs on mockRepo to satisfy oms.Repository.

func (m *mockRepo) CreateBranch(_ context.Context, b *oms.OntologyBranch) error {
	if m.createErr != nil {
		return m.createErr
	}
	// Check for duplicate (ontologyRid, name)
	for _, existing := range m.branches {
		if existing.OntologyRID == b.OntologyRID && existing.Name == b.Name {
			return oms.ErrDuplicate
		}
	}
	b.CreatedAt = time.Now()
	b.UpdatedAt = time.Now()
	m.branches = append(m.branches, *b)
	return nil
}

func (m *mockRepo) GetBranch(_ context.Context, id string) (*oms.OntologyBranch, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.branches {
		if m.branches[i].ID == id {
			return &m.branches[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) ListBranches(_ context.Context, ontologyRID string) ([]oms.OntologyBranch, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.OntologyBranch
	for _, b := range m.branches {
		if b.OntologyRID == ontologyRID {
			result = append(result, b)
		}
	}
	return result, nil
}

func (m *mockRepo) CloseBranch(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i := range m.branches {
		if m.branches[i].ID == id {
			m.branches[i].Status = "closed"
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) CreateBranchChange(_ context.Context, c *oms.BranchChange) error {
	c.CreatedAt = time.Now()
	m.branchChanges = append(m.branchChanges, *c)
	return nil
}

func (m *mockRepo) ListBranchChanges(_ context.Context, branchID string) ([]oms.BranchChange, error) {
	var result []oms.BranchChange
	for _, c := range m.branchChanges {
		if c.BranchID == branchID {
			result = append(result, c)
		}
	}
	return result, nil
}

// Branch stubs on noopRepo to satisfy oms.Repository.

func (n *noopRepo) CreateBranch(_ context.Context, _ *oms.OntologyBranch) error {
	return nil
}
func (n *noopRepo) GetBranch(_ context.Context, _ string) (*oms.OntologyBranch, error) {
	return nil, nil
}
func (n *noopRepo) ListBranches(_ context.Context, _ string) ([]oms.OntologyBranch, error) {
	return nil, nil
}
func (n *noopRepo) CloseBranch(_ context.Context, _ string) error {
	return nil
}
func (n *noopRepo) CreateBranchChange(_ context.Context, _ *oms.BranchChange) error {
	return nil
}
func (n *noopRepo) ListBranchChanges(_ context.Context, _ string) ([]oms.BranchChange, error) {
	return nil, nil
}
