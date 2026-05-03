package oms_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Function stubs on mockRepo to satisfy oms.Repository.

func (m *mockRepo) CreateFunction(_ context.Context, fn *oms.Function) error {
	if m.createErr != nil {
		return m.createErr
	}
	if fn.Version == "" {
		fn.Version = oms.DefaultFunctionVersion
	}
	fn.BranchID = oms.NormalizeBranchID(fn.BranchID)
	for _, existing := range m.functions {
		if existing.OntologyRID == fn.OntologyRID &&
			existing.Name == fn.Name &&
			existing.Version == fn.Version &&
			oms.NormalizeBranchID(existing.BranchID) == fn.BranchID {
			return oms.ErrDuplicate
		}
	}
	m.functions = append(m.functions, *fn)
	return nil
}

func (m *mockRepo) GetFunction(_ context.Context, rid string) (*oms.Function, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.functions {
		if m.functions[i].RID == rid {
			return &m.functions[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) GetFunctionByName(ctx context.Context, ontologyRID, name string) (*oms.Function, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	branch := oms.BranchScopeFromContext(ctx)
	var matches []oms.Function
	for _, fn := range m.functions {
		ontologyMatch := fn.OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, fn.OntologyRID)
		if !ontologyMatch {
			continue
		}
		if fn.RID == name {
			return &fn, nil
		}
		if fn.Name != name {
			continue
		}
		fnBranch := oms.NormalizeBranchID(fn.BranchID)
		if fnBranch == branch || fnBranch == oms.DefaultBranch {
			matches = append(matches, fn)
		}
	}
	if len(matches) == 0 {
		return nil, oms.ErrNotFound
	}
	matches = mockPreferBranchFunctions(matches, branch)
	oms.SortFunctionsByVersionDesc(matches)
	winner := matches[0]
	return &winner, nil
}

func (m *mockRepo) GetFunctionByNameVersion(ctx context.Context, ontologyRID, name, version string) (*oms.Function, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	branch := oms.BranchScopeFromContext(ctx)
	var fallback *oms.Function
	for i, fn := range m.functions {
		ontologyMatch := fn.OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, fn.OntologyRID)
		if !ontologyMatch || fn.Name != name || fn.Version != version {
			continue
		}
		fnBranch := oms.NormalizeBranchID(fn.BranchID)
		if fnBranch == branch {
			out := m.functions[i]
			return &out, nil
		}
		if fnBranch == oms.DefaultBranch {
			out := m.functions[i]
			fallback = &out
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) ListFunctions(ctx context.Context, ontologyRID string) ([]oms.Function, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	branch := oms.BranchScopeFromContext(ctx)
	var result []oms.Function
	for _, fn := range m.functions {
		ontologyMatch := fn.OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, fn.OntologyRID)
		if !ontologyMatch {
			continue
		}
		fnBranch := oms.NormalizeBranchID(fn.BranchID)
		if fnBranch == branch || fnBranch == oms.DefaultBranch {
			result = append(result, fn)
		}
	}
	result = mockPreferBranchFunctionsByName(result, branch)
	oms.SortFunctionsByVersionDesc(result)
	return result, nil
}

func (m *mockRepo) ListFunctionVersionsByName(ctx context.Context, ontologyRID, name string) ([]oms.Function, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	branch := oms.BranchScopeFromContext(ctx)
	var result []oms.Function
	for _, fn := range m.functions {
		ontologyMatch := fn.OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, fn.OntologyRID)
		if !ontologyMatch || fn.Name != name {
			continue
		}
		fnBranch := oms.NormalizeBranchID(fn.BranchID)
		if fnBranch == branch || fnBranch == oms.DefaultBranch {
			result = append(result, fn)
		}
	}
	result = mockPreferBranchFunctions(result, branch)
	oms.SortFunctionsByVersionDesc(result)
	return result, nil
}

// mockPreferBranchFunctions mirrors pkg/oms.preferBranchFunctions for the
// in-memory test repo: when the request scope names a non-default branch and
// some versions of the named function are published on that branch, the
// matching main rows are suppressed so the branch row wins. Versions only
// published on main still flow through unchanged so the branch inherits the
// trunk's history.
func mockPreferBranchFunctions(in []oms.Function, branch string) []oms.Function {
	if branch == "" || branch == oms.DefaultBranch {
		return in
	}
	branchVersions := map[string]bool{}
	for _, fn := range in {
		if oms.NormalizeBranchID(fn.BranchID) == branch {
			branchVersions[fn.Version] = true
		}
	}
	if len(branchVersions) == 0 {
		return in
	}
	out := in[:0]
	for _, fn := range in {
		if oms.NormalizeBranchID(fn.BranchID) != branch && branchVersions[fn.Version] {
			continue
		}
		out = append(out, fn)
	}
	return out
}

// mockPreferBranchFunctionsByName mirrors pkg/oms.preferBranchFunctionsByName
// across multiple function names so the cross-name aggregate listing stays
// correct under branch overlay.
func mockPreferBranchFunctionsByName(in []oms.Function, branch string) []oms.Function {
	if branch == "" || branch == oms.DefaultBranch {
		return in
	}
	branchKeys := map[string]bool{}
	for _, fn := range in {
		if oms.NormalizeBranchID(fn.BranchID) == branch {
			branchKeys[fn.Name+"@"+fn.Version] = true
		}
	}
	if len(branchKeys) == 0 {
		return in
	}
	out := in[:0]
	for _, fn := range in {
		if oms.NormalizeBranchID(fn.BranchID) != branch && branchKeys[fn.Name+"@"+fn.Version] {
			continue
		}
		out = append(out, fn)
	}
	return out
}

func (m *mockRepo) UpdateFunction(_ context.Context, fn *oms.Function) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.functions {
		if m.functions[i].RID == fn.RID {
			m.functions[i] = *fn
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) DeleteFunction(_ context.Context, rid string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i := range m.functions {
		if m.functions[i].RID == rid {
			m.functions = append(m.functions[:i], m.functions[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}
