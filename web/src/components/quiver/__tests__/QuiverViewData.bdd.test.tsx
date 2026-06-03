import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

import { QuiverViewPage } from '../QuiverViewPage';
import { TimeRangePicker } from '../../layout/TimeRangePicker';

// Unit 12 C1 BDD — windowed /data on the read-only Quiver share page.
//
// Scenario: Given a saved dashboard whose URL carries ?from=&to=&step=,
// when the page renders, then it calls GET /data with exactly those
// query params and renders the bucketed series the server returns.
// When the user changes the time-range preset, then the /data call
// re-fires with the new window + step.

const DASHBOARD = {
  rid: 'ri.quiver.main.dashboard.u12data',
  name: 'Windowed',
  owner: 'user:alice',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  config: {
    ontologyApiName: 'demo',
    series: [
      { id: 's1', objectType: 'Host', primaryKey: 'h1', property: 'cpu', label: 'CPU', color: '#22d3ee' },
    ],
  },
};

const dataCalls: { from: string | null; to: string | null; step: string | null }[] = [];

const server = setupServer(
  http.get('/api/v2/quiver/dashboards/:rid/view', () =>
    HttpResponse.json(DASHBOARD),
  ),
  http.get('/api/v2/quiver/dashboards/:rid/data', ({ request }) => {
    const url = new URL(request.url);
    dataCalls.push({
      from: url.searchParams.get('from'),
      to: url.searchParams.get('to'),
      step: url.searchParams.get('step'),
    });
    return HttpResponse.json({
      rid: DASHBOARD.rid,
      from: url.searchParams.get('from') ?? '',
      to: url.searchParams.get('to') ?? '',
      step: url.searchParams.get('step') ?? '',
      series: [
        {
          id: 's1',
          label: 'CPU',
          color: '#22d3ee',
          objectType: 'Host',
          primaryKey: 'h1',
          property: 'cpu',
          points: [
            { time: '2026-01-01T00:00:00Z', value: 10 },
            { time: '2026-01-01T00:05:00Z', value: 30 },
          ],
        },
      ],
    });
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
  dataCalls.length = 0;
  server.resetHandlers();
});
afterAll(() => server.close());

// Deterministic "now" so the picker emits stable from/to params.
const FIXED_NOW = Date.parse('2026-01-02T00:00:00Z');

function renderView(initialPath: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        {/* The picker lives in the TopBar in the real app; we mount it
            alongside the page here to drive the URL search params. */}
        <TimeRangePicker now={() => FIXED_NOW} />
        <Routes>
          <Route
            path="/quiver/:ontology/:rid/view"
            element={<QuiverViewPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('US-482 BDD — QuiverViewPage windowed /data', () => {
  it('Given URL carries from/to/step, When the page renders, Then /data is called with those params and renders bucketed series', async () => {
    const from = '2026-01-01T00:00:00Z';
    const to = '2026-01-02T00:00:00Z';
    renderView(
      `/quiver/demo/${DASHBOARD.rid}/view?from=${from}&to=${to}&step=5m`,
    );

    await waitFor(() => {
      expect(screen.getByTestId('quiver-view-page')).toBeInTheDocument();
    });

    // /data fired with the exact window + step.
    await waitFor(() => {
      expect(dataCalls.length).toBeGreaterThanOrEqual(1);
    });
    expect(dataCalls[0]).toEqual({ from, to, step: '5m' });

    // The bucketed series renders: count = 2, sum = 40, avg = 20.
    const row = await screen.findByTestId('quiver-row-s1');
    expect(within(row).getByText('2')).toBeInTheDocument();
    expect(screen.getByTestId('quiver-sum-s1')).toHaveTextContent('40.00');
    expect(screen.getByTestId('quiver-avg-s1')).toHaveTextContent('20.00');
  });

  it('When the user picks a different time-range preset, Then /data re-fires with the new window + step', async () => {
    renderView(`/quiver/demo/${DASHBOARD.rid}/view?range=24h&from=a&to=b&step=5m`);

    await waitFor(() => {
      expect(screen.getByTestId('quiver-view-page')).toBeInTheDocument();
    });
    await waitFor(() => expect(dataCalls.length).toBeGreaterThanOrEqual(1));
    const before = dataCalls.length;

    // Click the 1h preset — its bundled step is 1m.
    fireEvent.click(screen.getByTestId('time-range-1h'));

    await waitFor(() => {
      expect(dataCalls.length).toBeGreaterThan(before);
    });
    const last = dataCalls[dataCalls.length - 1];
    expect(last.step).toBe('1m');
    // The 1h window is now-1h .. now against FIXED_NOW.
    expect(last.from).toBe(new Date(FIXED_NOW - 60 * 60 * 1000).toISOString());
    expect(last.to).toBe(new Date(FIXED_NOW).toISOString());
  });
});
