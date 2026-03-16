import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TypeTree } from '../TypeTree';
import type { ObjectType } from '../../../api/types';

const mockObjectTypes: ObjectType[] = [
  {
    rid: 'ri.1',
    apiName: 'Employee',
    displayName: 'Employee',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
  },
  {
    rid: 'ri.2',
    apiName: 'Department',
    displayName: 'Department',
    primaryKey: 'departmentId',
    status: 'EXPERIMENTAL',
    visibility: 'NORMAL',
  },
];

describe('TypeTree', () => {
  it('renders all object types', () => {
    render(
      <TypeTree
        objectTypes={mockObjectTypes}
        selectedType={null}
        onSelect={() => {}}
      />,
    );
    expect(screen.getByText('Employee')).toBeInTheDocument();
    expect(screen.getByText('Department')).toBeInTheDocument();
  });

  it('highlights selected type', () => {
    render(
      <TypeTree
        objectTypes={mockObjectTypes}
        selectedType="Employee"
        onSelect={() => {}}
      />,
    );
    const item = screen.getByText('Employee').closest('button');
    expect(item?.className).toContain('text-accent-cyan');
  });

  it('calls onSelect when an item is clicked', () => {
    const onSelect = vi.fn();
    render(
      <TypeTree
        objectTypes={mockObjectTypes}
        selectedType={null}
        onSelect={onSelect}
      />,
    );

    fireEvent.click(screen.getByText('Department'));
    expect(onSelect).toHaveBeenCalledWith('Department');
  });

  it('displays status badges', () => {
    render(
      <TypeTree
        objectTypes={mockObjectTypes}
        selectedType={null}
        onSelect={() => {}}
      />,
    );
    expect(screen.getByText('ACTIVE')).toBeInTheDocument();
    expect(screen.getByText('EXPERIMENTAL')).toBeInTheDocument();
  });
});
