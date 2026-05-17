import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

const apiMocks = vi.hoisted(() => ({
  viewQuiverDashboard: vi.fn(),
  listQuiverDashboards: vi.fn(),
  getQuiverDashboard: vi.fn(),
  saveQuiverDashboard: vi.fn(),
  deleteQuiverDashboard: vi.fn(),
  // US-483: batch sparkline fetch invoked once the dashboard envelope
  // resolves. Default to an empty series list so existing scenarios
  // that don't seed data still render without spinners.
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
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('QuiverViewPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    apiMocks.viewQuiverDashboard.mockReset();
    apiMocks.batchQuiverSparklines.mockReset();
    apiMocks.batchQuiverSparklines.mockResolvedValue({ rid: '', series: [] });
    tsMocks.streamTimeSeriesPoints.mockResolvedValue([]);
  });

  it('renders the dashboard from the share endpoint', async () => {
    apiMocks.viewQuiverDashboard.mockResolvedValue({
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
    });

    renderViewPage('/quiver/demo/ri.quiver.main.dashboard.abc/view');

    await waitFor(() => {
      expect(screen.getByTestId('quiver-view-page')).toBeInTheDocument();
    });
    expect(screen.getByTestId('quiver-view-title')).toHaveTextContent(
      'Shared Dashboard',
    );
    // Read-only mode: no add form, no remove buttons.
    expect(screen.queryByTestId('quiver-add-form')).not.toBeInTheDocument();
    expect(
      screen.queryByTestId('quiver-save-controls'),
    ).not.toBeInTheDocument();
    // Series panel rendered with the persisted spec.
    expect(screen.getByTestId('quiver-row-a')).toBeInTheDocument();
    expect(screen.queryByTestId('quiver-remove-a')).not.toBeInTheDocument();
    expect(apiMocks.viewQuiverDashboard).toHaveBeenCalledWith(
      'ri.quiver.main.dashboard.abc',
    );
  });

  it('shows a not-found state when the share endpoint 404s', async () => {
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
  });
});
