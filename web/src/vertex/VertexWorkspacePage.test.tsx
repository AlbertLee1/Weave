import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';

// jsdom can't WebGL, so the SigmaContainer render is replaced with a
// stable placeholder div the tests can assert against.
vi.mock('@react-sigma/core', () => ({
  SigmaContainer: ({ children, style }: { children?: React.ReactNode; style?: React.CSSProperties }) => (
    <div data-testid="vertex-canvas-mock" style={style}>
      {children}
    </div>
  ),
  useLoadGraph: () => () => undefined,
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
