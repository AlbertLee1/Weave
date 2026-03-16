import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PropertiesTable } from '../PropertiesTable';

const mockProperties: Record<string, { dataType: { type: string }; rid: string }> = {
  name: { dataType: { type: 'string' }, rid: 'ri.p.1' },
  age: { dataType: { type: 'integer' }, rid: 'ri.p.2' },
  isActive: { dataType: { type: 'boolean' }, rid: 'ri.p.3' },
};

describe('PropertiesTable', () => {
  it('renders property names', () => {
    render(<PropertiesTable properties={mockProperties} />);
    expect(screen.getByText('name')).toBeInTheDocument();
    expect(screen.getByText('age')).toBeInTheDocument();
    expect(screen.getByText('isActive')).toBeInTheDocument();
  });

  it('renders data types', () => {
    render(<PropertiesTable properties={mockProperties} />);
    expect(screen.getByText('string')).toBeInTheDocument();
    expect(screen.getByText('integer')).toBeInTheDocument();
    expect(screen.getByText('boolean')).toBeInTheDocument();
  });

  it('renders table headers', () => {
    render(<PropertiesTable properties={mockProperties} />);
    expect(screen.getByText('API Name')).toBeInTheDocument();
    expect(screen.getByText('Base Type')).toBeInTheDocument();
  });
});
