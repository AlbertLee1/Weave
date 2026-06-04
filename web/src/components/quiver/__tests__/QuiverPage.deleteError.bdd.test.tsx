import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';

// Mock the quiver API module so the page lists a deletable dashboard and
// we can drive the delete mutation into success/failure per scenario.
const quiverMocks = vi.hoisted(() => ({
  listQuiverDashboards: vi.fn(),
  getQuiverDashboard: vi.fn(),
  saveQuiverDashboard: vi.fn(),
  deleteQuiverDashboard: vi.fn(),
  viewQuiverDashboard: vi.fn(),
}));
vi.mock('../../../api/quiver', () => quiverMocks);

import { QuiverPage } from '../QuiverPage';
import { ApiRequestError } from '../../../api/client';
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

describe('BDD: QuiverPage dashboard delete error handling', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.removeItem('weave-active-branch');
    useBranchStore.setState({ selections: {} });
    quiverMocks.listQuiverDashboards.mockReset();
    quiverMocks.deleteQuiverDashboard.mockReset();
    quiverMocks.getQuiverDashboard.mockReset();
    quiverMocks.saveQuiverDashboard.mockReset();
    quiverMocks.viewQuiverDashboard.mockReset();
    // Page lists exactly one saved dashboard with a delete button.
    quiverMocks.listQuiverDashboards.mockResolvedValue({
      dashboards: [SAVED_DASHBOARD],
    });
    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);
  });

  // Given the Quiver page lists a saved dashboard,
  // When the user clicks delete but the request fails with an API error,
  // Then a friendly error message is shown (mirroring the save error path).
  it('surfaces a friendly error when delete fails with an ApiRequestError', async () => {
    quiverMocks.deleteQuiverDashboard.mockRejectedValue(
      new ApiRequestError({
        statusCode: 403,
        errorCode: 'PERMISSION_DENIED',
        errorName: 'DeleteNotPermitted',
        errorInstanceId: 'inst-1',
        parameters: { rid: SAVED_DASHBOARD.rid },
      }),
    );

    renderPage();

    const deleteBtn = await screen.findByTestId(
      `quiver-delete-${SAVED_DASHBOARD.rid}`,
    );
    fireEvent.click(deleteBtn);

    const errorBox = await screen.findByTestId('quiver-save-error');
    expect(errorBox).toHaveTextContent('DeleteNotPermitted');
    expect(errorBox).toHaveTextContent(SAVED_DASHBOARD.rid);
  });

  // Given the delete request rejects with a plain (non-API) error,
  // Then the raw error string is surfaced rather than swallowed.
  it('surfaces a generic error when delete fails with a non-API error', async () => {
    quiverMocks.deleteQuiverDashboard.mockRejectedValue(
      new Error('network down'),
    );

    renderPage();

    const deleteBtn = await screen.findByTestId(
      `quiver-delete-${SAVED_DASHBOARD.rid}`,
    );
    fireEvent.click(deleteBtn);

    const errorBox = await screen.findByTestId('quiver-save-error');
    expect(errorBox).toHaveTextContent('network down');
  });

  // Given a delete is in flight,
  // Then the delete button is disabled (guarding against double-submit)
  // and shows an in-flight hint.
  it('disables the delete button and shows an in-flight hint while pending', async () => {
    let resolveDelete: (() => void) | undefined;
    quiverMocks.deleteQuiverDashboard.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveDelete = resolve;
        }),
    );

    renderPage();

    const deleteBtn = await screen.findByTestId(
      `quiver-delete-${SAVED_DASHBOARD.rid}`,
    );
    expect(deleteBtn).not.toBeDisabled();

    fireEvent.click(deleteBtn);

    await waitFor(() => {
      expect(
        screen.getByTestId(`quiver-delete-${SAVED_DASHBOARD.rid}`),
      ).toBeDisabled();
    });
    expect(
      screen.getByTestId(`quiver-delete-${SAVED_DASHBOARD.rid}`),
    ).toHaveTextContent('…');

    // Resolve so the test doesn't leak a pending promise.
    resolveDelete?.();
  });

  // Given a delete succeeds,
  // Then the saved-dashboards list is refreshed and no error is shown
  // (existing success path remains intact).
  it('keeps the success path: refetches the list and shows no error', async () => {
    quiverMocks.deleteQuiverDashboard.mockResolvedValue(undefined);

    renderPage();

    const deleteBtn = await screen.findByTestId(
      `quiver-delete-${SAVED_DASHBOARD.rid}`,
    );
    fireEvent.click(deleteBtn);

    await waitFor(() => {
      expect(quiverMocks.deleteQuiverDashboard).toHaveBeenCalled();
    });
    expect(quiverMocks.deleteQuiverDashboard.mock.calls[0][0]).toBe(
      SAVED_DASHBOARD.rid,
    );
    // List query is invalidated → refetched at least once after the delete.
    await waitFor(() => {
      expect(
        quiverMocks.listQuiverDashboards.mock.calls.length,
      ).toBeGreaterThanOrEqual(2);
    });
    expect(screen.queryByTestId('quiver-save-error')).not.toBeInTheDocument();
  });
});
