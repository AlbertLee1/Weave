import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ObjectTable } from '../ObjectTable';
import type { ObjectType, WireObject } from '../../../api/types';

const objectType: ObjectType = {
  rid: 'ri.ot',
  apiName: 'Employee',
  displayName: 'Employee',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
  },
};

const rows: WireObject[] = [
  { __primaryKey: '1', __apiName: 'Employee', __rid: 'ri.o.1', id: '1', name: 'Alice' },
  { __primaryKey: '2', __apiName: 'Employee', __rid: 'ri.o.2', id: '2', name: 'Bob' },
];

describe('ObjectTable selection mode', () => {
  it('renders no checkbox column when selection prop is omitted', () => {
    render(
      <ObjectTable ontologyApiName="ont" objectType={objectType} data={rows} />,
    );
    expect(screen.queryByTestId('select-all')).not.toBeInTheDocument();
    expect(screen.queryByTestId('select-row-1')).not.toBeInTheDocument();
  });

  it('renders per-row checkbox + select-all when selection prop is supplied', () => {
    const onToggle = vi.fn();
    const onToggleAll = vi.fn();
    render(
      <ObjectTable
        ontologyApiName="ont"
        objectType={objectType}
        data={rows}
        selection={{
          selectedKeys: new Set(),
          onToggle,
          onToggleAll,
        }}
      />,
    );
    expect(screen.getByTestId('select-all')).toBeInTheDocument();
    expect(screen.getByTestId('select-row-1')).toBeInTheDocument();
    expect(screen.getByTestId('select-row-2')).toBeInTheDocument();
  });

  it('calls onToggle with the primary key when a row checkbox is clicked', () => {
    const onToggle = vi.fn();
    render(
      <ObjectTable
        ontologyApiName="ont"
        objectType={objectType}
        data={rows}
        selection={{
          selectedKeys: new Set(),
          onToggle,
          onToggleAll: vi.fn(),
        }}
      />,
    );
    fireEvent.click(screen.getByTestId('select-row-1'));
    expect(onToggle).toHaveBeenCalledWith('1');
  });

  it('row checkbox click does not propagate to row onRowClick', () => {
    const onRowClick = vi.fn();
    const onToggle = vi.fn();
    render(
      <ObjectTable
        ontologyApiName="ont"
        objectType={objectType}
        data={rows}
        onRowClick={onRowClick}
        selection={{
          selectedKeys: new Set(),
          onToggle,
          onToggleAll: vi.fn(),
        }}
      />,
    );
    fireEvent.click(screen.getByTestId('select-row-1'));
    expect(onToggle).toHaveBeenCalledWith('1');
    expect(onRowClick).not.toHaveBeenCalled();
  });

  it('select-all checkbox reflects "all selected" state', () => {
    const onToggleAll = vi.fn();
    render(
      <ObjectTable
        ontologyApiName="ont"
        objectType={objectType}
        data={rows}
        selection={{
          selectedKeys: new Set(['1', '2']),
          onToggle: vi.fn(),
          onToggleAll,
        }}
      />,
    );
    const selectAll = screen.getByTestId('select-all') as HTMLInputElement;
    expect(selectAll.checked).toBe(true);
    fireEvent.click(selectAll);
    // Clicking a checked "select-all" fires onChange with checked=false.
    expect(onToggleAll).toHaveBeenCalledWith(false);
  });

  it('select-all checkbox calls onToggleAll(true) when clicked while unchecked', () => {
    const onToggleAll = vi.fn();
    render(
      <ObjectTable
        ontologyApiName="ont"
        objectType={objectType}
        data={rows}
        selection={{
          selectedKeys: new Set(),
          onToggle: vi.fn(),
          onToggleAll,
        }}
      />,
    );
    const selectAll = screen.getByTestId('select-all') as HTMLInputElement;
    expect(selectAll.checked).toBe(false);
    fireEvent.click(selectAll);
    expect(onToggleAll).toHaveBeenCalledWith(true);
  });
});
