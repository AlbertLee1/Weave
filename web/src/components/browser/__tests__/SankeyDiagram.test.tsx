import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SankeyDiagram } from '../SankeyDiagram';
import type { ObjectType, WireObject } from '../../../api/types';

// -----------------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------------

const shipmentType: ObjectType = {
  rid: 'ri.ot.shipment',
  apiName: 'Shipment',
  displayName: 'Shipment',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    source: { dataType: { type: 'string' }, rid: 'ri.p.source' },
    target: { dataType: { type: 'string' }, rid: 'ri.p.target' },
  },
};

const fallbackPairType: ObjectType = {
  rid: 'ri.ot.fallback',
  apiName: 'Fallback',
  displayName: 'Fallback',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    // Two unrelated string fields — the component should fall back to
    // declaration order when no canonical pair name matches.
    department: { dataType: { type: 'string' }, rid: 'ri.p.department' },
    region: { dataType: { type: 'string' }, rid: 'ri.p.region' },
  },
};

const onlyOneStringType: ObjectType = {
  rid: 'ri.ot.singular',
  apiName: 'Singular',
  displayName: 'Singular',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
    quantity: { dataType: { type: 'integer' }, rid: 'ri.p.quantity' },
  },
};

function row(id: string, source: string, target: string): WireObject {
  return {
    __rid: `ri.o.${id}`,
    __primaryKey: id,
    __apiName: 'Shipment',
    id,
    source,
    target,
  };
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

describe('SankeyDiagram', () => {
  it('renders a no-pair empty state when the type lacks two categorical properties', () => {
    render(<SankeyDiagram objectType={onlyOneStringType} data={[]} />);
    expect(screen.getByTestId('sankey-empty-no-pair')).toBeInTheDocument();
    expect(screen.queryByTestId('sankey-chart')).not.toBeInTheDocument();
  });

  it('renders a no-data empty state when the pair exists but no rows aggregate', () => {
    render(
      <SankeyDiagram
        objectType={shipmentType}
        data={[row('only-one', 'A', '')]}
      />,
    );
    expect(screen.getByTestId('sankey-empty-no-data')).toBeInTheDocument();
    expect(screen.queryByTestId('sankey-chart')).not.toBeInTheDocument();
  });

  it('detects the canonical (source, target) pair and aggregates by row count', () => {
    render(
      <SankeyDiagram
        objectType={shipmentType}
        data={[
          row('a1', 'NY', 'LA'),
          row('a2', 'NY', 'LA'),
          row('a3', 'NY', 'SF'),
          row('a4', 'BO', 'LA'),
        ]}
      />,
    );
    const chart = screen.getByTestId('sankey-chart');
    expect(chart.getAttribute('data-source-field')).toBe('source');
    expect(chart.getAttribute('data-target-field')).toBe('target');

    const flows = screen.getAllByTestId('sankey-flow');
    expect(flows).toHaveLength(3);
    const summary = flows.map((f) => ({
      source: f.getAttribute('data-source'),
      target: f.getAttribute('data-target'),
      count: f.getAttribute('data-count'),
    }));
    expect(summary).toEqual(
      expect.arrayContaining([
        { source: 'NY', target: 'LA', count: '2' },
        { source: 'NY', target: 'SF', count: '1' },
        { source: 'BO', target: 'LA', count: '1' },
      ]),
    );
  });

  it('renders one node per distinct value on each side', () => {
    render(
      <SankeyDiagram
        objectType={shipmentType}
        data={[row('a1', 'NY', 'LA'), row('a2', 'BO', 'LA'), row('a3', 'NY', 'SF')]}
      />,
    );
    const sourceNodes = screen
      .getAllByTestId('sankey-node')
      .filter((n) => n.getAttribute('data-side') === 'source');
    const targetNodes = screen
      .getAllByTestId('sankey-node')
      .filter((n) => n.getAttribute('data-side') === 'target');
    expect(sourceNodes.map((n) => n.getAttribute('data-name')).sort()).toEqual([
      'BO',
      'NY',
    ]);
    expect(targetNodes.map((n) => n.getAttribute('data-name')).sort()).toEqual([
      'LA',
      'SF',
    ]);
  });

  it('falls back to the first two declaration-order categorical fields when no canonical pair matches', () => {
    const r: WireObject = {
      __rid: 'ri.o.f1',
      __primaryKey: 'f1',
      __apiName: 'Fallback',
      id: 'f1',
      department: 'Eng',
      region: 'EU',
    };
    render(<SankeyDiagram objectType={fallbackPairType} data={[r]} />);
    const chart = screen.getByTestId('sankey-chart');
    expect(chart.getAttribute('data-source-field')).toBe('department');
    expect(chart.getAttribute('data-target-field')).toBe('region');
    expect(screen.getAllByTestId('sankey-flow')).toHaveLength(1);
  });

  it('forwards the first row of a flow on click when onRowClick is supplied', () => {
    const onRowClick = vi.fn();
    const first = row('a1', 'NY', 'LA');
    render(
      <SankeyDiagram
        objectType={shipmentType}
        data={[first, row('a2', 'NY', 'LA')]}
        onRowClick={onRowClick}
      />,
    );
    const flow = screen.getByTestId('sankey-flow');
    expect(flow.getAttribute('role')).toBe('button');
    fireEvent.click(flow);
    expect(onRowClick).toHaveBeenCalledWith(first);
  });

  it('skips rows where either side is empty', () => {
    render(
      <SankeyDiagram
        objectType={shipmentType}
        data={[
          row('a1', 'NY', 'LA'),
          row('a2', '', 'LA'),
          row('a3', 'NY', ''),
          row('a4', 'BO', 'LA'),
        ]}
      />,
    );
    const flows = screen.getAllByTestId('sankey-flow');
    const counts = flows.map((f) => Number(f.getAttribute('data-count')));
    const total = counts.reduce((acc, c) => acc + c, 0);
    expect(total).toBe(2);
  });
});
