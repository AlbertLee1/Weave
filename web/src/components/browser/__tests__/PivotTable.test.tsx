import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { PivotTable } from '../PivotTable';
import type { ObjectType, WireObject } from '../../../api/types';

// -----------------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------------

const orderType: ObjectType = {
  rid: 'ri.ot.order',
  apiName: 'Order',
  displayName: 'Order',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    region: { dataType: { type: 'string' }, rid: 'ri.p.region' },
    category: { dataType: { type: 'string' }, rid: 'ri.p.category' },
    amount: { dataType: { type: 'integer' }, rid: 'ri.p.amount' },
  },
};

function order(
  id: string,
  region: string,
  category: string,
  amount: number,
): WireObject {
  return {
    __rid: `ri.o.${id}`,
    __primaryKey: id,
    __apiName: 'Order',
    id,
    region,
    category,
    amount,
  };
}

const sample: WireObject[] = [
  order('o1', 'NA', 'Books', 10),
  order('o2', 'NA', 'Books', 20),
  order('o3', 'NA', 'Music', 5),
  order('o4', 'EU', 'Books', 30),
  order('o5', 'EU', 'Music', 7),
];

// jsdom doesn't fully model DataTransfer; supply a minimal stub so the
// drag events round-trip a payload.
function makeDataTransfer() {
  const map = new Map<string, string>();
  return {
    setData: (k: string, v: string) => {
      map.set(k, v);
    },
    getData: (k: string) => map.get(k) ?? '',
    types: [] as string[],
    effectAllowed: '',
    dropEffect: '',
  };
}

function dragField(
  source: HTMLElement,
  target: HTMLElement,
  dataTransfer: ReturnType<typeof makeDataTransfer>,
) {
  fireEvent.dragStart(source, { dataTransfer });
  // Keep types in sync so DragOver acceptance check passes.
  dataTransfer.types = ['application/x-weave-pivot'];
  fireEvent.dragOver(target, { dataTransfer });
  fireEvent.drop(target, { dataTransfer });
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

describe('PivotTable', () => {
  it('shows an empty placeholder until both a dimension and a value are placed', () => {
    render(<PivotTable objectType={orderType} data={sample} />);
    expect(screen.getByTestId('pivot-empty')).toBeInTheDocument();
    expect(screen.queryByTestId('pivot-grid')).not.toBeInTheDocument();
  });

  it('lists every non-PK property in the Available zone', () => {
    render(<PivotTable objectType={orderType} data={sample} />);
    const available = screen.getByTestId('pivot-available');
    const fields = within(available)
      .getAllByTestId('pivot-field-available')
      .map((el) => el.getAttribute('data-field'));
    expect(fields.sort()).toEqual(['amount', 'category', 'region']);
  });

  it('aggregates count by single row dimension when a value is placed', () => {
    render(<PivotTable objectType={orderType} data={sample} />);

    const dt = makeDataTransfer();
    const region = within(screen.getByTestId('pivot-available')).getByText(
      'region',
    ).parentElement!;
    dragField(region, screen.getByTestId('pivot-zone-rows'), dt);

    const dt2 = makeDataTransfer();
    const amount = within(screen.getByTestId('pivot-available')).getByText(
      'amount',
    ).parentElement!;
    dragField(amount, screen.getByTestId('pivot-zone-values'), dt2);

    expect(screen.getByTestId('pivot-grid')).toBeInTheDocument();

    const rows = screen.getAllByTestId('pivot-row');
    const rowKeys = rows
      .map((r) => r.getAttribute('data-row-key'))
      .sort();
    expect(rowKeys).toEqual(['EU', 'NA']);

    // amount lands in 'values' as 'sum' (numeric default).
    const valueChip = screen.getByTestId('pivot-field-values');
    expect(valueChip.getAttribute('data-aggregation')).toBe('sum');

    const totals = screen.getAllByTestId('pivot-row-total');
    const byRow: Record<string, number> = {};
    for (const t of totals) {
      const k = t.getAttribute('data-row-key')!;
      byRow[k] = Number(t.textContent);
    }
    expect(byRow['NA']).toBe(35); // 10+20+5
    expect(byRow['EU']).toBe(37); // 30+7
  });

  it('cross-tabs rows × columns and reports per-cell sum', () => {
    render(<PivotTable objectType={orderType} data={sample} />);

    const dt1 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('region')
        .parentElement!,
      screen.getByTestId('pivot-zone-rows'),
      dt1,
    );
    const dt2 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('category')
        .parentElement!,
      screen.getByTestId('pivot-zone-columns'),
      dt2,
    );
    const dt3 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('amount')
        .parentElement!,
      screen.getByTestId('pivot-zone-values'),
      dt3,
    );

    const cells = screen.getAllByTestId('pivot-cell');
    const summary = cells.map((c) => ({
      row: c.getAttribute('data-row-key'),
      col: c.getAttribute('data-col-key'),
      value: c.textContent,
    }));
    expect(summary).toEqual(
      expect.arrayContaining([
        { row: 'NA', col: 'Books', value: '30' },
        { row: 'NA', col: 'Music', value: '5' },
        { row: 'EU', col: 'Books', value: '30' },
        { row: 'EU', col: 'Music', value: '7' },
      ]),
    );
  });

  it('switches the aggregation for a value chip when the dropdown changes', () => {
    render(<PivotTable objectType={orderType} data={sample} />);

    const dt1 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('region')
        .parentElement!,
      screen.getByTestId('pivot-zone-rows'),
      dt1,
    );
    const dt2 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('amount')
        .parentElement!,
      screen.getByTestId('pivot-zone-values'),
      dt2,
    );

    const select = screen.getByLabelText(
      'Aggregation for amount',
    ) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: 'avg' } });

    const totals = screen.getAllByTestId('pivot-row-total');
    const byRow: Record<string, string> = {};
    for (const t of totals) {
      const k = t.getAttribute('data-row-key')!;
      byRow[k] = t.textContent ?? '';
    }
    // NA: avg(10,20,5) = 11.67; EU: avg(30,7) = 18.50
    expect(byRow['NA']).toBe('11.67');
    expect(byRow['EU']).toBe('18.50');
  });

  it('forwards the first row of a row-group on click when onRowClick is supplied', () => {
    const onRowClick = vi.fn();
    render(
      <PivotTable
        objectType={orderType}
        data={sample}
        onRowClick={onRowClick}
      />,
    );

    const dt1 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('region')
        .parentElement!,
      screen.getByTestId('pivot-zone-rows'),
      dt1,
    );
    const dt2 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('amount')
        .parentElement!,
      screen.getByTestId('pivot-zone-values'),
      dt2,
    );

    const naRow = screen
      .getAllByTestId('pivot-row')
      .find((r) => r.getAttribute('data-row-key') === 'NA')!;
    fireEvent.click(naRow);
    expect(onRowClick).toHaveBeenCalledTimes(1);
    expect(onRowClick.mock.calls[0][0].__primaryKey).toBe('o1');
  });

  it('removes a placed field via the × affordance and returns it to Available', () => {
    render(<PivotTable objectType={orderType} data={sample} />);

    const dt1 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('region')
        .parentElement!,
      screen.getByTestId('pivot-zone-rows'),
      dt1,
    );

    const placed = screen.getByTestId('pivot-field-rows');
    expect(placed.getAttribute('data-field')).toBe('region');
    fireEvent.click(within(placed).getByLabelText('Remove region'));

    expect(screen.queryByTestId('pivot-field-rows')).not.toBeInTheDocument();
    const available = screen.getByTestId('pivot-available');
    const fields = within(available)
      .getAllByTestId('pivot-field-available')
      .map((el) => el.getAttribute('data-field'));
    expect(fields).toContain('region');
  });

  it('rejects a numeric-only field dropped on the Rows zone', () => {
    render(<PivotTable objectType={orderType} data={sample} />);

    const dt = makeDataTransfer();
    const amount = within(screen.getByTestId('pivot-available')).getByText(
      'amount',
    ).parentElement!;
    dragField(amount, screen.getByTestId('pivot-zone-rows'), dt);

    // amount should NOT have moved into Rows; remains in Available.
    expect(screen.queryByTestId('pivot-field-rows')).not.toBeInTheDocument();
    const available = screen.getByTestId('pivot-available');
    const fields = within(available)
      .getAllByTestId('pivot-field-available')
      .map((el) => el.getAttribute('data-field'));
    expect(fields).toContain('amount');
  });

  it('emits a grand total row that sums every column-group total', () => {
    render(<PivotTable objectType={orderType} data={sample} />);

    const dt1 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('region')
        .parentElement!,
      screen.getByTestId('pivot-zone-rows'),
      dt1,
    );
    const dt2 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('category')
        .parentElement!,
      screen.getByTestId('pivot-zone-columns'),
      dt2,
    );
    const dt3 = makeDataTransfer();
    dragField(
      within(screen.getByTestId('pivot-available')).getByText('amount')
        .parentElement!,
      screen.getByTestId('pivot-zone-values'),
      dt3,
    );

    const grandRow = screen.getByTestId('pivot-grand-total');
    const colTotals = within(grandRow)
      .getAllByTestId('pivot-col-total')
      .map((el) => ({
        col: el.getAttribute('data-col-key'),
        value: Number(el.textContent),
      }));
    expect(colTotals).toEqual(
      expect.arrayContaining([
        { col: 'Books', value: 60 }, // 10+20+30
        { col: 'Music', value: 12 }, // 5+7
      ]),
    );
    const grand = within(grandRow).getByTestId('pivot-grand-cell');
    expect(Number(grand.textContent)).toBe(72);
  });
});
