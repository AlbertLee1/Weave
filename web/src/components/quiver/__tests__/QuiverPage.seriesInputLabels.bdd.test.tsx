import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

// Mock the quiver API module so the page renders without hitting the network.
// The "add series" form is rendered unconditionally, so an empty dashboard
// list is enough to exercise its inputs.
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

// Given the Quiver "add series" form is on screen,
// Then each of its inputs exposes a screen-reader accessible name so they can
// be located by role + name (not just by placeholder).
describe('BDD: QuiverPage "add series" form inputs have accessible names', () => {
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

  it('exposes an accessible name for the Object type input', async () => {
    renderPage();
    expect(
      await screen.findByRole('textbox', { name: /object type/i }),
    ).toBeInTheDocument();
  });

  it('exposes an accessible name for the Primary key input', async () => {
    renderPage();
    expect(
      await screen.findByRole('textbox', { name: /primary key/i }),
    ).toBeInTheDocument();
  });

  it('exposes an accessible name for the Property input', async () => {
    renderPage();
    expect(
      await screen.findByRole('textbox', { name: /property/i }),
    ).toBeInTheDocument();
  });

  it('exposes an accessible name for the Label input', async () => {
    renderPage();
    expect(
      await screen.findByRole('textbox', { name: /label/i }),
    ).toBeInTheDocument();
  });

  it('exposes an accessible name for the Branch input', async () => {
    renderPage();
    expect(
      await screen.findByRole('textbox', { name: /branch/i }),
    ).toBeInTheDocument();
  });
});
