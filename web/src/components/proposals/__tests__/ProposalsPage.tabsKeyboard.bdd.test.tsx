import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ProposalsPage } from '../ProposalsPage';
import * as proposalsApi from '../../../api/proposals';

// BDD: WAI-ARIA tabs keyboard contract for the ProposalsPage status-filter
// tablist. The tablist exposes role="tablist" / role="tab" + aria-selected;
// these scenarios assert the standard arrow / Home / End keyboard navigation
// with a roving tabindex (the same pattern shipped for AggregationPage and
// ApprovalsPage). The filter order is: All, Open, Approved, Rejected, Merged.

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

describe('BDD: ProposalsPage status-filter tablist keyboard navigation (WAI-ARIA tabs)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(proposalsApi, 'listProposals').mockResolvedValue({ data: [] });
  });

  it('Given the All tab is focused, When ArrowRight is pressed, Then focus + selection move forward and wrap back to All', async () => {
    const user = userEvent.setup();
    renderPage();

    const allTab = await screen.findByRole('tab', { name: /^all$/i });
    const openTab = screen.getByRole('tab', { name: /^open$/i });
    const approvedTab = screen.getByRole('tab', { name: /^approved$/i });
    const rejectedTab = screen.getByRole('tab', { name: /^rejected$/i });
    const mergedTab = screen.getByRole('tab', { name: /^merged$/i });

    allTab.focus();
    expect(allTab).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(openTab).toHaveFocus();
    expect(openTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(approvedTab).toHaveFocus();
    expect(approvedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(rejectedTab).toHaveFocus();
    expect(rejectedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(mergedTab).toHaveFocus();
    expect(mergedTab).toHaveAttribute('aria-selected', 'true');

    // Wrap-around from the last tab back to the first.
    await user.keyboard('{ArrowRight}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the All tab is focused, When ArrowLeft is pressed, Then focus + selection wrap to the last tab (Merged) and move backwards', async () => {
    const user = userEvent.setup();
    renderPage();

    const allTab = await screen.findByRole('tab', { name: /^all$/i });
    const rejectedTab = screen.getByRole('tab', { name: /^rejected$/i });
    const mergedTab = screen.getByRole('tab', { name: /^merged$/i });

    allTab.focus();

    // Wrap-around from the first tab back to the last.
    await user.keyboard('{ArrowLeft}');
    expect(mergedTab).toHaveFocus();
    expect(mergedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowLeft}');
    expect(rejectedTab).toHaveFocus();
    expect(rejectedTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given a tab is focused, When ArrowDown/ArrowUp are pressed, Then they mirror ArrowRight/ArrowLeft', async () => {
    const user = userEvent.setup();
    renderPage();

    const allTab = await screen.findByRole('tab', { name: /^all$/i });
    const openTab = screen.getByRole('tab', { name: /^open$/i });

    allTab.focus();

    await user.keyboard('{ArrowDown}');
    expect(openTab).toHaveFocus();
    expect(openTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given any tab is focused, When Home/End are pressed, Then focus + selection jump to the first/last tab', async () => {
    const user = userEvent.setup();
    renderPage();

    const allTab = await screen.findByRole('tab', { name: /^all$/i });
    const mergedTab = screen.getByRole('tab', { name: /^merged$/i });

    allTab.focus();

    await user.keyboard('{End}');
    expect(mergedTab).toHaveFocus();
    expect(mergedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the tablist follows the roving tabindex pattern, Then only the selected tab is in the tab order and mouse clicks still work', async () => {
    const user = userEvent.setup();
    renderPage();

    const allTab = await screen.findByRole('tab', { name: /^all$/i });
    const openTab = screen.getByRole('tab', { name: /^open$/i });
    const approvedTab = screen.getByRole('tab', { name: /^approved$/i });
    const rejectedTab = screen.getByRole('tab', { name: /^rejected$/i });
    const mergedTab = screen.getByRole('tab', { name: /^merged$/i });

    // "All" is the default selection.
    expect(allTab).toHaveAttribute('tabindex', '0');
    expect(openTab).toHaveAttribute('tabindex', '-1');
    expect(approvedTab).toHaveAttribute('tabindex', '-1');
    expect(rejectedTab).toHaveAttribute('tabindex', '-1');
    expect(mergedTab).toHaveAttribute('tabindex', '-1');

    // Mouse click still works and updates roving tabindex + selection.
    await user.click(approvedTab);
    expect(approvedTab).toHaveAttribute('aria-selected', 'true');
    expect(approvedTab).toHaveAttribute('tabindex', '0');
    expect(allTab).toHaveAttribute('tabindex', '-1');
    expect(openTab).toHaveAttribute('tabindex', '-1');
    expect(rejectedTab).toHaveAttribute('tabindex', '-1');
    expect(mergedTab).toHaveAttribute('tabindex', '-1');
  });
});
