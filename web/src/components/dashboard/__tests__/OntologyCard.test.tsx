import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { OntologyCard } from '../OntologyCard';

const mockOntology = {
  rid: 'ri.1',
  apiName: 'test-ontology',
  displayName: 'Test Ontology',
  description: 'A test ontology for unit testing',
};

describe('OntologyCard', () => {
  it('displays displayName', () => {
    render(
      <OntologyCard ontology={mockOntology} objectTypeCount={5} onClick={() => {}} />,
    );
    expect(screen.getByText('Test Ontology')).toBeInTheDocument();
  });

  it('displays description', () => {
    render(
      <OntologyCard ontology={mockOntology} objectTypeCount={5} onClick={() => {}} />,
    );
    expect(screen.getByText('A test ontology for unit testing')).toBeInTheDocument();
  });

  it('displays object type count', () => {
    render(
      <OntologyCard ontology={mockOntology} objectTypeCount={5} onClick={() => {}} />,
    );
    // The component renders the count and label as sibling text nodes
    // ("5" + " types"), so use a regex matcher rather than exact text.
    expect(screen.getByText(/5\s*types?/)).toBeInTheDocument();
  });

  it('calls onClick when clicked', () => {
    const onClick = vi.fn();
    render(
      <OntologyCard ontology={mockOntology} objectTypeCount={5} onClick={onClick} />,
    );

    fireEvent.click(screen.getByText('Test Ontology'));
    expect(onClick).toHaveBeenCalled();
  });
});
