import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act, fireEvent } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import Graph from 'graphology';

// jsdom can't WebGL, so the SigmaContainer render is replaced with a
// stable placeholder div the tests can assert against. useLoadGraph is
// captured into the module-scoped `loadedGraph` ref so VTX-018 assertions
// can introspect the loaded node/edge sets without booting Sigma.
let loadedGraph: Graph | null = null;
let afterRenderHandler: (() => void) | null = null;
const captureLoad = (g: Graph) => {
  loadedGraph = g;
  // Sigma fires afterRender on every render frame in production —
  // GraphLoader's useLoadGraph would normally trigger one synchronous
  // paint cycle. In jsdom there is no paint, so emulate it: as soon as
  // a graph is loaded, fire the captured afterRender handler so the
  // VertexNodeOverlay re-renders against the now-populated graph.
  if (afterRenderHandler) {
    queueMicrotask(afterRenderHandler);
  }
};

// VTX-019: the VertexNodeOverlay child of SigmaContainer also calls
// useSigma + useRegisterEvents. Stub both with deterministic shapes
// (1:1 graphToViewport, 800×600 viewport) so the overlay paints cards
// for each node loaded into the captured graph.
// VTX-020: multiple children (overlay + selection layer + highlighter)
// each call useRegisterEvents — merge each new handler set onto the
// captured handler map so tests can fire clickNode without losing
// afterRender.
const emptyGraphStub = {
  hasNode: () => false,
  getNodeAttribute: () => undefined,
  forEachNode: () => {},
  setNodeAttribute: () => {},
  nodes: () => [] as string[],
};
const sigmaContainer = document.createElement('div');
Object.defineProperty(sigmaContainer, 'getBoundingClientRect', {
  value: () => ({ left: 0, top: 0, width: 800, height: 600, right: 800, bottom: 600 }),
  configurable: true,
});
const sigmaStub = {
  graphToViewport: (p: { x: number; y: number }) => ({ x: p.x, y: p.y }),
  // VTX-024: drag conversion uses viewportToGraph. Mirror the identity
  // graphToViewport stub so test math is predictable.
  viewportToGraph: (p: { x: number; y: number }) => ({ x: p.x, y: p.y }),
  getDimensions: () => ({ width: 800, height: 600 }),
  getGraph: () => loadedGraph ?? emptyGraphStub,
  getContainer: () => sigmaContainer,
  refresh: () => {},
};

let capturedHandlers: Record<string, ((p: unknown) => void) | undefined> = {};

vi.mock('@react-sigma/core', () => ({
  SigmaContainer: ({ children, style }: { children?: React.ReactNode; style?: React.CSSProperties }) => (
    <div data-testid="vertex-canvas-mock" style={style}>
      {children}
    </div>
  ),
  useLoadGraph: () => captureLoad,
  useSigma: () => sigmaStub,
  useRegisterEvents: () =>
    (handlers: Record<string, ((p: unknown) => void) | undefined>) => {
      Object.assign(capturedHandlers, handlers);
      if (typeof handlers.afterRender === 'function') {
        afterRenderHandler = handlers.afterRender as () => void;
      }
    },
}));

import { VertexWorkspacePage } from './VertexWorkspacePage';

function renderAt(path: string) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/vertex/:rid" element={<VertexWorkspacePage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const realFetch = globalThis.fetch;

beforeEach(() => {
  loadedGraph = null;
  afterRenderHandler = null;
  capturedHandlers = {};
  globalThis.fetch = vi.fn() as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.clearAllMocks();
});

describe('VertexWorkspacePage (VTX-017)', () => {
  it('Given /vertex/new When mount Then shows empty canvas + 5 TopBar buttons', async () => {
    renderAt('/vertex/new');
    expect(screen.getByTestId('vertex-canvas-mock')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-topbar')).toBeInTheDocument();
    for (const id of [
      'vertex-topbar-save',
      'vertex-topbar-share',
      'vertex-topbar-layout',
      'vertex-topbar-time-selection',
      'vertex-topbar-run',
    ]) {
      expect(screen.getByTestId(id)).toBeInTheDocument();
    }
    // /vertex/new must NOT trigger a backend fetch.
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it('Given /vertex/{rid} that 404s When mount Then shows "Graph not found" + back-home button', async () => {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      status: 404,
      statusText: 'Not Found',
      text: async () => '{"errorCode":"NOT_FOUND","errorName":"GraphNotFound","errorInstanceId":"x"}',
      json: async () => ({ errorCode: 'NOT_FOUND', errorName: 'GraphNotFound', errorInstanceId: 'x' }),
    });

    renderAt('/vertex/ri.vertex.main.graph.unknown');
    await waitFor(() => {
      expect(screen.getByTestId('vertex-not-found')).toBeInTheDocument();
    });
    expect(screen.getByTestId('vertex-not-found').textContent).toContain('Graph not found');
    const home = screen.getByTestId('vertex-not-found-home');
    expect(home.getAttribute('href')).toBe('/');
  });

  it('Given /vertex/{rid} that resolves When mount Then renders the canvas + TopBar', async () => {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () =>
        JSON.stringify({
          rid: 'ri.vertex.main.graph.alpha',
          name: 'Alpha',
          version: 1,
          payload: { layers: [], edges: [], positions: {} },
        }),
      json: async () => ({
        rid: 'ri.vertex.main.graph.alpha',
        name: 'Alpha',
        version: 1,
        payload: { layers: [], edges: [], positions: {} },
      }),
    });

    renderAt('/vertex/ri.vertex.main.graph.alpha');
    await waitFor(() => {
      expect(screen.getByTestId('vertex-canvas-mock')).toBeInTheDocument();
    });
    expect(screen.getByTestId('vertex-topbar')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByTestId('vertex-topbar-graph-name').textContent).toContain('Alpha');
    });
  });

  it('snapshot: /vertex/new shell is stable', () => {
    const { container } = renderAt('/vertex/new');
    expect(container).toMatchSnapshot();
  });
});

describe('VertexWorkspacePage rendering (VTX-018)', () => {
  const fiftyAirportPayload = {
    layers: [
      {
        id: 'layer-airports',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objectType: 'Airport',
        objects: Array.from({ length: 50 }, (_, i) => ({
          objectRid: `ri.ontology.main.object.airport.${i}`,
          properties: { name: `Airport ${i}` },
        })),
      },
    ],
    edges: Array.from({ length: 50 }, (_, i) => ({
      id: `edge-${i}`,
      linkTypeRid: 'ri.ontology.main.link-type.flight',
      source: `ri.ontology.main.object.airport.${i}`,
      target: `ri.ontology.main.object.airport.${(i + 1) % 50}`,
    })),
    positions: {},
  };

  it('Given_payloadWith50Airports_When_pageRenders_Then_loadsGraphWith50NodesIntoSigma', async () => {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () =>
        JSON.stringify({
          rid: 'ri.vertex.main.graph.airports',
          name: 'Airports',
          version: 1,
          payload: fiftyAirportPayload,
        }),
      json: async () => ({
        rid: 'ri.vertex.main.graph.airports',
        name: 'Airports',
        version: 1,
        payload: fiftyAirportPayload,
      }),
    });

    renderAt('/vertex/ri.vertex.main.graph.airports');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(50);
    });
    expect(loadedGraph!.size).toBe(50);
    const someId = 'ri.ontology.main.object.airport.0';
    expect(loadedGraph!.hasNode(someId)).toBe(true);
    expect(loadedGraph!.getNodeAttribute(someId, 'label')).toBe('Airport 0');
  });

  it('Given_payloadEdgesWithDefaultDirection_When_pageRenders_Then_emitsArrowTypeEdges', async () => {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify({ rid: 'r', name: 'r', version: 1, payload: fiftyAirportPayload }),
      json: async () => ({ rid: 'r', name: 'r', version: 1, payload: fiftyAirportPayload }),
    });

    renderAt('/vertex/r');
    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.size).toBe(50);
    });
    // Spot-check the first edge's type attribute — the helper should have
    // mapped the default (no typeClasses) to Sigma's built-in 'arrow' type.
    const firstEdge = loadedGraph!.edges()[0];
    expect(loadedGraph!.getEdgeAttribute(firstEdge, 'type')).toBe('arrow');
  });
});

describe('VertexWorkspacePage extended-label overlay (VTX-019)', () => {
  const payloadWithLabels = {
    layers: [
      {
        id: 'layer-airports',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objectType: 'Airport',
        extendedLabels: [
          { kind: 'property', property: 'onTimePct', label: 'On-time %' },
          { kind: 'timeSeries', property: 'temperatureSeries', label: 'Temp °C' },
          { kind: 'measure', functionRid: 'ri.functions.main.fn.avgDelay' },
        ],
        objects: [
          {
            objectRid: 'ri.ontology.main.object.airport.JFK',
            properties: { name: 'JFK', onTimePct: 92 },
          },
        ],
      },
    ],
    edges: [],
    positions: {},
  };

  it('Given_payloadWith3ExtendedLabels_When_pageRenders_Then_overlayCardShows3LabelRows', async () => {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () =>
        JSON.stringify({
          rid: 'ri.vertex.main.graph.labels',
          name: 'Labels',
          version: 1,
          payload: payloadWithLabels,
        }),
      json: async () => ({
        rid: 'ri.vertex.main.graph.labels',
        name: 'Labels',
        version: 1,
        payload: payloadWithLabels,
      }),
    });

    renderAt('/vertex/ri.vertex.main.graph.labels');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(1);
    });

    const root = await screen.findByTestId('vertex-node-overlay-root');
    expect(root).toBeInTheDocument();

    const card = await screen.findByTestId(
      'vertex-node-overlay-card-ri.ontology.main.object.airport.JFK',
    );
    expect(card.querySelectorAll('[data-testid^="vertex-extended-label-"]')).toHaveLength(3);
    expect(screen.getByTestId('vertex-extended-label-property').textContent).toContain('On-time %');
    expect(screen.getByTestId('vertex-extended-label-property').textContent).toContain('92');
    expect(screen.getByTestId('vertex-extended-label-timeSeries').textContent).toContain('Temp °C');
    expect(screen.getByTestId('vertex-extended-label-measure').textContent).toContain('avgDelay');
  });
});

describe('VertexWorkspacePage selection interactions (VTX-020)', () => {
  const payload = {
    layers: [
      {
        id: 'layer-airports',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objects: [
          {
            objectRid: 'ri.airport.JFK',
            properties: { name: 'JFK', city: 'New York', onTimePct: 92 },
          },
          {
            objectRid: 'ri.airport.LHR',
            properties: { name: 'LHR', city: 'London' },
          },
        ],
      },
    ],
    edges: [],
    positions: {},
  };

  function mockFetchOk(body: unknown) {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify(body),
      json: async () => body,
    });
  }

  function makeMouseEvent(opts: { shiftKey?: boolean; ctrlKey?: boolean; metaKey?: boolean } = {}): MouseEvent {
    return new MouseEvent('click', {
      bubbles: true,
      cancelable: true,
      shiftKey: opts.shiftKey ?? false,
      ctrlKey: opts.ctrlKey ?? false,
      metaKey: opts.metaKey ?? false,
    });
  }

  it('Given_userSingleClicksNode_When_clickNodeFires_Then_sidebarOpensAndNodeHighlights', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    // Sidebar hidden before any selection.
    expect(screen.queryByTestId('vertex-selection-sidebar')).not.toBeInTheDocument();

    await act(async () => {
      capturedHandlers.clickNode?.({ node: 'ri.airport.JFK', event: { original: makeMouseEvent() } } as unknown);
    });

    const sidebar = await screen.findByTestId('vertex-selection-sidebar');
    expect(sidebar).toBeInTheDocument();
    expect(screen.getByTestId('vertex-selection-sidebar-header').textContent).toContain('JFK');
    // Properties panel shows the property values for the single selected object.
    expect(screen.getByText('city')).toBeInTheDocument();
    expect(screen.getByText('New York')).toBeInTheDocument();

    // Node is highlighted: SelectionHighlighter wrote the attribute.
    expect(loadedGraph!.getNodeAttribute('ri.airport.JFK', 'highlighted')).toBe(true);
    expect(loadedGraph!.getNodeAttribute('ri.airport.LHR', 'highlighted')).toBe(false);
  });

  it('Given_userCtrlClicksTwoNodes_When_clickNodeFiresTwice_Then_batchSidebarShows2Items', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    await act(async () => {
      capturedHandlers.clickNode?.({ node: 'ri.airport.JFK', event: { original: makeMouseEvent() } } as unknown);
    });
    await act(async () => {
      capturedHandlers.clickNode?.({
        node: 'ri.airport.LHR',
        event: { original: makeMouseEvent({ ctrlKey: true }) },
      } as unknown);
    });

    const sidebar = await screen.findByTestId('vertex-selection-sidebar');
    expect(sidebar).toBeInTheDocument();
    expect(screen.getByTestId('vertex-selection-sidebar-batch')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-selection-sidebar-count').textContent).toContain('2');
    expect(loadedGraph!.getNodeAttribute('ri.airport.JFK', 'highlighted')).toBe(true);
    expect(loadedGraph!.getNodeAttribute('ri.airport.LHR', 'highlighted')).toBe(true);
  });

  it('Given_selectionExists_When_userClicksStageWithoutShift_Then_selectionClearsAndSidebarHides', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    await act(async () => {
      capturedHandlers.clickNode?.({ node: 'ri.airport.JFK', event: { original: makeMouseEvent() } } as unknown);
    });
    expect(await screen.findByTestId('vertex-selection-sidebar')).toBeInTheDocument();

    await act(async () => {
      capturedHandlers.clickStage?.({ event: { original: makeMouseEvent() } } as unknown);
    });
    expect(screen.queryByTestId('vertex-selection-sidebar')).not.toBeInTheDocument();
    expect(loadedGraph!.getNodeAttribute('ri.airport.JFK', 'highlighted')).toBe(false);
  });
});

describe('VertexWorkspacePage hierarchical layout (VTX-022)', () => {
  const chainPayload = {
    layers: [
      {
        id: 'layer-chain',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objects: [
          { objectRid: 'ri.airport.A', properties: { name: 'A' } },
          { objectRid: 'ri.airport.B', properties: { name: 'B' } },
          { objectRid: 'ri.airport.C', properties: { name: 'C' } },
        ],
      },
    ],
    edges: [
      { id: 'e1', linkTypeRid: 'ri.lt.flight', source: 'ri.airport.A', target: 'ri.airport.B' },
      { id: 'e2', linkTypeRid: 'ri.lt.flight', source: 'ri.airport.B', target: 'ri.airport.C' },
    ],
    positions: {},
  };

  function mockFetchOk(body: unknown) {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify(body),
      json: async () => body,
    });
  }

  it('Given_userClicksLayoutButton_When_popoverOpens_Then_hierarchicalControlsAreVisible', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: chainPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(3);
    });

    expect(screen.queryByTestId('vertex-layout-popover')).not.toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });

    expect(screen.getByTestId('vertex-layout-popover')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-layout-hierarchical-reverse')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-layout-hierarchical-roots')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-layout-hierarchical-apply')).toBeInTheDocument();
  });

  it('Given_chainPayload_When_userAppliesHierarchical_Then_yIncreasesAlongTheChain', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: chainPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(3);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-hierarchical-apply'));
    });

    await waitFor(() => {
      const ya = loadedGraph!.getNodeAttribute('ri.airport.A', 'y') as number;
      const yb = loadedGraph!.getNodeAttribute('ri.airport.B', 'y') as number;
      const yc = loadedGraph!.getNodeAttribute('ri.airport.C', 'y') as number;
      expect(typeof ya).toBe('number');
      expect(ya).toBeLessThan(yb);
      expect(yb).toBeLessThan(yc);
    });
  });

  it('Given_reverseChecked_When_userAppliesHierarchical_Then_yDecreasesAlongTheChain', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: chainPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(3);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-hierarchical-reverse'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-hierarchical-apply'));
    });

    await waitFor(() => {
      const ya = loadedGraph!.getNodeAttribute('ri.airport.A', 'y') as number;
      const yb = loadedGraph!.getNodeAttribute('ri.airport.B', 'y') as number;
      const yc = loadedGraph!.getNodeAttribute('ri.airport.C', 'y') as number;
      expect(typeof ya).toBe('number');
      expect(ya).toBeGreaterThan(yb);
      expect(yb).toBeGreaterThan(yc);
    });
  });

  it('Given_userSpecifiesRootNode_When_userAppliesHierarchical_Then_rootIsPlacedInTopTier', async () => {
    // Branch graph A→B, A→C. Default puts A at top; we force C as root.
    const branchPayload = {
      layers: [
        {
          id: 'layer-branch',
          objectTypeRid: 'ri.ontology.main.object-type.airport',
          objects: [
            { objectRid: 'ri.airport.A', properties: { name: 'A' } },
            { objectRid: 'ri.airport.B', properties: { name: 'B' } },
            { objectRid: 'ri.airport.C', properties: { name: 'C' } },
          ],
        },
      ],
      edges: [
        { id: 'e1', linkTypeRid: 'ri.lt', source: 'ri.airport.A', target: 'ri.airport.B' },
        { id: 'e2', linkTypeRid: 'ri.lt', source: 'ri.airport.A', target: 'ri.airport.C' },
      ],
      positions: {},
    };
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: branchPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(3);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    await act(async () => {
      fireEvent.change(screen.getByTestId('vertex-layout-hierarchical-roots'), {
        target: { value: 'ri.airport.C' },
      });
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-hierarchical-apply'));
    });

    await waitFor(() => {
      const ya = loadedGraph!.getNodeAttribute('ri.airport.A', 'y') as number;
      const yc = loadedGraph!.getNodeAttribute('ri.airport.C', 'y') as number;
      expect(typeof yc).toBe('number');
      expect(yc).toBeLessThanOrEqual(ya);
    });
  });

  it('Given_popoverOpen_When_userClicksApply_Then_popoverCloses', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: chainPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(3);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    expect(screen.getByTestId('vertex-layout-popover')).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-hierarchical-apply'));
    });
    await waitFor(() => {
      expect(screen.queryByTestId('vertex-layout-popover')).not.toBeInTheDocument();
    });
  });
});

describe('VertexWorkspacePage force / circular / auto layouts (VTX-023)', () => {
  const trianglePayload = {
    layers: [
      {
        id: 'layer-triangle',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objects: [
          { objectRid: 'ri.airport.A', properties: { name: 'A' } },
          { objectRid: 'ri.airport.B', properties: { name: 'B' } },
          { objectRid: 'ri.airport.C', properties: { name: 'C' } },
          { objectRid: 'ri.airport.D', properties: { name: 'D' } },
        ],
      },
    ],
    edges: [
      { id: 'e1', linkTypeRid: 'ri.lt', source: 'ri.airport.A', target: 'ri.airport.B' },
      { id: 'e2', linkTypeRid: 'ri.lt', source: 'ri.airport.B', target: 'ri.airport.C' },
      { id: 'e3', linkTypeRid: 'ri.lt', source: 'ri.airport.C', target: 'ri.airport.A' },
    ],
    positions: {},
  };

  function mockFetchOk(body: unknown) {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify(body),
      json: async () => body,
    });
  }

  function snapshotPositions(): Map<string, { x: number; y: number }> {
    const map = new Map<string, { x: number; y: number }>();
    if (!loadedGraph) return map;
    loadedGraph.forEachNode((id: string) => {
      const x = loadedGraph!.getNodeAttribute(id, 'x') as number;
      const y = loadedGraph!.getNodeAttribute(id, 'y') as number;
      map.set(id, { x, y });
    });
    return map;
  }

  it('Given_userPicksForceLayout_When_Applies_Then_nodePositionsMutate', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: trianglePayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(4);
    });

    const before = snapshotPositions();

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-kind-force'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-force-apply'));
    });

    await waitFor(() => {
      const after = snapshotPositions();
      // ForceAtlas2 should leave all nodes with finite positions and
      // perturb at least one of them away from the seed layout.
      for (const [id, p] of after) {
        expect(Number.isFinite(p.x)).toBe(true);
        expect(Number.isFinite(p.y)).toBe(true);
        // Sanity: position is non-degenerate (the algorithm pushes nodes apart).
        const seed = before.get(id);
        expect(seed).toBeDefined();
      }
      const moved = [...after].some(([id, p]) => {
        const seed = before.get(id)!;
        return Math.abs(p.x - seed.x) > 1e-6 || Math.abs(p.y - seed.y) > 1e-6;
      });
      expect(moved).toBe(true);
    });
  });

  it('Given_userPicksCircularLayout_When_Applies_Then_allNodesShareSameRadius', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: trianglePayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(4);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-kind-circular'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-circular-apply'));
    });

    await waitFor(() => {
      const after = snapshotPositions();
      const radii = [...after.values()].map((p) => Math.hypot(p.x, p.y));
      // All four nodes should land on the same circle centred at origin
      // (within fp tolerance).
      const min = Math.min(...radii);
      const max = Math.max(...radii);
      expect(max - min).toBeLessThan(1e-6);
      // Radius is the layout's default (DEFAULT_RADIUS = 200) — positive.
      expect(min).toBeGreaterThan(0);
    });
  });

  it('Given_userPicksAutoLayoutAndGraphHas4Nodes_When_Applies_Then_dispatchesForceBehaviour', async () => {
    // < 100 nodes → auto = force. ForceAtlas2's signature here is that
    // every node moves at least slightly from its seed circle position.
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: trianglePayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(4);
    });

    const before = snapshotPositions();

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-kind-auto'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-auto-apply'));
    });

    await waitFor(() => {
      const after = snapshotPositions();
      // All radii should NOT collapse to a single value — force layout
      // produces variation that circular wouldn't.
      const radii = [...after.values()].map((p) => Math.hypot(p.x, p.y));
      const min = Math.min(...radii);
      const max = Math.max(...radii);
      // At least one node moved relative to baseline OR the radii spread
      // meaningfully (>1 unit) — both signal a force pass ran rather than
      // a pure circular placement.
      const moved = [...after].some(([id, p]) => {
        const seed = before.get(id)!;
        return Math.abs(p.x - seed.x) > 1e-6 || Math.abs(p.y - seed.y) > 1e-6;
      });
      expect(moved || max - min > 1).toBe(true);
    });
  });

  it('Given_userPicksAutoLayoutAndGraphHas150Nodes_When_Applies_Then_dispatchesHierarchicalBehaviour', async () => {
    // ≥ 100 nodes → auto = hierarchical. Verify by building a long chain
    // and asserting y monotonically increases along the chain (the
    // hierarchical-TB signature).
    const N = 150;
    const longChainPayload = {
      layers: [
        {
          id: 'layer-chain',
          objectTypeRid: 'ri.ontology.main.object-type.airport',
          objects: Array.from({ length: N }, (_, i) => ({
            objectRid: `ri.airport.${i}`,
            properties: { name: `A${i}` },
          })),
        },
      ],
      edges: Array.from({ length: N - 1 }, (_, i) => ({
        id: `e${i}`,
        linkTypeRid: 'ri.lt',
        source: `ri.airport.${i}`,
        target: `ri.airport.${i + 1}`,
      })),
      positions: {},
    };
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: longChainPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(N);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-kind-auto'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-auto-apply'));
    });

    await waitFor(() => {
      const y0 = loadedGraph!.getNodeAttribute('ri.airport.0', 'y') as number;
      const yLast = loadedGraph!.getNodeAttribute(`ri.airport.${N - 1}`, 'y') as number;
      expect(typeof y0).toBe('number');
      expect(typeof yLast).toBe('number');
      // Hierarchical TB: root smallest y, leaf largest.
      expect(y0).toBeLessThan(yLast);
    });
  });

  it('Given_popoverOpen_When_kindIsForce_Then_hierarchicalControlsAreHidden', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: trianglePayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    expect(screen.getByTestId('vertex-layout-hierarchical-controls')).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-kind-force'));
    });
    expect(
      screen.queryByTestId('vertex-layout-hierarchical-controls'),
    ).not.toBeInTheDocument();
  });
});

describe('VertexWorkspacePage manual drag + pinned (VTX-024)', () => {
  const dragPayload = {
    layers: [
      {
        id: 'layer-airports',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objects: [
          { objectRid: 'ri.airport.A', properties: { name: 'A' } },
          { objectRid: 'ri.airport.B', properties: { name: 'B' } },
          { objectRid: 'ri.airport.C', properties: { name: 'C' } },
        ],
      },
    ],
    edges: [
      { id: 'e1', linkTypeRid: 'ri.lt', source: 'ri.airport.A', target: 'ri.airport.B' },
      { id: 'e2', linkTypeRid: 'ri.lt', source: 'ri.airport.B', target: 'ri.airport.C' },
    ],
    positions: {},
  };

  const dragPayloadPinnedA = {
    ...dragPayload,
    positions: {
      'ri.airport.A': { x: 42, y: -17, pinned: true },
    },
  };

  function mockFetchOk(body: unknown) {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify(body),
      json: async () => body,
    });
  }

  function mockPatchOk() {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify({ rid: 'ri.g' }),
      json: async () => ({ rid: 'ri.g' }),
    });
  }

  function fireSigmaEvent(name: string, payload: unknown) {
    const handler = capturedHandlers[name];
    if (!handler) throw new Error(`no handler registered for ${name}`);
    handler(payload);
  }

  it('Given_userDragsNodeA_When_mouseup_Then_PATCHesLayoutWithPinnedTrueAndUpdatesCoords', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: dragPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(3);
    });

    mockPatchOk();

    // Simulate downNode → mousemovebody → mouseup. The captured handlers
    // run inside React render flow, so wrap each fire in act().
    await act(async () => {
      fireSigmaEvent('downNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('mousedown') },
      });
    });
    await act(async () => {
      const mv = new MouseEvent('mousemove', { clientX: 250, clientY: 150 });
      fireSigmaEvent('mousemovebody', { x: 250, y: 150, original: mv });
    });
    await act(async () => {
      const up = new MouseEvent('mouseup', { clientX: 250, clientY: 150 });
      fireSigmaEvent('mouseup', { x: 250, y: 150, original: up });
    });

    // Expect a PATCH /api/vertex/v1/graphs/{rid}/layout call with the new
    // coords + pinned:true.
    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>;
    const patchCall = fetchMock.mock.calls.find(
      (c: unknown[]) => typeof c[0] === 'string' && (c[0] as string).endsWith('/layout'),
    );
    expect(patchCall).toBeDefined();
    expect(patchCall![0]).toBe('/api/vertex/v1/graphs/ri.g/layout');
    const init = patchCall![1] as RequestInit;
    expect(init.method).toBe('PATCH');
    const body = JSON.parse(init.body as string);
    expect(body.positions['ri.airport.A']).toEqual({ x: 250, y: 150, pinned: true });
  });

  it('Given_pinnedNodeA_When_userAppliesLayout_Then_nodeAStaysAtPinnedCoords', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: dragPayloadPinnedA });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(3);
    });

    // Apply the default hierarchical layout.
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-layout'));
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-layout-hierarchical-apply'));
    });

    await waitFor(() => {
      const xA = loadedGraph!.getNodeAttribute('ri.airport.A', 'x') as number;
      const yA = loadedGraph!.getNodeAttribute('ri.airport.A', 'y') as number;
      expect(xA).toBe(42);
      expect(yA).toBe(-17);
    });
    // B and C should still be present (and finite — moved by the layout).
    expect(Number.isFinite(loadedGraph!.getNodeAttribute('ri.airport.B', 'x') as number)).toBe(true);
    expect(Number.isFinite(loadedGraph!.getNodeAttribute('ri.airport.C', 'x') as number)).toBe(true);
  });

  it('Given_pinnedNodeA_When_rightClickAndUnpin_Then_PATCHesPinnedFalseAndRemovesFromPinSet', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: dragPayloadPinnedA });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(3);
    });

    // Right-click on the pinned node.
    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu', { clientX: 120, clientY: 90 }) },
      });
    });

    const menu = await screen.findByTestId('vertex-node-context-menu');
    expect(menu.getAttribute('data-node')).toBe('ri.airport.A');
    const unpinButton = screen.getByTestId('vertex-node-context-menu-unpin');

    mockPatchOk();
    await act(async () => {
      fireEvent.click(unpinButton);
    });

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>;
    const patchCall = fetchMock.mock.calls.find(
      (c: unknown[]) => typeof c[0] === 'string' && (c[0] as string).endsWith('/layout'),
    );
    expect(patchCall).toBeDefined();
    const body = JSON.parse((patchCall![1] as RequestInit).body as string);
    expect(body.positions['ri.airport.A'].pinned).toBe(false);

    // Menu closes after unpin.
    expect(screen.queryByTestId('vertex-node-context-menu')).not.toBeInTheDocument();
  });

  it('Given_unpinnedNode_When_rightClick_Then_menuRendersWithPinInsteadOfUnpin', async () => {
    // VTX-024 originally rendered NO menu for a non-pinned node. VTX-026 lands
    // the multi-item menu; on an unpinned node the toggle item reads "Pin".
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: dragPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
    });

    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu') },
      });
    });

    const menu = await screen.findByTestId('vertex-node-context-menu');
    expect(menu.getAttribute('data-node')).toBe('ri.airport.A');
    expect(screen.getByTestId('vertex-node-context-menu-pin')).toBeInTheDocument();
    expect(screen.queryByTestId('vertex-node-context-menu-unpin')).not.toBeInTheDocument();
  });
});

describe('VertexWorkspacePage link merging (VTX-025)', () => {
  // Payload with 5 parallel edges A→B sharing the same LinkType RID — the
  // exact "Given A→B 有 5 条同 LinkType 边" scenario the BDD asks for.
  const fiveParallelPayload = {
    layers: [
      {
        id: 'layer-airports',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objects: [
          { objectRid: 'ri.airport.A', properties: { name: 'A' } },
          { objectRid: 'ri.airport.B', properties: { name: 'B' } },
        ],
      },
    ],
    edges: Array.from({ length: 5 }, (_, i) => ({
      id: `e${i}`,
      linkTypeRid: 'ri.lt.flight',
      source: 'ri.airport.A',
      target: 'ri.airport.B',
    })),
    positions: {},
  };

  function mockFetchOk(body: unknown) {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify(body),
      json: async () => body,
    });
  }

  it('Given_mergeFalseByDefault_When_pageRenders_Then_fiveParallelEdgesShowOnGraphAsFiveDistinctEdges', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: fiveParallelPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });
    // Default state: merge off — each of the 5 parallel edges renders
    // as its own graphology edge.
    expect(loadedGraph!.size).toBe(5);
    // No edge carries a ×N label.
    for (const key of loadedGraph!.edges()) {
      expect(loadedGraph!.getEdgeAttribute(key, 'label')).toBeUndefined();
    }
  });

  it('Given_mergeToggleEnabled_When_pageRendersAgain_Then_fiveEdgesCollapseToOneEdgeWithTimes5Label', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: fiveParallelPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    // Toggle merging on via the TopBar control.
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-merge-toggle'));
    });

    await waitFor(() => {
      // 5 → 1 merged edge.
      expect(loadedGraph!.size).toBe(1);
    });
    const mergedKey = loadedGraph!.edges()[0];
    expect(loadedGraph!.getEdgeAttribute(mergedKey, 'label')).toBe('×5');
    expect(loadedGraph!.getEdgeAttribute(mergedKey, 'count')).toBe(5);
    // Thicker than the default single-edge size of 1.
    const mergedSize = loadedGraph!.getEdgeAttribute(mergedKey, 'size') as number;
    expect(typeof mergedSize).toBe('number');
    expect(mergedSize).toBeGreaterThan(1);
  });

  it('Given_mergeToggleEnabledThenToggledOff_When_pageRendersAgain_Then_returnsToFiveParallelEdges', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: fiveParallelPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    // ON → 1 edge
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-merge-toggle'));
    });
    await waitFor(() => {
      expect(loadedGraph!.size).toBe(1);
    });

    // OFF again → back to 5 edges, no ×N labels.
    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-merge-toggle'));
    });
    await waitFor(() => {
      expect(loadedGraph!.size).toBe(5);
    });
    for (const key of loadedGraph!.edges()) {
      expect(loadedGraph!.getEdgeAttribute(key, 'label')).toBeUndefined();
    }
  });
});

describe('VertexWorkspacePage node context menu (VTX-026)', () => {
  // Two-node payload with an ontology api name on the layer so the
  // Open-in-Object-Explorer item builds a /explorer/{ontology} URL.
  const ctxPayload = {
    layers: [
      {
        id: 'layer-airports',
        ontology: 'main',
        objectType: 'Airport',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objects: [
          { objectRid: 'ri.airport.A', properties: { name: 'A' } },
          { objectRid: 'ri.airport.B', properties: { name: 'B' } },
        ],
      },
    ],
    edges: [
      { id: 'e1', linkTypeRid: 'ri.lt.flight', source: 'ri.airport.A', target: 'ri.airport.B' },
    ],
    positions: {},
  };

  const ctxPayloadPinnedA = {
    ...ctxPayload,
    positions: {
      'ri.airport.A': { x: 50, y: 60, pinned: true },
    },
  };

  function mockFetchOk(body: unknown) {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify(body),
      json: async () => body,
    });
  }

  function mockPatchOk() {
    (globalThis.fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify({ rid: 'ri.g' }),
      json: async () => ({ rid: 'ri.g' }),
    });
  }

  function fireSigmaEvent(name: string, payload: unknown) {
    const handler = capturedHandlers[name];
    if (!handler) throw new Error(`no handler registered for ${name}`);
    handler(payload);
  }

  it('Given_userRightClicksNode_When_menuOpens_Then_atLeast5MenuItemsAreShown', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: ctxPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu', { clientX: 10, clientY: 20 }) },
      });
    });

    await screen.findByTestId('vertex-node-context-menu');
    expect(screen.getByTestId('vertex-node-context-menu-search-around')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-node-context-menu-open-explorer')).toBeInTheDocument();
    // unpinned by default → toggle reads "Pin"
    expect(screen.getByTestId('vertex-node-context-menu-pin')).toBeInTheDocument();
    expect(screen.queryByTestId('vertex-node-context-menu-unpin')).not.toBeInTheDocument();
    expect(screen.getByTestId('vertex-node-context-menu-hide')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-node-context-menu-copy-rid')).toBeInTheDocument();
  });

  it('Given_rightClickPinnedNode_When_menuOpens_Then_toggleItemReadsUnpin', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: ctxPayloadPinnedA });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu') },
      });
    });

    await screen.findByTestId('vertex-node-context-menu');
    expect(screen.getByTestId('vertex-node-context-menu-unpin')).toBeInTheDocument();
    expect(screen.queryByTestId('vertex-node-context-menu-pin')).not.toBeInTheDocument();
  });

  it('Given_userClicksOpenInObjectExplorer_When_clicked_Then_windowOpenIsCalledWithExplorerUrl', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: ctxPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);

    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu') },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-node-context-menu-open-explorer'));
    });

    expect(openSpy).toHaveBeenCalledTimes(1);
    const [url, target] = openSpy.mock.calls[0];
    expect(url).toBe('/explorer/main?objectRid=ri.airport.A');
    expect(target).toBe('_blank');

    // Menu closes after clicking the item.
    expect(screen.queryByTestId('vertex-node-context-menu')).not.toBeInTheDocument();

    openSpy.mockRestore();
  });

  it('Given_userClicksHide_When_clicked_Then_nodeAndIncidentEdgeDisappearFromGraph', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: ctxPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
      expect(loadedGraph!.size).toBe(1);
    });

    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu') },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-node-context-menu-hide'));
    });

    await waitFor(() => {
      expect(loadedGraph!.hasNode('ri.airport.A')).toBe(false);
      // The single edge A→B is incident to the hidden node and is dropped.
      expect(loadedGraph!.size).toBe(0);
    });
    // B remains.
    expect(loadedGraph!.hasNode('ri.airport.B')).toBe(true);
  });

  it('Given_userClicksCopyRid_When_clicked_Then_clipboardWriteTextCalledWithRid', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: ctxPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    const writeText = vi.fn().mockResolvedValue(undefined);
    const origClipboard = (
      navigator as unknown as { clipboard?: { writeText: (s: string) => Promise<void> } }
    ).clipboard;
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });

    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu') },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-node-context-menu-copy-rid'));
    });

    expect(writeText).toHaveBeenCalledTimes(1);
    expect(writeText).toHaveBeenCalledWith('ri.airport.A');

    // Restore.
    Object.defineProperty(navigator, 'clipboard', {
      value: origClipboard,
      configurable: true,
    });
  });

  it('Given_userClicksPinOnUnpinnedNode_When_clicked_Then_PATCHesLayoutWithPinnedTrue', async () => {
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: ctxPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    mockPatchOk();

    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu') },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-node-context-menu-pin'));
    });

    const fetchMock = globalThis.fetch as unknown as ReturnType<typeof vi.fn>;
    const patchCall = fetchMock.mock.calls.find(
      (c: unknown[]) => typeof c[0] === 'string' && (c[0] as string).endsWith('/layout'),
    );
    expect(patchCall).toBeDefined();
    expect(patchCall![0]).toBe('/api/vertex/v1/graphs/ri.g/layout');
    expect((patchCall![1] as RequestInit).method).toBe('PATCH');
    const body = JSON.parse((patchCall![1] as RequestInit).body as string);
    expect(body.positions['ri.airport.A'].pinned).toBe(true);

    // Menu closes after pin.
    expect(screen.queryByTestId('vertex-node-context-menu')).not.toBeInTheDocument();
  });

  it('Given_userClicksSearchAround_When_clicked_Then_menuCloses', async () => {
    // VTX-026 only requires the item exists. The dialog wiring lands in
    // VTX-069. Verify the menu dismisses on click so callers can chain.
    mockFetchOk({ rid: 'ri.g', name: 'g', version: 1, payload: ctxPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    await act(async () => {
      fireSigmaEvent('rightClickNode', {
        node: 'ri.airport.A',
        event: { original: new MouseEvent('contextmenu') },
      });
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-node-context-menu-search-around'));
    });

    expect(screen.queryByTestId('vertex-node-context-menu')).not.toBeInTheDocument();
  });
});

describe('VertexWorkspacePage add objects dialog (VTX-027)', () => {
  // Two-airport payload: layer carries `ontology: 'main'` so the dialog
  // can derive the ontology context without an explicit prop.
  const seedPayload = {
    layers: [
      {
        id: 'layer-airports',
        ontology: 'main',
        objectType: 'Airport',
        objectTypeRid: 'ri.ontology.main.object-type.airport',
        objects: [
          { objectRid: 'ri.airport.JFK', properties: { name: 'JFK' } },
          { objectRid: 'ri.airport.LHR', properties: { name: 'LHR' } },
        ],
      },
    ],
    edges: [],
    positions: {},
  };

  // VTX-027 page tests need to mock multiple endpoints (the graph load
  // plus OSS objectTypes + search). Replace fetch with a URL-routed
  // implementation for these tests so each request resolves to the
  // appropriate body shape.
  function setupRoutedFetch(graphPayload: unknown) {
    const objectGets: string[] = [];
    const fiveAirports = Array.from({ length: 5 }, (_, i) => ({
      __rid: `ri.airport.NEW${i}`,
      __primaryKey: `NEW${i}`,
      __apiName: 'Airport',
      name: `New Airport ${i}`,
    }));
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/api/vertex/v1/graphs/')) {
        return {
          ok: true,
          status: 200,
          statusText: 'OK',
          text: async () => JSON.stringify(graphPayload),
          json: async () => graphPayload,
        } as Response;
      }
      if (url.includes('/api/v2/ontologies/main/objectTypes')) {
        const body = {
          data: [
            {
              rid: 'ri.ontology.main.object-type.airport',
              apiName: 'Airport',
              displayName: 'Airport',
              primaryKey: 'icao',
              titleProperty: 'name',
              status: 'ACTIVE',
              visibility: 'NORMAL',
            },
          ],
        };
        return {
          ok: true,
          status: 200,
          statusText: 'OK',
          text: async () => JSON.stringify(body),
          json: async () => body,
        } as Response;
      }
      if (url.includes('/Airport/search')) {
        const body = { data: fiveAirports };
        return {
          ok: true,
          status: 200,
          statusText: 'OK',
          text: async () => JSON.stringify(body),
          json: async () => body,
        } as Response;
      }
      if (url.includes('/api/v2/ontologies/main/objects/Airport/')) {
        objectGets.push(url);
        const pk = decodeURIComponent(url.split('/').pop() ?? '');
        const body = {
          __rid: `ri.airport.${pk}`,
          __primaryKey: pk,
          __apiName: 'Airport',
          name: `Fresh ${pk}`,
          status: 'active',
        };
        return {
          ok: true,
          status: 200,
          statusText: 'OK',
          text: async () => JSON.stringify(body),
          json: async () => body,
        } as Response;
      }
      throw new Error(`unmocked fetch: ${url}`);
    }) as unknown as typeof fetch;
    return { objectGets };
  }

  it('Given_topBarHasAddObjectsButton_When_clicked_Then_dialogOpens', async () => {
    setupRoutedFetch({ rid: 'ri.g', name: 'g', version: 1, payload: seedPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    expect(screen.queryByTestId('vertex-add-objects-dialog')).not.toBeInTheDocument();
    const button = screen.getByTestId('vertex-topbar-add-objects');
    expect(button).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(button);
    });

    expect(await screen.findByTestId('vertex-add-objects-dialog')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-add-objects-type')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-add-objects-search')).toBeInTheDocument();
  });

  it('Given_userPicksFiveAndClicksAdd_When_added_Then_fiveNewNodesAppearOnTheGraph', async () => {
    setupRoutedFetch({ rid: 'ri.g', name: 'g', version: 1, payload: seedPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-add-objects'));
    });
    await screen.findByTestId('vertex-add-objects-dialog');

    // Wait for the dialog to default the type and trigger the search.
    await waitFor(() => {
      expect((screen.getByTestId('vertex-add-objects-type') as HTMLSelectElement).value).toBe('Airport');
    });

    await act(async () => {
      fireEvent.change(screen.getByTestId('vertex-add-objects-search'), {
        target: { value: 'New' },
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('vertex-add-objects-row-ri.airport.NEW0')).toBeInTheDocument();
    });

    for (let i = 0; i < 5; i++) {
      await act(async () => {
        fireEvent.click(screen.getByTestId(`vertex-add-objects-row-ri.airport.NEW${i}`));
      });
    }

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-add-objects-add'));
    });

    await waitFor(() => {
      // Original 2 + 5 new = 7 nodes.
      expect(loadedGraph!.order).toBe(7);
      for (let i = 0; i < 5; i++) {
        expect(loadedGraph!.hasNode(`ri.airport.NEW${i}`)).toBe(true);
      }
    });
    // Dialog dismissed itself on Add.
    expect(screen.queryByTestId('vertex-add-objects-dialog')).not.toBeInTheDocument();
  });

  it('Given_userSelectsAddedNode_When_sidebarOpens_Then_fetchesFreshObjectByPrimaryKey', async () => {
    const calls = setupRoutedFetch({ rid: 'ri.g', name: 'g', version: 1, payload: seedPayload });
    renderAt('/vertex/ri.g');

    await waitFor(() => {
      expect(loadedGraph).not.toBeNull();
      expect(loadedGraph!.order).toBe(2);
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-topbar-add-objects'));
    });
    await screen.findByTestId('vertex-add-objects-dialog');

    await waitFor(() => {
      expect((screen.getByTestId('vertex-add-objects-type') as HTMLSelectElement).value).toBe('Airport');
    });

    await act(async () => {
      fireEvent.change(screen.getByTestId('vertex-add-objects-search'), {
        target: { value: 'New' },
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('vertex-add-objects-row-ri.airport.NEW0')).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('vertex-add-objects-row-ri.airport.NEW0'));
      fireEvent.click(screen.getByTestId('vertex-add-objects-add'));
    });

    await waitFor(() => {
      expect(loadedGraph!.hasNode('ri.airport.NEW0')).toBe(true);
    });

    await act(async () => {
      capturedHandlers.clickNode?.({
        node: 'ri.airport.NEW0',
        event: { original: new MouseEvent('click', { bubbles: true }) },
      } as unknown);
    });

    await waitFor(() => {
      expect(calls.objectGets).toContain('/api/v2/ontologies/main/objects/Airport/NEW0');
    });
    expect(await screen.findByText('Fresh NEW0')).toBeInTheDocument();
  });
});
