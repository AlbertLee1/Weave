import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ApprovalsPage } from '../ApprovalsPage';
import * as approvalsApi from '../../../api/approvals';
import type { ActionApproval } from '../../../api/approvals';

const pendingApproval: ActionApproval = {
  id: 'a-01',
  ontologyApiName: 'default',
  actionType: 'deleteAccount',
  parameters: { id: 'acct-1' },
  approvers: ['approver-1'],
  status: 'PENDING',
  requestedBy: 'alice',
  createdAt: '2026-04-18T12:00:00Z',
  updatedAt: '2026-04-18T12:00:00Z',
};

const approvedApproval: ActionApproval = {
  ...pendingApproval,
  id: 'a-02',
  status: 'APPROVED',
  reviewedBy: 'bob',
  reason: 'LGTM',
};

function renderPage(initial = '/approvals/default') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/approvals/:ontology" element={<ApprovalsPage />} />
          <Route path="/approvals" element={<ApprovalsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ApprovalsPage (US-243)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders pending approvals with parameter details and action buttons', async () => {
    vi.spyOn(approvalsApi, 'listApprovals').mockResolvedValue({
      data: [pendingApproval],
    });

    renderPage();

    await waitFor(() => expect(screen.getByTestId('approval-card')).toBeInTheDocument());
    expect(screen.getByText('deleteAccount')).toBeInTheDocument();
    expect(screen.getByTestId('approval-parameters').textContent).toContain('acct-1');
    expect(screen.getByTestId('approval-approve-btn')).toBeInTheDocument();
    expect(screen.getByTestId('approval-reject-btn')).toBeInTheDocument();
  });

  it('opens the approve modal, sends reason, and refetches on success', async () => {
    const listSpy = vi
      .spyOn(approvalsApi, 'listApprovals')
      .mockResolvedValueOnce({ data: [pendingApproval] })
      .mockResolvedValueOnce({ data: [] });
    const approveSpy = vi
      .spyOn(approvalsApi, 'approveAction')
      .mockResolvedValue({ approvalId: pendingApproval.id, status: 'APPROVED' });

    renderPage();

    fireEvent.click(await screen.findByTestId('approval-approve-btn'));
    const modal = await screen.findByTestId('modal-overlay');
    const textarea = within(modal).getByTestId('review-reason-input');
    fireEvent.change(textarea, { target: { value: 'LGTM' } });
    fireEvent.click(within(modal).getByTestId('review-submit'));

    await waitFor(() =>
      expect(approveSpy).toHaveBeenCalledWith('default', pendingApproval.id, 'LGTM'),
    );
    await waitFor(() => expect(listSpy).toHaveBeenCalledTimes(2));
  });

  it('sends undefined reason when the input is left blank', async () => {
    vi.spyOn(approvalsApi, 'listApprovals').mockResolvedValue({ data: [pendingApproval] });
    const rejectSpy = vi
      .spyOn(approvalsApi, 'rejectAction')
      .mockResolvedValue({ approvalId: pendingApproval.id, status: 'REJECTED' });

    renderPage();

    fireEvent.click(await screen.findByTestId('approval-reject-btn'));
    const modal = await screen.findByTestId('modal-overlay');
    fireEvent.click(within(modal).getByTestId('review-submit'));

    await waitFor(() =>
      expect(rejectSpy).toHaveBeenCalledWith('default', pendingApproval.id, undefined),
    );
  });

  it('shows empty state when no rows are returned', async () => {
    vi.spyOn(approvalsApi, 'listApprovals').mockResolvedValue({ data: [] });

    renderPage();

    expect(await screen.findByText(/no approvals match/i)).toBeInTheDocument();
  });

  it('switching status filter re-queries with the new status', async () => {
    const listSpy = vi.spyOn(approvalsApi, 'listApprovals').mockImplementation(
      async (_o, params) => ({
        data: params?.status === 'APPROVED' ? [approvedApproval] : [pendingApproval],
      }),
    );

    renderPage();

    await waitFor(() =>
      expect(listSpy).toHaveBeenCalledWith(
        'default',
        expect.objectContaining({ status: 'PENDING' }),
      ),
    );

    fireEvent.click(screen.getByRole('tab', { name: /approved/i }));

    await waitFor(() =>
      expect(listSpy).toHaveBeenCalledWith(
        'default',
        expect.objectContaining({ status: 'APPROVED' }),
      ),
    );
  });

  it('surfaces a review error without closing the modal', async () => {
    vi.spyOn(approvalsApi, 'listApprovals').mockResolvedValue({ data: [pendingApproval] });
    vi.spyOn(approvalsApi, 'approveAction').mockRejectedValue(
      new Error('forbidden'),
    );

    renderPage();

    fireEvent.click(await screen.findByTestId('approval-approve-btn'));
    const modal = await screen.findByTestId('modal-overlay');
    fireEvent.click(within(modal).getByTestId('review-submit'));

    await waitFor(() =>
      expect(within(modal).getByRole('alert').textContent).toContain('forbidden'),
    );
  });
});
