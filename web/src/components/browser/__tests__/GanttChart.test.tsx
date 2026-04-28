import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { GanttChart } from '../GanttChart';
import type { ObjectType, WireObject } from '../../../api/types';

// -----------------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------------

const projectType: ObjectType = {
  rid: 'ri.ot.project',
  apiName: 'Project',
  displayName: 'Project',
  primaryKey: 'id',
  titleProperty: 'name',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
    startDate: { dataType: { type: 'date' }, rid: 'ri.p.startDate' },
    endDate: { dataType: { type: 'date' }, rid: 'ri.p.endDate' },
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
    // Two unrelated temporal fields — the component should fall back to
    // declaration order when no canonical pair name matches.
    createdAt: { dataType: { type: 'datetime' }, rid: 'ri.p.createdAt' },
    closedAt: { dataType: { type: 'datetime' }, rid: 'ri.p.closedAt' },
  },
};

const noTemporalType: ObjectType = {
  rid: 'ri.ot.plain',
  apiName: 'Plain',
  displayName: 'Plain',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
  },
};

const rowAlpha: WireObject = {
  __rid: 'ri.o.alpha',
  __primaryKey: 'alpha',
  __apiName: 'Project',
  id: 'alpha',
  name: 'Alpha',
  startDate: '2025-01-01',
  endDate: '2025-03-15',
};

const rowBeta: WireObject = {
  __rid: 'ri.o.beta',
  __primaryKey: 'beta',
  __apiName: 'Project',
  id: 'beta',
  name: 'Beta',
  startDate: '2025-02-01',
  endDate: '2025-04-30',
};

const rowMissingDates: WireObject = {
  __rid: 'ri.o.gamma',
  __primaryKey: 'gamma',
  __apiName: 'Project',
  id: 'gamma',
  name: 'Gamma',
  // No startDate / endDate — should be silently skipped.
};

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

describe('GanttChart', () => {
  it('renders a no-pair empty state when the type has no temporal properties', () => {
    render(<GanttChart objectType={noTemporalType} data={[]} />);
    expect(screen.getByTestId('gantt-empty-no-pair')).toBeInTheDocument();
    expect(screen.queryByTestId('gantt-chart')).not.toBeInTheDocument();
  });

  it('renders a no-data empty state when temporal pair exists but rows lack values', () => {
    render(
      <GanttChart objectType={projectType} data={[rowMissingDates]} />,
    );
    expect(screen.getByTestId('gantt-empty-no-data')).toBeInTheDocument();
  });

  it('detects the canonical startDate/endDate pair and renders one bar per parseable row', () => {
    render(
      <GanttChart
        objectType={projectType}
        data={[rowAlpha, rowBeta, rowMissingDates]}
      />,
    );
    const chart = screen.getByTestId('gantt-chart');
    expect(chart.getAttribute('data-start-field')).toBe('startDate');
    expect(chart.getAttribute('data-end-field')).toBe('endDate');
    const rows = screen.getAllByTestId('gantt-row');
    // gamma is dropped because both date fields are missing
    expect(rows).toHaveLength(2);
    const pks = rows.map((r) => r.getAttribute('data-pk'));
    expect(pks).toEqual(['alpha', 'beta']);
  });

  it('falls back to the first two declaration-order temporal fields when no canonical pair matches', () => {
    const row: WireObject = {
      __rid: 'ri.o.f1',
      __primaryKey: 'f1',
      __apiName: 'Fallback',
      id: 'f1',
      createdAt: '2025-01-01T08:00:00Z',
      closedAt: '2025-01-02T18:30:00Z',
    };
    render(<GanttChart objectType={fallbackPairType} data={[row]} />);
    const chart = screen.getByTestId('gantt-chart');
    expect(chart.getAttribute('data-start-field')).toBe('createdAt');
    expect(chart.getAttribute('data-end-field')).toBe('closedAt');
    expect(screen.getAllByTestId('gantt-row')).toHaveLength(1);
  });

  it('makes each row activatable and forwards the underlying WireObject on click', () => {
    const onRowClick = vi.fn();
    render(
      <GanttChart
        objectType={projectType}
        data={[rowAlpha]}
        onRowClick={onRowClick}
      />,
    );
    const row = screen.getByTestId('gantt-row');
    expect(row.getAttribute('role')).toBe('button');
    fireEvent.click(row);
    expect(onRowClick).toHaveBeenCalledWith(rowAlpha);
  });

  it('renders an axis with at least one tick', () => {
    render(<GanttChart objectType={projectType} data={[rowAlpha]} />);
    const ticks = screen.getAllByTestId('gantt-tick');
    expect(ticks.length).toBeGreaterThan(0);
  });

  it('shows the row title (titleProperty) and falls back to the primary key when missing', () => {
    const titledRow: WireObject = {
      __rid: 'ri.o.x',
      __primaryKey: 'x',
      __apiName: 'Project',
      id: 'x',
      name: 'Quarterly Plan',
      startDate: '2025-01-01',
      endDate: '2025-01-15',
    };
    const untitledRow: WireObject = {
      __rid: 'ri.o.y',
      __primaryKey: 'y-pk',
      __apiName: 'Project',
      id: 'y-pk',
      // name absent → label falls through to __primaryKey
      startDate: '2025-02-01',
      endDate: '2025-02-10',
    };
    render(
      <GanttChart objectType={projectType} data={[titledRow, untitledRow]} />,
    );
    const chart = screen.getByTestId('gantt-chart');
    expect(within(chart).getByText('Quarterly Plan')).toBeInTheDocument();
    expect(within(chart).getByText('y-pk')).toBeInTheDocument();
  });
});
