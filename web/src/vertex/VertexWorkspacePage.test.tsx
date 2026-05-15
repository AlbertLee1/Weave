import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import Graph from 'graphology';

// jsdom can't WebGL, so the SigmaContainer render is replaced with a
// stable placeholder div the tests can assert against. useLoadGraph is
// captured into the module-scoped `loadedGraph` ref so VTX-018 assertions
// can introspect the loaded node/edge sets without booting Sigma.
let loadedGraph: Graph | null = null;
const captureLoad = (g: Graph) => {
  loadedGraph = g;
};

vi.mock('@react-sigma/core', () => ({
  SigmaContainer: ({ children, style }: { children?: React.ReactNode; style?: React.CSSProperties }) => (
    <div data-testid="vertex-canvas-mock" style={style}>
      {children}
    </div>
  ),
  useLoadGraph: () => captureLoad,
}));

import { VertexWorkspacePage } from './VertexWorkspacePage';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/vertex/:rid" element={<VertexWorkspacePage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const realFetch = globalThis.fetch;

beforeEach(() => {
  loadedGraph = null;
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
    expect(screen.getByTestId('vertex-topbar-graph-name').textContent).toContain('Alpha');
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
