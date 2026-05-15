import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import { EMPTY_SELECTION, type SelectionState } from '../features/vertex/selections/selectionState';
import { VertexSelectionSidebar, type VertexObjectSummary } from './VertexSelectionSidebar';

const jfk: VertexObjectSummary = {
  rid: 'ri.airport.JFK',
  label: 'JFK',
  properties: { name: 'JFK', city: 'New York', onTimePct: 92 },
};

const lhr: VertexObjectSummary = {
  rid: 'ri.airport.LHR',
  label: 'LHR',
  properties: { name: 'LHR', city: 'London' },
};

describe('VTX-020 VertexSelectionSidebar', () => {
  it('Given_emptySelection_When_render_Then_sidebarIsHidden', () => {
    render(
      <VertexSelectionSidebar
        selection={EMPTY_SELECTION}
        objectsByRid={new Map([[jfk.rid, jfk]])}
      />,
    );
    expect(screen.queryByTestId('vertex-selection-sidebar')).not.toBeInTheDocument();
  });

  it('Given_singleSelectedNode_When_render_Then_sidebarShowsPropertiesPanel', () => {
    const sel: SelectionState = new Set([jfk.rid]);
    render(
      <VertexSelectionSidebar
        selection={sel}
        objectsByRid={new Map([[jfk.rid, jfk]])}
      />,
    );

    expect(screen.getByTestId('vertex-selection-sidebar')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-selection-sidebar-header').textContent).toContain('JFK');
    // Property rows visible.
    expect(screen.getByText('city')).toBeInTheDocument();
    expect(screen.getByText('New York')).toBeInTheDocument();
    expect(screen.getByText('onTimePct')).toBeInTheDocument();
    expect(screen.getByText('92')).toBeInTheDocument();
  });

  it('Given_multipleSelectedNodes_When_render_Then_sidebarShowsBatchPanel', () => {
    const sel: SelectionState = new Set([jfk.rid, lhr.rid]);
    render(
      <VertexSelectionSidebar
        selection={sel}
        objectsByRid={new Map([[jfk.rid, jfk], [lhr.rid, lhr]])}
      />,
    );

    expect(screen.getByTestId('vertex-selection-sidebar')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-selection-sidebar-batch')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-selection-sidebar-count').textContent).toContain('2');
    // Both rids appear in the list.
    expect(screen.getByText('JFK')).toBeInTheDocument();
    expect(screen.getByText('LHR')).toBeInTheDocument();
    // Single-object Properties panel is not rendered in batch mode.
    expect(screen.queryByText('onTimePct')).not.toBeInTheDocument();
  });

  it('Given_selectedRidWithoutObjectSummary_When_render_Then_fallsBackToRidLabel', () => {
    const sel: SelectionState = new Set(['ri.unknown']);
    render(<VertexSelectionSidebar selection={sel} objectsByRid={new Map()} />);
    const header = screen.getByTestId('vertex-selection-sidebar-header');
    expect(header.textContent).toContain('ri.unknown');
  });
});
