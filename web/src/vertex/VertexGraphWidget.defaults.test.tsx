import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { VertexGraphWidget } from './VertexGraphWidget';
import '../i18n';

// VTX-122 — cover the default fetch-based loader/saver paths that the
// happy-path suite (VertexGraphWidget.test.tsx) bypasses via injected
// loader/saver. Mocks the global fetch so we exercise the production
// network code without spinning up a backend.

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

describe('VertexGraphWidget — default fetch loader/saver', () => {
  it('default loader GETs /api/vertex/v1/graphs/{rid}', async () => {
    const fetchMock = vi.fn(async () =>
      ({
        ok: true,
        status: 200,
        json: async () => ({ cameraZoom: 2 }),
      } as Response),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    render(<VertexGraphWidget graphRid="ri.vertex.main.graph.alpha" />);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/vertex/v1/graphs/ri.vertex.main.graph.alpha',
      );
    });
    await waitFor(() => screen.getByTestId('vertex-widget-canvas'));
  });

  it('default loader surfaces fetch failure as inline error', async () => {
    const fetchMock = vi.fn(async () =>
      ({ ok: false, status: 503, json: async () => ({}) } as Response),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    render(<VertexGraphWidget graphRid="ri.vertex.main.graph.alpha" />);
    await waitFor(() => {
      expect(screen.getByTestId('vertex-widget-error').textContent).toContain('503');
    });
  });

  it('default saver PATCHes /api/vertex/v1/graphs/{overrideRid} on Save click', async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      calls.push([url, init]);
      if (init?.method === 'PATCH') {
        return { ok: true, status: 200, json: async () => ({}) } as Response;
      }
      return { ok: true, status: 200, json: async () => ({ cameraZoom: 1 }) } as Response;
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        overrideGraphRid="ri.vertex.main.graph.alpha-override"
      />,
    );
    const btn = await screen.findByTestId('vertex-widget-save');
    await waitFor(() => expect((btn as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(btn);

    await waitFor(() => {
      expect(calls.some(([u, i]) => u.includes('alpha-override') && i?.method === 'PATCH')).toBe(true);
    });
  });

  it('default saver surfaces fetch failure as inline error', async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === 'PATCH') {
        return { ok: false, status: 500, json: async () => ({}) } as Response;
      }
      return { ok: true, status: 200, json: async () => ({ cameraZoom: 1 }) } as Response;
    });
    globalThis.fetch = fetchMock as unknown as typeof fetch;

    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        overrideGraphRid="ri.vertex.main.graph.alpha-override"
      />,
    );
    const btn = await screen.findByTestId('vertex-widget-save');
    await waitFor(() => expect((btn as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(btn);

    await waitFor(() => {
      expect(screen.getByTestId('vertex-widget-error').textContent).toContain('500');
    });
  });
});
