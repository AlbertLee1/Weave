import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { EMPTY_SELECTION, type SelectionState } from '../features/vertex/selections/selectionState';
import type { ExtendedLabel } from '../features/vertex/render/extendedLabels';
import { VertexSelectionSidebar, type VertexObjectSummary } from './VertexSelectionSidebar';

const jfk: VertexObjectSummary = {
  rid: 'ri.ontology.main.object.airport.JFK',
  label: 'JFK',
  properties: { name: 'JFK', city: 'New York', onTimePct: 92 },
  ontologyApiName: 'flights',
  objectType: 'Airport',
  primaryKey: 'JFK',
};

const lhr: VertexObjectSummary = {
  rid: 'ri.ontology.main.object.airport.LHR',
  label: 'LHR',
  properties: { name: 'LHR', city: 'London' },
  ontologyApiName: 'flights',
  objectType: 'Airport',
  primaryKey: 'LHR',
};

interface FetchCalls {
  ossGets: string[];
  activityGets: string[];
  timeseriesPosts: string[];
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function installFetch(calls: FetchCalls, options?: {
  ossOverride?: Record<string, unknown>;
  events?: Array<Record<string, unknown>>;
  timeseries?: Record<string, Array<{ time: string; value: number }>>;
}) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();
      // OSS getObject — GET /api/v2/ontologies/{ont}/objects/{type}/{pk}
      const getMatch = url.match(/\/api\/v2\/ontologies\/[^/]+\/objects\/[^/]+\/([^/?#]+)(?:[?#]|$)/);
      if (method === 'GET' && getMatch) {
        calls.ossGets.push(url);
        return jsonResponse(
          options?.ossOverride ?? {
            __rid: jfk.rid,
            __primaryKey: 'JFK',
            __apiName: 'Airport',
            name: 'JFK Fresh',
            city: 'New York Fresh',
            onTimePct: 95,
          },
        );
      }
      // Object activity — GET .../activity
      if (method === 'GET' && url.includes('/activity')) {
        calls.activityGets.push(url);
        return jsonResponse({
          data: options?.events ?? [
            {
              id: 'evt-1',
              objectTypeRid: 'ri.ontology.main.object-type.airport',
              primaryKey: 'JFK',
              version: 3,
              editType: 'MODIFY',
              recordedAt: '2026-05-14T10:00:00Z',
            },
          ],
        });
      }
      // Timeseries streamPoints — POST .../timeseries/{property}/streamPoints
      const tsMatch = url.match(/\/timeseries\/([^/?#]+)\/streamPoints/);
      if (method === 'POST' && tsMatch) {
        calls.timeseriesPosts.push(url);
        const prop = decodeURIComponent(tsMatch[1]);
        const points =
          options?.timeseries?.[prop] ?? [
            { time: '2026-05-14T10:00:00Z', value: 1 },
            { time: '2026-05-14T11:00:00Z', value: 4 },
            { time: '2026-05-14T12:00:00Z', value: 2 },
          ];
        return jsonResponse(points);
      }
      return new Response('{}', { status: 200 });
    }),
  );
}

function makeQC() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function renderSidebar(
  selection: SelectionState,
  objectsByRid: ReadonlyMap<string, VertexObjectSummary>,
  extendedLabelsByRid?: ReadonlyMap<string, ExtendedLabel[]>,
) {
  const qc = makeQC();
  return render(
    <QueryClientProvider client={qc}>
      <VertexSelectionSidebar
        selection={selection}
        objectsByRid={objectsByRid}
        extendedLabelsByRid={extendedLabelsByRid}
      />
    </QueryClientProvider>,
  );
}

describe('VertexSelectionSidebar — VTX-020 baseline', () => {
  let calls: FetchCalls;
  beforeEach(() => {
    calls = { ossGets: [], activityGets: [], timeseriesPosts: [] };
    installFetch(calls);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('Given_emptySelection_When_render_Then_sidebarIsHidden', () => {
    renderSidebar(EMPTY_SELECTION, new Map([[jfk.rid, jfk]]));
    expect(screen.queryByTestId('vertex-selection-sidebar')).not.toBeInTheDocument();
  });

  it('Given_singleSelectedNode_When_render_Then_headerShowsLabel', () => {
    const sel: SelectionState = new Set([jfk.rid]);
    renderSidebar(sel, new Map([[jfk.rid, jfk]]));
    expect(screen.getByTestId('vertex-selection-sidebar')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-selection-sidebar-header').textContent).toContain('JFK');
  });

  it('Given_multipleSelectedNodes_When_render_Then_sidebarShowsBatchPanel', () => {
    const sel: SelectionState = new Set([jfk.rid, lhr.rid]);
    renderSidebar(sel, new Map([[jfk.rid, jfk], [lhr.rid, lhr]]));
    expect(screen.getByTestId('vertex-selection-sidebar-batch')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-selection-sidebar-count').textContent).toContain('2');
    expect(screen.getByText('JFK')).toBeInTheDocument();
    expect(screen.getByText('LHR')).toBeInTheDocument();
  });

  it('Given_selectedRidWithoutObjectSummary_When_render_Then_fallsBackToRidLabel', () => {
    const sel: SelectionState = new Set(['ri.unknown']);
    renderSidebar(sel, new Map());
    const header = screen.getByTestId('vertex-selection-sidebar-header');
    expect(header.textContent).toContain('ri.unknown');
  });
});

describe('VertexSelectionSidebar — VTX-021 four-tab panel', () => {
  let calls: FetchCalls;
  beforeEach(() => {
    calls = { ossGets: [], activityGets: [], timeseriesPosts: [] };
    installFetch(calls);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('Given_singleSelectedObject_When_sidebarRenders_Then_showsFourTabs', () => {
    const sel: SelectionState = new Set([jfk.rid]);
    renderSidebar(sel, new Map([[jfk.rid, jfk]]));
    expect(screen.getByTestId('vertex-sidebar-tab-properties')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-sidebar-tab-series')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-sidebar-tab-linkedEvents')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-sidebar-tab-derivedFuncs')).toBeInTheDocument();
  });

  it('Given_propertiesTabActive_When_summaryHasApiNames_Then_fetchesOSSGetAndListsFreshValues', async () => {
    const sel: SelectionState = new Set([jfk.rid]);
    renderSidebar(sel, new Map([[jfk.rid, jfk]]));
    await waitFor(() => {
      expect(calls.ossGets.length).toBeGreaterThan(0);
    });
    // OSS-fetched value rendered.
    await waitFor(() => {
      expect(screen.getByText('JFK Fresh')).toBeInTheDocument();
    });
    // The OSS path matches /api/v2/ontologies/flights/objects/Airport/JFK.
    expect(calls.ossGets[0]).toContain('/api/v2/ontologies/flights/objects/Airport/JFK');
  });

  it('Given_summaryWithoutApiNames_When_propertiesTabRenders_Then_fallsBackToSnapshotPropertiesWithoutFetching', async () => {
    const sel: SelectionState = new Set([jfk.rid]);
    const partial: VertexObjectSummary = {
      rid: jfk.rid,
      label: jfk.label,
      properties: jfk.properties,
    };
    renderSidebar(sel, new Map([[partial.rid, partial]]));
    expect(screen.getByText('city')).toBeInTheDocument();
    expect(screen.getByText('New York')).toBeInTheDocument();
    // No OSS call possible without api metadata.
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(calls.ossGets.length).toBe(0);
  });

  it('Given_twoTimeSeriesLabels_When_seriesTabIsActivated_Then_displaysTwoSparklines', async () => {
    const sel: SelectionState = new Set([jfk.rid]);
    const labels: ExtendedLabel[] = [
      { key: 'timeSeries:onTime:0', kind: 'timeSeries', label: 'onTimePct' },
      { key: 'timeSeries:delay:1', kind: 'timeSeries', label: 'delayMin' },
    ];
    renderSidebar(sel, new Map([[jfk.rid, jfk]]), new Map([[jfk.rid, labels]]));

    await userEvent.click(screen.getByTestId('vertex-sidebar-tab-series'));

    await waitFor(() => {
      expect(screen.getAllByTestId('vertex-sidebar-series-sparkline').length).toBe(2);
    });
    // Two timeseries fetches, one per property.
    expect(calls.timeseriesPosts.length).toBe(2);
  });

  it('Given_linkedEventsTabActive_When_activityFetched_Then_listsRecentEvents', async () => {
    const sel: SelectionState = new Set([jfk.rid]);
    renderSidebar(sel, new Map([[jfk.rid, jfk]]));

    await userEvent.click(screen.getByTestId('vertex-sidebar-tab-linkedEvents'));

    await waitFor(() => {
      expect(calls.activityGets.length).toBeGreaterThan(0);
    });
    // Activity API requests pageSize=50 (cap per BDD "最近 50 个事件").
    expect(calls.activityGets[0]).toMatch(/[?&]pageSize=50/);
    await waitFor(() => {
      expect(screen.getByTestId('vertex-sidebar-linked-events-list')).toBeInTheDocument();
      expect(screen.getByText(/MODIFY/i)).toBeInTheDocument();
    });
  });

  it('Given_measureLabels_When_derivedFuncsTabActive_Then_listsMeasureCards', async () => {
    const sel: SelectionState = new Set([jfk.rid]);
    const labels: ExtendedLabel[] = [
      { key: 'measure:ri.f.avg:0', kind: 'measure', label: 'avgDelay' },
      { key: 'measure:ri.f.max:1', kind: 'measure', label: 'maxDelay' },
    ];
    renderSidebar(sel, new Map([[jfk.rid, jfk]]), new Map([[jfk.rid, labels]]));

    await userEvent.click(screen.getByTestId('vertex-sidebar-tab-derivedFuncs'));

    await waitFor(() => {
      expect(screen.getByText('avgDelay')).toBeInTheDocument();
      expect(screen.getByText('maxDelay')).toBeInTheDocument();
    });
  });

  it('Given_clickPropertiesTabAfterSeries_When_switchBack_Then_propertiesAreVisibleAgain', async () => {
    const sel: SelectionState = new Set([jfk.rid]);
    renderSidebar(sel, new Map([[jfk.rid, jfk]]));

    await userEvent.click(screen.getByTestId('vertex-sidebar-tab-series'));
    await userEvent.click(screen.getByTestId('vertex-sidebar-tab-properties'));

    // Default snapshot property bag is back on screen.
    expect(screen.getByText('city')).toBeInTheDocument();
  });
});
