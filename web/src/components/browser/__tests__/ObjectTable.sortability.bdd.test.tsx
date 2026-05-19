import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ObjectTable } from '../ObjectTable';
import type { ObjectType, WireObject } from '../../../api/types';

const objectType: ObjectType = {
  rid: 'ri.ot.delivery',
  apiName: 'Delivery',
  displayName: 'Delivery',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    summary: { dataType: { type: 'string' }, rid: 'ri.p.summary' },
    deliveredDate: { dataType: { type: 'date' }, rid: 'ri.p.deliveredDate' },
  },
};

const rows: WireObject[] = [
  {
    __primaryKey: 'delivery-1',
    __apiName: 'Delivery',
    __rid: 'ri.object.delivery.1',
    id: 'delivery-1',
    summary: 'Dock received',
    deliveredDate: '2026-05-19',
  },
];

function renderTable(
  sortablePropertyKeys?: ReadonlySet<string>,
  onSort = vi.fn(),
) {
  render(
    <ObjectTable
      ontologyApiName="logistics"
      objectType={objectType}
      data={rows}
      onSort={onSort}
      sortablePropertyKeys={sortablePropertyKeys}
    />,
  );

  return { onSort };
}

describe('BDD: ObjectTable metadata-gated sortability (SELF-445)', () => {
  it('Given detailed property metadata marks summary as not sortable, When the Browser table renders, Then the summary column is not exposed as a sortable header', () => {
    const { onSort } = renderTable(new Set(['deliveredDate']));

    fireEvent.click(screen.getByText('summary'));

    expect(onSort).not.toHaveBeenCalled();
  });

  it('Given detailed property metadata marks deliveredDate as sortable, When the user sorts that column, Then the table emits a deliveredDate sort request', () => {
    const { onSort } = renderTable(new Set(['deliveredDate']));

    fireEvent.click(screen.getByText('deliveredDate'));

    expect(onSort).toHaveBeenCalledWith('deliveredDate', 'asc');
  });

  it('Given detailed property metadata is unavailable, When the table renders, Then only the primary key sort affordance remains enabled', () => {
    const { onSort } = renderTable();

    fireEvent.click(screen.getByText('summary'));
    expect(onSort).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText('id'));
    expect(onSort).toHaveBeenCalledWith('__primaryKey', 'asc');
  });
});
