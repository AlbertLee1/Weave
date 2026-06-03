import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

import { QuiverPage } from '../QuiverPage';

// Unit 12 C6 BDD — per-series transform panel.
//
// Scenario: Given a series in the workbench, when the user defines a
// `resample` transform (interval + agg) and runs it, then the SPA POSTs
// the right body to /timeseries/transform and renders the transformed
// (bucketed) points.

let transformBody: unknown = null;

const server = setupServer(
  // Quiver dashboard list endpoint — degraded-mode returns []; keep quiet.
  http.get('/api/v2/quiver/dashboards', () =>
    HttpResponse.json({ dashboards: [] }),
  ),
  // Raw points fetch when a series is added (QuiverWorkbenchView fan-out).
  http.post(
    '/api/v2/ontologies/:ontology/objects/:objectType/:pk/timeseries/:property/streamPoints',
    () =>
      HttpResponse.json([
        { time: '2026-01-01T00:00:00Z', value: 10 },
        { time: '2026-01-01T00:30:00Z', value: 20 },
        { time: '2026-01-01T01:00:00Z', value: 30 },
      ]),
  ),
  http.post(
    '/api/v2/ontologies/:ontology/timeseries/transform',
    async ({ request }) => {
      transformBody = await request.json();
      return HttpResponse.json({
        points: [
          { time: '2026-01-01T00:00:00Z', value: 15 },
          { time: '2026-01-01T01:00:00Z', value: 30 },
        ],
      });
    },
  ),
);

beforeAll(() => server.listen());
afterEach(() => {
  transformBody = null;
  server.resetHandlers();
  localStorage.clear();
});
afterAll(() => server.close());

function renderPage(initialPath = '/quiver/demo') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/quiver/:ontology" element={<QuiverPage />} />
          <Route path="/quiver/:ontology/:rid" element={<QuiverPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function addSeries(ot: string, pk: string, prop: string) {
  fireEvent.change(screen.getByTestId('quiver-input-objectType'), {
    target: { value: ot },
  });
  fireEvent.change(screen.getByTestId('quiver-input-primaryKey'), {
    target: { value: pk },
  });
  fireEvent.change(screen.getByTestId('quiver-input-property'), {
    target: { value: prop },
  });
  fireEvent.click(screen.getByTestId('quiver-add-button'));
}

describe('US-402 BDD — QuiverPage per-series transform panel', () => {
  it('Given a resample transform is defined, When run, Then it POSTs the right body to /timeseries/transform', async () => {
    renderPage();
    addSeries('Host', 'h1', 'cpu');

    // The transform panel exposes the added series.
    const panel = await screen.findByTestId('quiver-transform-panel');
    expect(panel).toBeInTheDocument();

    // Pick the series, op=resample, interval=1h, agg=sum.
    const seriesSelect = within(panel).getByTestId(
      'transform-series-select',
    ) as HTMLSelectElement;
    const rid = seriesSelect.options[0].value;
    fireEvent.change(seriesSelect, { target: { value: rid } });
    fireEvent.change(within(panel).getByTestId('transform-op-select'), {
      target: { value: 'resample' },
    });
    fireEvent.change(within(panel).getByTestId('transform-interval-input'), {
      target: { value: '1h' },
    });
    fireEvent.change(within(panel).getByTestId('transform-agg-select'), {
      target: { value: 'sum' },
    });
    fireEvent.click(within(panel).getByTestId('transform-add-step'));

    // Run the chain.
    fireEvent.click(within(panel).getByTestId('transform-run'));

    await waitFor(() => {
      expect(transformBody).not.toBeNull();
    });
    expect(transformBody).toEqual({
      source: { objectType: 'Host', primaryKey: 'h1', property: 'cpu' },
      transforms: [
        { op: 'resample', params: { interval: '1h', agg: 'sum' } },
      ],
    });

    // The transformed points are surfaced.
    await waitFor(() => {
      expect(
        within(panel).getByTestId('transform-result-count'),
      ).toHaveTextContent('2');
    });
  });

  it('does not POST until at least one transform step is defined', async () => {
    renderPage();
    addSeries('Host', 'h1', 'cpu');

    const panel = await screen.findByTestId('quiver-transform-panel');
    // Run button is disabled with an empty chain.
    expect(within(panel).getByTestId('transform-run')).toBeDisabled();
    expect(transformBody).toBeNull();
  });
});
