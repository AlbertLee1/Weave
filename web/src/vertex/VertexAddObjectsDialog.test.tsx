import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { VertexAddObjectsDialog } from './VertexAddObjectsDialog';

// Lightweight URL-routed fetch mock — each integration tests configures
// what /api/v2/ontologies/.../objectTypes and .../search return.
type Handler = {
  match: (url: string, init?: RequestInit) => boolean;
  body: unknown;
  status?: number;
};

let handlers: Handler[] = [];

function setupFetch() {
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    for (const h of handlers) {
      if (h.match(url, init)) {
        const status = h.status ?? 200;
        return {
          ok: status >= 200 && status < 300,
          status,
          statusText: 'OK',
          headers: new Headers({ 'content-type': 'application/json' }),
          text: async () => JSON.stringify(h.body),
          json: async () => h.body,
        } as Response;
      }
    }
    throw new Error(`unmocked fetch: ${url}`);
  }) as unknown as typeof fetch;
}

const realFetch = globalThis.fetch;

beforeEach(() => {
  handlers = [];
  setupFetch();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.clearAllMocks();
});

function withQueryClient(node: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return <QueryClientProvider client={qc}>{node}</QueryClientProvider>;
}

const objectTypesBody = {
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

describe('VertexAddObjectsDialog (VTX-027)', () => {
  it('Given_open_When_mounted_Then_rendersDialogWithObjectTypeDropdownAndSearchBox', async () => {
    handlers = [
      { match: (u) => u.includes('/objectTypes'), body: objectTypesBody },
    ];
    render(
      withQueryClient(
        <VertexAddObjectsDialog
          open
          ontologyApiName="main"
          onClose={() => {}}
          onAdd={() => {}}
        />,
      ),
    );

    expect(await screen.findByTestId('vertex-add-objects-dialog')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-add-objects-type')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-add-objects-search')).toBeInTheDocument();
    // ObjectType dropdown is populated from useObjectTypes.
    await waitFor(() => {
      const opts = screen.getAllByTestId(/^vertex-add-objects-type-option-/);
      expect(opts.length).toBeGreaterThan(0);
    });
  });

  it('Given_userTypesJfk_When_searchFires_Then_resultRowsRender', async () => {
    handlers = [
      { match: (u) => u.includes('/objectTypes'), body: objectTypesBody },
      {
        match: (u) => u.includes('/Airport/search'),
        body: {
          data: [
            { __rid: 'ri.airport.JFK', __primaryKey: 'JFK', __apiName: 'Airport', name: 'JFK Intl' },
            { __rid: 'ri.airport.JFK2', __primaryKey: 'JFK2', __apiName: 'Airport', name: 'JFK Regional' },
          ],
        },
      },
    ];

    render(
      withQueryClient(
        <VertexAddObjectsDialog
          open
          ontologyApiName="main"
          onClose={() => {}}
          onAdd={() => {}}
        />,
      ),
    );

    await screen.findByTestId('vertex-add-objects-dialog');
    // Wait for the default object type to be selected by the dialog.
    await waitFor(() => {
      expect((screen.getByTestId('vertex-add-objects-type') as HTMLSelectElement).value).toBe('Airport');
    });

    await act(async () => {
      fireEvent.change(screen.getByTestId('vertex-add-objects-search'), {
        target: { value: 'JFK' },
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('vertex-add-objects-row-ri.airport.JFK')).toBeInTheDocument();
      expect(screen.getByTestId('vertex-add-objects-row-ri.airport.JFK2')).toBeInTheDocument();
    });
  });

  it('Given_fiveResultsChecked_When_userClicksAdd_Then_onAddCalledWithFiveObjects', async () => {
    const fiveAirports = Array.from({ length: 5 }, (_, i) => ({
      __rid: `ri.airport.A${i}`,
      __primaryKey: `A${i}`,
      __apiName: 'Airport',
      name: `Airport ${i}`,
    }));
    handlers = [
      { match: (u) => u.includes('/objectTypes'), body: objectTypesBody },
      { match: (u) => u.includes('/Airport/search'), body: { data: fiveAirports } },
    ];

    const onAdd = vi.fn();
    const onClose = vi.fn();
    render(
      withQueryClient(
        <VertexAddObjectsDialog
          open
          ontologyApiName="main"
          onClose={onClose}
          onAdd={onAdd}
        />,
      ),
    );

    await screen.findByTestId('vertex-add-objects-dialog');
    await waitFor(() => {
      expect((screen.getByTestId('vertex-add-objects-type') as HTMLSelectElement).value).toBe('Airport');
    });

    await act(async () => {
      fireEvent.change(screen.getByTestId('vertex-add-objects-search'), {
        target: { value: 'Airport' },
      });
    });

    // Wait for results to render, then check all 5.
    await waitFor(() => {
      expect(screen.getByTestId('vertex-add-objects-row-ri.airport.A0')).toBeInTheDocument();
    });
    for (let i = 0; i < 5; i++) {
      await act(async () => {
        fireEvent.click(screen.getByTestId(`vertex-add-objects-row-ri.airport.A${i}`));
      });
    }

    // Add button is now enabled.
    const addBtn = screen.getByTestId('vertex-add-objects-add') as HTMLButtonElement;
    expect(addBtn.disabled).toBe(false);

    await act(async () => {
      fireEvent.click(addBtn);
    });

    expect(onAdd).toHaveBeenCalledTimes(1);
    const arg = onAdd.mock.calls[0][0] as Array<{ rid: string; label: string; primaryKey?: string }>;
    expect(arg).toHaveLength(5);
    expect(arg.map((o) => o.rid).sort()).toEqual([
      'ri.airport.A0',
      'ri.airport.A1',
      'ri.airport.A2',
      'ri.airport.A3',
      'ri.airport.A4',
    ]);
    expect(arg.map((o) => o.primaryKey).sort()).toEqual([
      'A0',
      'A1',
      'A2',
      'A3',
      'A4',
    ]);
    // Dialog dismisses itself on Add.
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('Given_open_false_When_render_Then_renderNothing', () => {
    const { container } = render(
      withQueryClient(
        <VertexAddObjectsDialog
          open={false}
          ontologyApiName="main"
          onClose={() => {}}
          onAdd={() => {}}
        />,
      ),
    );
    expect(container.firstChild).toBeNull();
  });
});
