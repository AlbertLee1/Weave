import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { PermissionRequestsPage } from '../PermissionRequestsPage';
import * as api from '../../../api/permissionRequests';
import type { PermissionRequest } from '../../../api/permissionRequests';

const pending: PermissionRequest = {
  id: 'p-1',
  targetRid: 'ri.ontology.main.object.alpha',
  requestedBy: 'user:bob',
  reason: 'I need to view the alpha object',
  status: 'PENDING',
  createdAt: '2026-04-28T12:00:00Z',
  updatedAt: '2026-04-28T12:00:00Z',
};

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <PermissionRequestsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('PermissionRequestsPage (US-339)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders a pending row with approve and reject buttons', async () => {
    vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [pending],
      total: 1,
      limit: 50,
      offset: 0,
    });

    renderPage();

    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );
    expect(screen.getByText(pending.targetRid)).toBeInTheDocument();
    expect(screen.getByTestId('permission-request-approve-btn')).toBeInTheDocument();
    expect(screen.getByTestId('permission-request-reject-btn')).toBeInTheDocument();
  });

  it('opens the request-access dialog and submits a new request', async () => {
    const listSpy = vi
      .spyOn(api, 'listPermissionRequests')
      .mockResolvedValueOnce({ requests: [], total: 0, limit: 50, offset: 0 })
      .mockResolvedValueOnce({ requests: [pending], total: 1, limit: 50, offset: 0 });
    const createSpy = vi
      .spyOn(api, 'createPermissionRequest')
      .mockResolvedValue(pending);

    renderPage();

    fireEvent.click(await screen.findByTestId('permission-request-create'));
    const modal = await screen.findByTestId('modal-overlay');
    fireEvent.change(within(modal).getByTestId('permission-request-target-input'), {
      target: { value: 'ri.ontology.main.object.alpha' },
    });
    fireEvent.change(within(modal).getByTestId('permission-request-reason-input'), {
      target: { value: 'audit access' },
    });
    fireEvent.click(within(modal).getByTestId('permission-request-submit'));

    await waitFor(() =>
      expect(createSpy).toHaveBeenCalledWith('ri.ontology.main.object.alpha', 'audit access'),
    );
    await waitFor(() => expect(listSpy.mock.calls.length).toBeGreaterThanOrEqual(2));
  });

  it('blocks submit when the target RID is empty', async () => {
    vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [],
      total: 0,
      limit: 50,
      offset: 0,
    });
    const createSpy = vi.spyOn(api, 'createPermissionRequest');

    renderPage();
    fireEvent.click(await screen.findByTestId('permission-request-create'));
    const modal = await screen.findByTestId('modal-overlay');
    fireEvent.click(within(modal).getByTestId('permission-request-submit'));

    expect(createSpy).not.toHaveBeenCalled();
    expect(within(modal).getByText(/Target RID is required/i)).toBeInTheDocument();
  });

  it('approves a pending row through the review modal', async () => {
    vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [pending],
      total: 1,
      limit: 50,
      offset: 0,
    });
    const approveSpy = vi
      .spyOn(api, 'approvePermissionRequest')
      .mockResolvedValue({ ...pending, status: 'APPROVED', decidedBy: 'user:admin' });

    renderPage();

    fireEvent.click(await screen.findByTestId('permission-request-approve-btn'));
    const modal = await screen.findByTestId('modal-overlay');
    fireEvent.change(within(modal).getByTestId('permission-review-note-input'), {
      target: { value: 'looks legit' },
    });
    fireEvent.click(within(modal).getByTestId('permission-review-submit'));

    await waitFor(() =>
      expect(approveSpy).toHaveBeenCalledWith(pending.id, 'looks legit'),
    );
  });

  it('rejects a pending row through the review modal', async () => {
    vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [pending],
      total: 1,
      limit: 50,
      offset: 0,
    });
    const rejectSpy = vi
      .spyOn(api, 'rejectPermissionRequest')
      .mockResolvedValue({ ...pending, status: 'REJECTED', decidedBy: 'user:admin' });

    renderPage();

    fireEvent.click(await screen.findByTestId('permission-request-reject-btn'));
    const modal = await screen.findByTestId('modal-overlay');
    fireEvent.click(within(modal).getByTestId('permission-review-submit'));

    await waitFor(() =>
      expect(rejectSpy).toHaveBeenCalledWith(pending.id, undefined),
    );
  });

  it('renders the unavailable empty state when the API 404s', async () => {
    const { ApiRequestError } = await import('../../../api/client');
    vi.spyOn(api, 'listPermissionRequests').mockRejectedValue(
      new ApiRequestError({
        errorCode: 'NOT_FOUND',
        errorName: 'PermissionRequestsUnavailable',
        errorInstanceId: '',
        statusCode: 404,
      }),
    );

    renderPage();

    await waitFor(() =>
      expect(screen.getByText(/Permission Requests unavailable/i)).toBeInTheDocument(),
    );
  });
});
