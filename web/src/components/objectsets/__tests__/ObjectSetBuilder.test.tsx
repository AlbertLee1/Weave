import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ObjectSetBuilder } from '../ObjectSetBuilder';
import type { ObjectSetDefinition } from '../../../api/types';

describe('ObjectSetBuilder', () => {
  const objectTypes = ['Employee', 'Department', 'Project'];

  it('renders base type selector with object types', () => {
    const onChange = vi.fn();
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{ type: 'base', objectType: 'Employee' }}
        onChange={onChange}
      />,
    );
    expect(screen.getByRole('combobox', { name: /objectset type/i })).toHaveValue('base');
    expect(screen.getByDisplayValue('Employee')).toBeInTheDocument();
  });

  it('calls onChange when object type is changed', () => {
    const onChange = vi.fn();
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{ type: 'base', objectType: 'Employee' }}
        onChange={onChange}
      />,
    );

    const select = screen.getByDisplayValue('Employee');
    fireEvent.change(select, { target: { value: 'Department' } });
    expect(onChange).toHaveBeenCalledWith({
      type: 'base',
      objectType: 'Department',
    });
  });

  it('renders filter type with nested object set', () => {
    const filterDef: ObjectSetDefinition = {
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Employee' },
      where: { type: 'eq', field: 'status', value: 'active' },
    };
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={filterDef}
        onChange={() => {}}
      />,
    );
    // The top-level type select should show 'filter' as the selected value
    const typeSelects = screen.getAllByRole('combobox', { name: /objectset type/i });
    expect(typeSelects[0]).toHaveValue('filter');
  });

  it('renders union type label', () => {
    const unionDef: ObjectSetDefinition = {
      type: 'union',
      objectSets: [
        { type: 'base', objectType: 'Employee' },
        { type: 'base', objectType: 'Department' },
      ],
    };
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={unionDef}
        onChange={() => {}}
      />,
    );
    // The top-level type select should show 'union' as the selected value
    const typeSelects = screen.getAllByRole('combobox', { name: /objectset type/i });
    expect(typeSelects[0]).toHaveValue('union');
  });

  it('allows switching type via type selector', () => {
    const onChange = vi.fn();
    render(
      <ObjectSetBuilder
        objectTypes={objectTypes}
        value={{ type: 'base', objectType: 'Employee' }}
        onChange={onChange}
      />,
    );

    const typeSelect = screen.getByDisplayValue('base');
    fireEvent.change(typeSelect, { target: { value: 'filter' } });
    // Should call onChange with a new filter definition
    expect(onChange).toHaveBeenCalled();
    const newVal = onChange.mock.calls[0][0] as ObjectSetDefinition;
    expect(newVal.type).toBe('filter');
  });
});
