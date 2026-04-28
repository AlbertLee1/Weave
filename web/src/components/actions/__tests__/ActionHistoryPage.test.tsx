import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ActionHistoryPage } from '../ActionHistoryPage';
import * as actionHistoryApi from '../../../api/actionHistory';
import * as ontologiesApi from '../../../api/ontologies';
import type { ActionHistoryEntry } from '../../../api/actionHistory';
import type { ActionType } from '../../../api/types';
import { ApiRequestError } from '../../../api/client';

const successEntry: ActionHistoryEntry = {
  id: 1,
  actionTypeRid: 'rid:at:create',
  userId: 'user:alice',
  parameters: { name: 'Alice' },
  edits: [{ type: 'createObject' }],
  status: 'SUCCESS',
  createdAt: '2026-04-28T14:00:00Z',
};

const failedEntry: ActionHistoryEntry = {
  id: 2,
  actionTypeRid: 'rid:at:delete',
  userId: 'user:bob',
  parameters: {},
  edits: [],
  status: 'FAILED',
  errorMessage: 'boom',
  createdAt: '2026-04-28T13:00:00Z',
};

const actionTypes: ActionType[] = [
  {
    rid: 'rid:at:create',
    apiName: 'createEmployee',
    displayName: 'Create Employee',
    parameters: [],
  } as unknown as ActionType,
  {
    rid: 'rid:at:delete',
    apiName: 'deleteEmployee',
    displayName: 'Delete Employee',
    parameters: [],
  } as unknown as ActionType,
];

function renderPage(initial = '/actions/default/history') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route
            path="/actions/:ontology/history"
            element={<ActionHistoryPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ActionHistoryPage (US-317)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue(actionTypes);
  });

  it('renders the history list with status badges and action api names', async () => {
    vi.spyOn(actionHistoryApi, 'listActionHistory').mockResolvedValue({
      data: [successEntry, failedEntry],
      total: 2,
    });

    renderPage();

    const rows = await screen.findAllByTestId('action-history-row');
    expect(rows).toHaveLength(2);
    // First row: SUCCESS badge + apiName resolved from RID.
    expect(within(rows[0]).getByText('SUCCESS')).toBeInTheDocument();
    expect(within(rows[0]).getByText('createEmployee')).toBeInTheDocument();
    expect(within(rows[1]).getByText('FAILED')).toBeInTheDocument();
    expect(within(rows[1]).getByText('deleteEmployee')).toBeInTheDocument();
  });

  it('passes filter params through to the API call when filters change', async () => {
    const listSpy = vi
      .spyOn(actionHistoryApi, 'listActionHistory')
      .mockResolvedValue({ data: [successEntry], total: 1 });

    renderPage();

    await waitFor(() => expect(listSpy).toHaveBeenCalled());
    // Wait for the action-type dropdown to be populated before selecting one.
    await waitFor(() =>
      expect(
        within(screen.getByTestId('filter-action-type')).getByRole('option', {
          name: 'createEmployee',
        }),
      ).toBeInTheDocument(),
    );
    listSpy.mockClear();

    // Change action type filter
    fireEvent.change(screen.getByTestId('filter-action-type'), {
      target: { value: 'createEmployee' },
    });
    // Change status (Failed tab)
    fireEvent.click(screen.getByRole('tab', { name: 'Failed' }));
    // Change user id
    fireEvent.change(screen.getByTestId('filter-user-id'), {
      target: { value: 'user:bob' },
    });

    await waitFor(() => {
      const lastCall = listSpy.mock.calls[listSpy.mock.calls.length - 1];
      expect(lastCall?.[0]).toBe('default');
      expect(lastCall?.[1]).toMatchObject({
        actionType: 'createEmployee',
        status: 'FAILED',
        userId: 'user:bob',
      });
    });
  });

  it('opens a detail modal showing parameters and edits when View is clicked', async () => {
    vi.spyOn(actionHistoryApi, 'listActionHistory').mockResolvedValue({
      data: [successEntry],
      total: 1,
    });
    const detailSpy = vi
      .spyOn(actionHistoryApi, 'getActionHistoryEntry')
      .mockResolvedValue({
        ...successEntry,
        prevEdits: { foo: 'bar' },
      });

    renderPage();

    fireEvent.click(await screen.findByTestId('view-detail-btn'));
    const modal = await screen.findByTestId('modal-overlay');

    expect(detailSpy).toHaveBeenCalledWith('default', 1);
    await waitFor(() =>
      expect(within(modal).getByTestId('action-history-detail')).toBeInTheDocument(),
    );

    // Parameters and Edits + PrevEdits sections rendered.
    expect(within(modal).getByTestId('detail-parameters').textContent).toContain('Alice');
    expect(within(modal).getByTestId('detail-edits').textContent).toContain('createObject');
    expect(within(modal).getByTestId('detail-prev-edits').textContent).toContain('foo');
  });

  it('renders an empty state when no rows are returned', async () => {
    vi.spyOn(actionHistoryApi, 'listActionHistory').mockResolvedValue({
      data: [],
      total: 0,
    });

    renderPage();

    await waitFor(() =>
      expect(screen.getByText('No action executions')).toBeInTheDocument(),
    );
  });

  it('renders an empty state when the endpoint is unavailable (degraded mode)', async () => {
    // Hook short-circuits 404 to {data: []} — exercise the fallback path.
    vi.spyOn(actionHistoryApi, 'listActionHistory').mockRejectedValue(
      new ApiRequestError({
        statusCode: 404,
        errorCode: 'NOT_FOUND',
        errorName: 'ActionHistoryUnavailable',
        errorInstanceId: 'test',
      }),
    );

    renderPage();

    await waitFor(() =>
      expect(screen.getByText('No action executions')).toBeInTheDocument(),
    );
  });
});
