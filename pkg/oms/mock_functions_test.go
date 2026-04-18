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
	for _, existing := range m.functions {
		if existing.OntologyRID == fn.OntologyRID && existing.Name == fn.Name && existing.Version == fn.Version {
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

func (m *mockRepo) GetFunctionByName(_ context.Context, ontologyRID, name string) (*oms.Function, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	var matches []oms.Function
	for _, fn := range m.functions {
		ontologyMatch := fn.OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, fn.OntologyRID)
		if !ontologyMatch {
			continue
		}
		if fn.RID == name {
			return &fn, nil
		}
		if fn.Name == name {
			matches = append(matches, fn)
		}
	}
	if len(matches) == 0 {
		return nil, oms.ErrNotFound
	}
	oms.SortFunctionsByVersionDesc(matches)
	winner := matches[0]
	return &winner, nil
}

func (m *mockRepo) GetFunctionByNameVersion(_ context.Context, ontologyRID, name, version string) (*oms.Function, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for _, fn := range m.functions {
		ontologyMatch := fn.OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, fn.OntologyRID)
		if ontologyMatch && fn.Name == name && fn.Version == version {
			return &fn, nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) ListFunctions(_ context.Context, ontologyRID string) ([]oms.Function, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.Function
	for _, fn := range m.functions {
		if fn.OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, fn.OntologyRID) {
			result = append(result, fn)
		}
	}
	oms.SortFunctionsByVersionDesc(result)
	return result, nil
}

func (m *mockRepo) ListFunctionVersionsByName(_ context.Context, ontologyRID, name string) ([]oms.Function, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.Function
	for _, fn := range m.functions {
		ontologyMatch := fn.OntologyRID == ontologyRID || m.matchOntologyByApiName(ontologyRID, fn.OntologyRID)
		if ontologyMatch && fn.Name == name {
			result = append(result, fn)
		}
	}
	oms.SortFunctionsByVersionDesc(result)
	return result, nil
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
