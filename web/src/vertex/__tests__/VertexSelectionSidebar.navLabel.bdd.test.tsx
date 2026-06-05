// BDD: the Vertex selection sidebar's tab <nav> must carry an accessible
// name so it is distinguishable from the page's "Main navigation" landmark
// (a11y, navigation landmark naming). a11y fix — goal dimension ③.
//
// Given a single selected node renders VertexSelectionSidebar,
// Then the tab navigation exposes an accessible name (aria-label) matching
// /selection|vertex/i so screen-reader landmark navigation is unambiguous.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { SelectionState } from '../../features/vertex/selections/selectionState';
import { VertexSelectionSidebar, type VertexObjectSummary } from '../VertexSelectionSidebar';

const jfk: VertexObjectSummary = {
  rid: 'ri.ontology.main.object.airport.JFK',
  label: 'JFK',
  properties: { name: 'JFK', city: 'New York' },
  ontologyApiName: 'flights',
  objectType: 'Airport',
  primaryKey: 'JFK',
};

function makeQC() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderSidebar(selection: SelectionState, objectsByRid: ReadonlyMap<string, VertexObjectSummary>) {
  const qc = makeQC();
  return render(
    <QueryClientProvider client={qc}>
      <VertexSelectionSidebar selection={selection} objectsByRid={objectsByRid} />
    </QueryClientProvider>,
  );
}

describe('VertexSelectionSidebar — tab nav landmark accessible name (a11y)', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (): Promise<Response> => new Response('{}', { status: 200 })),
    );
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('Given_singleSelectedNode_When_render_Then_tabNavHasAccessibleName', () => {
    const sel: SelectionState = new Set([jfk.rid]);
    renderSidebar(sel, new Map([[jfk.rid, jfk]]));

    // The tab <nav> is a navigation landmark; it must be queryable by its
    // accessible name (sourced from aria-label).
    const nav = screen.getByRole('navigation', { name: /selection|vertex/i });
    expect(nav).toBeInTheDocument();
    expect(nav).toHaveAttribute('data-testid', 'vertex-sidebar-tabs');
  });
});
