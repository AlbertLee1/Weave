import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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

describe('BDD: ApprovalsPage status-filter tablist keyboard navigation (WAI-ARIA tabs)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(approvalsApi, 'listApprovals').mockResolvedValue({
      data: [pendingApproval],
    });
  });

  it('Given the Pending tab is focused, When ArrowRight is pressed, Then focus and selection move to Approved, Rejected, All, and wrap back to Pending', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const approvedTab = screen.getByRole('tab', { name: /approved/i });
    const rejectedTab = screen.getByRole('tab', { name: /rejected/i });
    const allTab = screen.getByRole('tab', { name: /all/i });

    pendingTab.focus();
    expect(pendingTab).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(approvedTab).toHaveFocus();
    expect(approvedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(rejectedTab).toHaveFocus();
    expect(rejectedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');

    // Wrap-around from the last tab back to the first.
    await user.keyboard('{ArrowRight}');
    expect(pendingTab).toHaveFocus();
    expect(pendingTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the Pending tab is focused, When ArrowLeft is pressed, Then focus and selection wrap to the last tab (All) and move backwards', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const rejectedTab = screen.getByRole('tab', { name: /rejected/i });
    const allTab = screen.getByRole('tab', { name: /all/i });

    pendingTab.focus();

    // Wrap-around from the first tab back to the last.
    await user.keyboard('{ArrowLeft}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowLeft}');
    expect(rejectedTab).toHaveFocus();
    expect(rejectedTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given a tab is focused, When ArrowDown/ArrowUp are pressed, Then they mirror ArrowRight/ArrowLeft', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const approvedTab = screen.getByRole('tab', { name: /approved/i });

    pendingTab.focus();

    await user.keyboard('{ArrowDown}');
    expect(approvedTab).toHaveFocus();
    expect(approvedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(pendingTab).toHaveFocus();
    expect(pendingTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given any tab is focused, When Home/End are pressed, Then focus and selection jump to the first/last tab', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const allTab = screen.getByRole('tab', { name: /all/i });

    pendingTab.focus();

    await user.keyboard('{End}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(pendingTab).toHaveFocus();
    expect(pendingTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the tablist follows the roving tabindex pattern, Then only the selected tab is in the tab order and mouse clicks still work', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const approvedTab = screen.getByRole('tab', { name: /approved/i });
    const rejectedTab = screen.getByRole('tab', { name: /rejected/i });
    const allTab = screen.getByRole('tab', { name: /all/i });

    // Pending is the default selection.
    expect(pendingTab).toHaveAttribute('tabindex', '0');
    expect(approvedTab).toHaveAttribute('tabindex', '-1');
    expect(rejectedTab).toHaveAttribute('tabindex', '-1');
    expect(allTab).toHaveAttribute('tabindex', '-1');

    // Mouse click still works and updates roving tabindex + selection.
    await user.click(approvedTab);
    expect(approvedTab).toHaveAttribute('aria-selected', 'true');
    expect(approvedTab).toHaveAttribute('tabindex', '0');
    expect(pendingTab).toHaveAttribute('tabindex', '-1');
    expect(rejectedTab).toHaveAttribute('tabindex', '-1');
    expect(allTab).toHaveAttribute('tabindex', '-1');
  });
});
