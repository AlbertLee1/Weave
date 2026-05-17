import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { DashboardPage } from '../DashboardPage';

// DOG-002 BDD — Dashboard global "Object Types" stat must reflect actual
// per-ontology counts from the API, not a hardcoded 0.
//
// PRD acceptance:
//   1. When at least one ontology has object types, the global Object Types
//      stat equals the sum of per-card counts (and is > 0).
//   2. With IoT Demo having 4 types and Northwind having 4 seeded types,
//      the stat is >= 4 and matches API-derived totals (i.e. 8).
//   3. While per-ontology fetches are loading, the dashboard does not present
//      a final misleading "0" as the completed total — either it shows the
//      loading skeleton or it waits to render the final value.

const ONTOLOGIES = [
  {
    rid: 'ri.ontology.main.ontology.northwind',
    apiName: 'northwind',
    displayName: 'Northwind',
    description: 'Northwind sample data',
  },
  {
    rid: 'ri.ontology.main.ontology.iotDemo',
    apiName: 'iotDemo',
    displayName: 'IoT Demo',
    description: 'IoT demo ontology',
  },
];

const NORTHWIND_TYPES = [
  { rid: 'ri.ot.nw.customer', apiName: 'Customer', displayName: 'Customer', primaryKey: 'id', status: 'ACTIVE', visibility: 'PROMINENT' },
  { rid: 'ri.ot.nw.order', apiName: 'Order', displayName: 'Order', primaryKey: 'id', status: 'ACTIVE', visibility: 'PROMINENT' },
  { rid: 'ri.ot.nw.product', apiName: 'Product', displayName: 'Product', primaryKey: 'id', status: 'ACTIVE', visibility: 'PROMINENT' },
  { rid: 'ri.ot.nw.employee', apiName: 'Employee', displayName: 'Employee', primaryKey: 'id', status: 'ACTIVE', visibility: 'PROMINENT' },
];

const IOT_TYPES = [
  { rid: 'ri.ot.iot.device', apiName: 'Device', displayName: 'Device', primaryKey: 'id', status: 'ACTIVE', visibility: 'PROMINENT' },
  { rid: 'ri.ot.iot.sensor', apiName: 'Sensor', displayName: 'Sensor', primaryKey: 'id', status: 'ACTIVE', visibility: 'PROMINENT' },
  { rid: 'ri.ot.iot.reading', apiName: 'Reading', displayName: 'Reading', primaryKey: 'id', status: 'ACTIVE', visibility: 'PROMINENT' },
  { rid: 'ri.ot.iot.gateway', apiName: 'Gateway', displayName: 'Gateway', primaryKey: 'id', status: 'ACTIVE', visibility: 'PROMINENT' },
];

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function installFetch(opts?: { northwindDelayMs?: number }) {
  const delayMs = opts?.northwindDelayMs ?? 0;
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      if (/\/api\/v2\/ontologies(\?.*)?$/.test(url)) {
        return jsonResponse({ data: ONTOLOGIES });
      }
      if (/\/api\/v2\/ontologies\/northwind\/objectTypes(\?.*)?$/.test(url)) {
        if (delayMs > 0) {
          await new Promise((r) => setTimeout(r, delayMs));
        }
        return jsonResponse({ data: NORTHWIND_TYPES });
      }
      if (/\/api\/v2\/ontologies\/iotDemo\/objectTypes(\?.*)?$/.test(url)) {
        return jsonResponse({ data: IOT_TYPES });
      }
      return new Response('{}', { status: 200 });
    }),
  );
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <DashboardPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('DashboardPage — total Object Types stat (DOG-002)', () => {
  beforeEach(() => {
    vi.useRealTimers();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('sums per-ontology object type counts into the global stat', async () => {
    installFetch();
    renderPage();

    // Wait for both per-ontology cards to render their counts.
    await waitFor(() => {
      const cards = screen.getAllByTestId('dashboard-ontology-card-wrapper');
      expect(cards.length).toBe(2);
      for (const card of cards) {
        expect(within(card).getByText(/\d+\s*types?/)).toBeInTheDocument();
      }
    });

    // The Object Types stat should be the sum: 4 + 4 = 8.
    await waitFor(() => {
      const stat = screen.getByTestId('stat-object-types');
      const value = within(stat).getByTestId('stat-value');
      expect(value.textContent).toBe('8');
    });
  });

  it('matches sum of per-card type counts shown on the cards', async () => {
    installFetch();
    renderPage();

    await waitFor(() => {
      expect(screen.getAllByTestId('dashboard-ontology-card-wrapper').length).toBe(2);
    });

    await waitFor(() => {
      const cards = screen.getAllByTestId('dashboard-ontology-card-wrapper');
      let sum = 0;
      for (const card of cards) {
        const chip = within(card).getByText(/^(\d+)\s*types?$/);
        const match = chip.textContent?.match(/^(\d+)\s*types?$/);
        sum += match ? Number(match[1]) : 0;
      }
      const value = within(screen.getByTestId('stat-object-types')).getByTestId('stat-value');
      expect(Number(value.textContent)).toBe(sum);
      expect(Number(value.textContent)).toBeGreaterThanOrEqual(4);
    });
  });

  it('does not display a misleading 0 once ontologies have loaded but counts are still pending', async () => {
    // Northwind takes a long time; iotDemo resolves immediately. By the
    // time the page leaves the ontology-loading skeleton, neither full
    // total is available yet — so the stat must not render the bogus 0.
    installFetch({ northwindDelayMs: 200 });
    renderPage();

    // Wait until the dashboard has finished its ontology-list loading
    // skeleton (cards are now mounted).
    await waitFor(() => {
      expect(screen.getAllByTestId('dashboard-ontology-card-wrapper').length).toBe(2);
    });

    // Immediately after mount, the IoT card resolves but Northwind is
    // still pending — assert the stat is NOT the misleading 0.
    const stat = screen.getByTestId('stat-object-types');
    const value = within(stat).getByTestId('stat-value');
    // It can be 4 (iot only, with northwind treated as 0 placeholder is bad),
    // a loading marker, or eventually 8 — but it must not be the literal 0
    // that the bug produced.
    expect(value.textContent).not.toBe('0');

    // Eventually it converges to 8 (the sum) once northwind resolves.
    await waitFor(() => {
      expect(within(screen.getByTestId('stat-object-types')).getByTestId('stat-value').textContent).toBe('8');
    });
  });
});
