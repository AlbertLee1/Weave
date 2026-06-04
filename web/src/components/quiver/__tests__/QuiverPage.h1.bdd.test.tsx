import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

// Mock the quiver API module so the page renders without hitting the network.
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

// QuiverPage is a standalone route page; for screen-reader landmark navigation
// it must expose its main title as the page's single top-level <h1>.
describe('BDD: QuiverPage exposes its title as a top-level h1', () => {
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

  // Given the Quiver page is rendered,
  // Then the "Quiver Workbench" title is a level-1 heading (h1).
  it('renders the "Quiver Workbench" title as an h1', () => {
    renderPage();
    expect(
      screen.getByRole('heading', { level: 1, name: /Quiver Workbench/i }),
    ).toBeInTheDocument();
  });

  // And there is exactly one h1 on the page (single top-level landmark).
  it('renders exactly one h1 on the page', () => {
    renderPage();
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
  });
});
