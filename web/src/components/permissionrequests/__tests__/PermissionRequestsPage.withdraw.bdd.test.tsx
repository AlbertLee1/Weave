import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { PermissionRequestsPage } from '../PermissionRequestsPage';
import * as api from '../../../api/permissionRequests';
import type { PermissionRequest } from '../../../api/permissionRequests';

const myPending: PermissionRequest = {
  id: 'p-9',
  targetRid: 'ri.ontology.main.object.alpha',
  requestedBy: 'user:me',
  reason: 'temporary access',
  status: 'PENDING',
  createdAt: '2026-05-01T12:00:00Z',
  updatedAt: '2026-05-01T12:00:00Z',
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

describe('BDD: PermissionRequestsPage withdraw own pending request (US-339 soft cancel)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('Given the "Only my requests" view, When Withdraw is clicked, Then cancel (DELETE) is called for that request', async () => {
    vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [myPending],
      total: 1,
      limit: 50,
      offset: 0,
    });
    const cancelSpy = vi
      .spyOn(api, 'cancelPermissionRequest')
      .mockResolvedValue();

    renderPage();

    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );
    // Scope to the caller's own requests so withdraw is unambiguously valid.
    fireEvent.click(screen.getByTestId('permission-requests-mine-toggle'));

    const withdraw = await screen.findByTestId('permission-request-withdraw-btn');
    fireEvent.click(withdraw);

    await waitFor(() => expect(cancelSpy).toHaveBeenCalledWith('p-9'));
  });

  it('Given the approver browses all requests (mine off), Then no Withdraw button is shown', async () => {
    vi.spyOn(api, 'listPermissionRequests').mockResolvedValue({
      requests: [myPending],
      total: 1,
      limit: 50,
      offset: 0,
    });

    renderPage();

    await waitFor(() =>
      expect(screen.getByTestId('permission-request-card')).toBeInTheDocument(),
    );
    // mineOnly defaults off → approver context → withdraw hidden (would 403).
    expect(screen.getByTestId('permission-request-approve-btn')).toBeInTheDocument();
    expect(screen.queryByTestId('permission-request-withdraw-btn')).toBeNull();
  });
});
