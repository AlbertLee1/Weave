import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

// Mock the quiver API module so the page renders without hitting the network.
const apiMocks = vi.hoisted(() => ({
  viewQuiverDashboard: vi.fn(),
  listQuiverDashboards: vi.fn(),
  getQuiverDashboard: vi.fn(),
  saveQuiverDashboard: vi.fn(),
  deleteQuiverDashboard: vi.fn(),
  batchQuiverSparklines: vi.fn().mockResolvedValue({ rid: '', series: [] }),
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
          <Route path="*" element={<QuiverViewPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const LOADED_DASHBOARD = {
  rid: 'ri.quiver.main.dashboard.abc',
  name: 'Shared Dashboard',
  owner: 'user:alice',
  createdAt: '2026-04-18T10:00:00Z',
  updatedAt: '2026-04-18T10:00:00Z',
  config: {
    ontologyApiName: 'demo',
    series: [
      {
        id: 'a',
        objectType: 'Server',
        primaryKey: 's1',
        property: 'cpu',
        label: 'CPU',
        color: '#22d3ee',
      },
    ],
  },
};

// QuiverViewPage is a standalone route page; for screen-reader landmark
// navigation it must expose a single stable top-level <h1> across all of its
// state branches (missing-rid / loading / error / loaded).
describe('BDD: QuiverViewPage exposes a stable top-level h1', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    apiMocks.viewQuiverDashboard.mockReset();
    apiMocks.batchQuiverSparklines.mockReset();
    apiMocks.batchQuiverSparklines.mockResolvedValue({ rid: '', series: [] });
    tsMocks.streamTimeSeriesPoints.mockResolvedValue([]);
  });

  // Given a loaded shared dashboard,
  // Then there is exactly one level-1 heading on the page.
  it('renders exactly one h1 in the loaded state', async () => {
    apiMocks.viewQuiverDashboard.mockResolvedValue(LOADED_DASHBOARD);

    renderViewPage('/quiver/demo/ri.quiver.main.dashboard.abc/view');

    await waitFor(() => {
      expect(screen.getByTestId('quiver-view-page')).toBeInTheDocument();
    });
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
  });

  // Given the dashboard is still loading,
  // Then the page already exposes exactly one h1.
  it('renders exactly one h1 in the loading state', () => {
    // Never resolves: the page stays in its loading branch.
    apiMocks.viewQuiverDashboard.mockReturnValue(new Promise(() => {}));

    renderViewPage('/quiver/demo/ri.quiver.main.dashboard.abc/view');

    expect(screen.getByTestId('quiver-view-loading')).toBeInTheDocument();
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
  });

  // Given the share endpoint 404s,
  // Then the error state still exposes exactly one h1.
  it('renders exactly one h1 in the error state', async () => {
    apiMocks.viewQuiverDashboard.mockRejectedValue(
      Object.assign(new Error('boom'), {
        statusCode: 404,
        errorName: 'QuiverDashboardNotFound',
        errorCode: 'NOT_FOUND',
        errorInstanceId: 'x',
        name: 'ApiRequestError',
      }),
    );

    renderViewPage('/quiver/demo/ri.quiver.main.dashboard.missing/view');

    await waitFor(() => {
      expect(screen.getByTestId('quiver-view-error')).toBeInTheDocument();
    });
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
  });

  // Given no RID is supplied in the URL,
  // Then the missing-dashboard state still exposes exactly one h1.
  it('renders exactly one h1 in the missing-rid state', () => {
    renderViewPage('/quiver/demo//view');

    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
  });
});
