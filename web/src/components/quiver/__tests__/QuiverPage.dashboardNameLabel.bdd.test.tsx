import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

// Mock the quiver API module so the page renders without hitting the network.
// A successful (non-error) dashboards list keeps `dashboardsAvailable` true, so
// the save controls — including the dashboard-name input — are rendered.
const quiverMocks = vi.hoisted(() => ({
  listQuiverDashboards: vi.fn(),
  getQuiverDashboard: vi.fn(),
  saveQuiverDashboard: vi.fn(),
  deleteQuiverDashboard: vi.fn(),
  viewQuiverDashboard: vi.fn(),
}));
vi.mock('../../../api/quiver', () => quiverMocks);

import { QuiverPage } from '../QuiverPage';
import * as timeseriesApi from '../../../api/timeseries';
import { useBranchStore } from '../../../stores/branchStore';

function renderPage(initialPath = '/quiver/test') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/quiver/:ontology" element={<QuiverPage />} />
          <Route path="/quiver/:ontology/:rid" element={<QuiverPage />} />
          <Route path="*" element={<QuiverPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// Given the Quiver page is rendered with the save controls visible,
// Then the "Dashboard name" input exposes a screen-reader accessible name so it
// can be located by role + name (not only by placeholder).
describe('BDD: QuiverPage dashboard-name input has an accessible name', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.removeItem('weave-active-branch');
    useBranchStore.setState({ selections: {} });
    quiverMocks.listQuiverDashboards.mockReset();
    quiverMocks.getQuiverDashboard.mockReset();
    quiverMocks.saveQuiverDashboard.mockReset();
    quiverMocks.deleteQuiverDashboard.mockReset();
    quiverMocks.viewQuiverDashboard.mockReset();
    quiverMocks.listQuiverDashboards.mockResolvedValue({ dashboards: [] });
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
  });

  it('exposes an accessible name for the Dashboard name input', async () => {
    renderPage();
    expect(
      await screen.findByRole('textbox', { name: /dashboard name/i }),
    ).toBeInTheDocument();
  });
});
