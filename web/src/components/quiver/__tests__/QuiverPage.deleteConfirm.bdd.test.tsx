import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

// Mock the quiver API module so the page lists a deletable dashboard and we
// can assert whether the delete request is (or is NOT) actually fired.
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

const SAVED_DASHBOARD = {
  rid: 'ri.quiver.main.dashboard.deletable',
  name: 'Capacity Review',
  owner: 'user:test',
  config: { ontologyApiName: 'test', series: [] },
  createdAt: '2026-04-18T10:00:00Z',
  updatedAt: '2026-04-18T10:00:00Z',
};

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

describe('BDD: QuiverPage confirms before deleting a dashboard', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.removeItem('weave-active-branch');
    useBranchStore.setState({ selections: {} });
    quiverMocks.listQuiverDashboards.mockReset();
    quiverMocks.deleteQuiverDashboard.mockReset();
    quiverMocks.getQuiverDashboard.mockReset();
    quiverMocks.saveQuiverDashboard.mockReset();
    quiverMocks.viewQuiverDashboard.mockReset();
    quiverMocks.listQuiverDashboards.mockResolvedValue({
      dashboards: [SAVED_DASHBOARD],
    });
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
  });

  // Given the Quiver page lists a saved dashboard,
  // When the user clicks the row's Delete (×) button,
  // Then the delete API is NOT called immediately and a confirmation
  // dialog appears naming the dashboard.
  it('does not delete immediately; opens a confirmation dialog naming the dashboard', async () => {
    const user = userEvent.setup();
    quiverMocks.deleteQuiverDashboard.mockResolvedValue(undefined);

    renderPage();

    const deleteBtn = await screen.findByTestId(
      `quiver-delete-${SAVED_DASHBOARD.rid}`,
    );
    await user.click(deleteBtn);

    // No destructive call yet — the gate must stop here.
    expect(quiverMocks.deleteQuiverDashboard).not.toHaveBeenCalled();

    // A confirmation dialog appears, naming the dashboard.
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/Capacity Review/)).toBeInTheDocument();
    expect(
      within(dialog).getByText(/cannot be undone/i),
    ).toBeInTheDocument();
  });

  // Given the confirmation dialog is open,
  // When the user clicks Cancel,
  // Then the dialog closes and nothing is deleted.
  it('cancelling the dialog closes it and deletes nothing', async () => {
    const user = userEvent.setup();
    quiverMocks.deleteQuiverDashboard.mockResolvedValue(undefined);

    renderPage();

    const deleteBtn = await screen.findByTestId(
      `quiver-delete-${SAVED_DASHBOARD.rid}`,
    );
    await user.click(deleteBtn);

    const dialog = await screen.findByRole('dialog');
    await user.click(
      within(dialog).getByTestId('quiver-delete-confirm-cancel'),
    );

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
    expect(quiverMocks.deleteQuiverDashboard).not.toHaveBeenCalled();
  });

  // Given the confirmation dialog is open,
  // When the user clicks the destructive Delete button,
  // Then the delete API is called with the dashboard's rid, the dialog
  // closes and the row disappears once the list refetches empty.
  it('confirming triggers the delete and removes the row on success', async () => {
    const user = userEvent.setup();
    quiverMocks.deleteQuiverDashboard.mockResolvedValue(undefined);
    // First list load shows the dashboard; the invalidated refetch after a
    // successful delete returns an empty list so the row disappears.
    quiverMocks.listQuiverDashboards
      .mockResolvedValueOnce({ dashboards: [SAVED_DASHBOARD] })
      .mockResolvedValue({ dashboards: [] });

    renderPage();

    const deleteBtn = await screen.findByTestId(
      `quiver-delete-${SAVED_DASHBOARD.rid}`,
    );
    await user.click(deleteBtn);

    const dialog = await screen.findByRole('dialog');
    await user.click(
      within(dialog).getByTestId('quiver-delete-confirm-confirm'),
    );

    await waitFor(() => {
      expect(quiverMocks.deleteQuiverDashboard).toHaveBeenCalledTimes(1);
    });
    expect(quiverMocks.deleteQuiverDashboard.mock.calls[0][0]).toBe(
      SAVED_DASHBOARD.rid,
    );

    // Dialog closes after a successful confirm.
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });

    // The invalidated refetch returned an empty list, so the row disappears.
    await waitFor(() => {
      expect(
        screen.queryByTestId(`quiver-delete-${SAVED_DASHBOARD.rid}`),
      ).not.toBeInTheDocument();
    });
  });
});
