import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

// US-483 BDD — Quiver Sparkline 多系列预加载.
//
// PRD literal: "前端 dashboard load 改为单请求".
//
// The two scenarios pin the contract from the SPA side:
//   - Given a saved 3-series dashboard, when the SPA mounts
//     QuiverViewPage, then exactly ONE batch call to
//     batchQuiverSparklines is made AND zero per-series
//     streamTimeSeriesPoints requests fire.
//   - Negative control: an empty dashboard (no series) skips the
//     batch call entirely — the regression that would always call it
//     wastefully is caught here.

const apiMocks = vi.hoisted(() => ({
  viewQuiverDashboard: vi.fn(),
  listQuiverDashboards: vi.fn(),
  getQuiverDashboard: vi.fn(),
  saveQuiverDashboard: vi.fn(),
  deleteQuiverDashboard: vi.fn(),
  batchQuiverSparklines: vi.fn(),
}));

vi.mock('../../../api/quiver', () => apiMocks);

const tsMocks = vi.hoisted(() => ({
  streamTimeSeriesPoints: vi.fn().mockResolvedValue([]),
}));
vi.mock('../../../api/timeseries', () => tsMocks);

import { QuiverViewPage } from '../QuiverViewPage';

function renderViewPage(initialPath: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
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

describe('US-483 — QuiverViewPage batches sparklines into one request', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    apiMocks.viewQuiverDashboard.mockReset();
    apiMocks.batchQuiverSparklines.mockReset();
    tsMocks.streamTimeSeriesPoints.mockReset();
    tsMocks.streamTimeSeriesPoints.mockResolvedValue([]);
  });

  it('fires exactly one batchQuiverSparklines call for a 3-series dashboard and zero per-series fetches', async () => {
    // Given a 3-series saved dashboard.
    apiMocks.viewQuiverDashboard.mockResolvedValue({
      rid: 'ri.quiver.main.dashboard.us483',
      name: 'US-483',
      owner: 'user:alice',
      createdAt: '2026-04-18T10:00:00Z',
      updatedAt: '2026-04-18T10:00:00Z',
      config: {
        ontologyApiName: 'demo',
        series: [
          { id: 's1', objectType: 'Host', primaryKey: 'h1', property: 'cpu', label: 'h1', color: '#1' },
          { id: 's2', objectType: 'Host', primaryKey: 'h2', property: 'cpu', label: 'h2', color: '#2' },
          { id: 's3', objectType: 'Host', primaryKey: 'h3', property: 'cpu', label: 'h3', color: '#3' },
        ],
      },
    });
    // And the batch endpoint returns one envelope carrying all 3 series.
    apiMocks.batchQuiverSparklines.mockResolvedValue({
      rid: 'ri.quiver.main.dashboard.us483',
      series: [
        { id: 's1', label: 'h1', color: '#1', objectType: 'Host', primaryKey: 'h1', property: 'cpu', points: [{ time: '2026-01-01T00:00:00Z', value: 1 }] },
        { id: 's2', label: 'h2', color: '#2', objectType: 'Host', primaryKey: 'h2', property: 'cpu', points: [{ time: '2026-01-01T00:00:00Z', value: 2 }] },
        { id: 's3', label: 'h3', color: '#3', objectType: 'Host', primaryKey: 'h3', property: 'cpu', points: [{ time: '2026-01-01T00:00:00Z', value: 3 }] },
      ],
    });

    // When the share page mounts.
    renderViewPage('/quiver/demo/ri.quiver.main.dashboard.us483/view');

    // Then the dashboard renders.
    await waitFor(() => {
      expect(screen.getByTestId('quiver-view-page')).toBeInTheDocument();
    });
    // And one batch call fired carrying the dashboard RID.
    await waitFor(() => {
      expect(apiMocks.batchQuiverSparklines).toHaveBeenCalledTimes(1);
    });
    expect(apiMocks.batchQuiverSparklines).toHaveBeenCalledWith(
      'ri.quiver.main.dashboard.us483',
      {},
    );

    // And zero per-series requests fired — the whole point of US-483.
    // We give react-query a microtask to settle before asserting the
    // zero-call invariant; the batch promise resolves synchronously
    // in this fake.
    await waitFor(() => {
      // Each series row should be rendered.
      expect(screen.getByTestId('quiver-row-s1')).toBeInTheDocument();
      expect(screen.getByTestId('quiver-row-s2')).toBeInTheDocument();
      expect(screen.getByTestId('quiver-row-s3')).toBeInTheDocument();
    });
    expect(tsMocks.streamTimeSeriesPoints).not.toHaveBeenCalled();
  });

  it('does not fire the batch call when the dashboard has no series', async () => {
    apiMocks.viewQuiverDashboard.mockResolvedValue({
      rid: 'ri.quiver.main.dashboard.us483-empty',
      name: 'empty',
      owner: 'user:alice',
      createdAt: '2026-04-18T10:00:00Z',
      updatedAt: '2026-04-18T10:00:00Z',
      config: { ontologyApiName: 'demo', series: [] },
    });
    apiMocks.batchQuiverSparklines.mockResolvedValue({
      rid: 'ri.quiver.main.dashboard.us483-empty',
      series: [],
    });

    renderViewPage('/quiver/demo/ri.quiver.main.dashboard.us483-empty/view');

    await waitFor(() => {
      expect(screen.getByTestId('quiver-view-page')).toBeInTheDocument();
    });
    // The empty-state copy renders instead of the workbench.
    expect(screen.getByText('Empty dashboard')).toBeInTheDocument();
    // And the batch call MUST stay un-fired — wastefully fetching for
    // a zero-series dashboard would defeat the "load = one request"
    // promise on the degenerate case.
    expect(apiMocks.batchQuiverSparklines).not.toHaveBeenCalled();
    expect(tsMocks.streamTimeSeriesPoints).not.toHaveBeenCalled();
  });
});
