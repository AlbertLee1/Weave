import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ProposalsPage } from '../ProposalsPage';
import * as proposalsApi from '../../../api/proposals';

// BDD: a11y focus management for the self-drawn merge-confirm dialog
// (MergeConfirmDialog). The dialog is NOT the shared common/Modal, so it must
// implement its own standard focus contract — mirroring VertexShareLinkPanel
// (#229): on open focus moves inside the dialog, Tab / Shift+Tab cycle within
// it (degrades safely), Escape invokes the existing cancel callback, and on
// close focus returns to the element that opened it (the Merge button).
//
// These scenarios drive the page end-to-end: list an approved proposal, select
// it, open the merge dialog from the Merge button, then assert keyboard focus
// behaviour. The merge business logic, the typed-confirm gate, the visuals and
// the input's existing autoFocus are all left untouched.

const APPROVED = {
  id: 'prop-1',
  branchId: 'branch-xyz',
  ontologyRid: 'ri.ontology.main.ontology.default',
  title: 'Rename customer fields',
  description: 'Tidy up the customer object type',
  status: 'approved' as const,
  author: 'alice@test',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-02T00:00:00Z',
};

function mockApi() {
  vi.spyOn(proposalsApi, 'listProposals').mockResolvedValue({
    data: [APPROVED],
  });
  vi.spyOn(proposalsApi, 'getProposal').mockResolvedValue({
    ...APPROVED,
    reviews: [],
  });
  vi.spyOn(proposalsApi, 'getBranchDiff').mockResolvedValue({ data: [] });
  vi.spyOn(proposalsApi, 'getBranchBreakingChanges').mockResolvedValue({
    branchId: APPROVED.branchId,
    changes: [],
  });
}

function renderPage(initial = '/proposals/default') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/proposals/:ontology" element={<ProposalsPage />} />
          <Route path="/proposals" element={<ProposalsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// Open the merge dialog by selecting the approved proposal and clicking Merge.
// Returns the Merge button so scenarios can assert focus restoration to it.
async function openMergeDialog(user: ReturnType<typeof userEvent.setup>) {
  const row = await screen.findByTestId('proposals-row');
  await user.click(row);

  const mergeBtn = await screen.findByTestId('proposals-merge-btn');
  mergeBtn.focus();
  expect(mergeBtn).toHaveFocus();
  await user.click(mergeBtn);

  const dialog = await screen.findByTestId('proposals-merge-dialog');
  return { dialog, mergeBtn };
}

describe('BDD: ProposalsPage merge-confirm dialog focus management (a11y)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    mockApi();
  });

  it('Given the merge dialog is opened, Then focus moves inside the dialog', async () => {
    const user = userEvent.setup();
    renderPage();

    const { dialog } = await openMergeDialog(user);

    // Focus must land inside the dialog — keyboard users should never remain
    // on the page behind the modal overlay.
    expect(dialog.contains(document.activeElement)).toBe(true);
  });

  it('Given focus is inside the dialog, When Tab/Shift+Tab cycle, Then focus never escapes the dialog', async () => {
    const user = userEvent.setup();
    renderPage();

    const { dialog } = await openMergeDialog(user);

    // Tab repeatedly — focus should stay trapped within the dialog subtree.
    for (let i = 0; i < 6; i += 1) {
      await user.tab();
      expect(dialog.contains(document.activeElement)).toBe(true);
    }

    // Shift+Tab repeatedly — same guarantee in the reverse direction.
    for (let i = 0; i < 6; i += 1) {
      await user.tab({ shift: true });
      expect(dialog.contains(document.activeElement)).toBe(true);
    }
  });

  it('Given the dialog is open, When Escape is pressed, Then the dialog closes (cancel callback) and focus returns to the Merge button', async () => {
    const user = userEvent.setup();
    renderPage();

    const { mergeBtn } = await openMergeDialog(user);

    await user.keyboard('{Escape}');

    // Escape invokes the existing cancel path: the dialog unmounts.
    expect(screen.queryByTestId('proposals-merge-dialog')).not.toBeInTheDocument();
    // Focus is restored to the trigger so keyboard users are not stranded.
    expect(mergeBtn).toHaveFocus();
  });

  it('Given the dialog is open, When the Cancel button is clicked, Then focus returns to the Merge button', async () => {
    const user = userEvent.setup();
    renderPage();

    const { dialog, mergeBtn } = await openMergeDialog(user);

    await user.click(
      within(dialog).getByTestId('proposals-merge-dialog-cancel-btn'),
    );

    expect(
      screen.queryByTestId('proposals-merge-dialog'),
    ).not.toBeInTheDocument();
    expect(mergeBtn).toHaveFocus();
  });
});
