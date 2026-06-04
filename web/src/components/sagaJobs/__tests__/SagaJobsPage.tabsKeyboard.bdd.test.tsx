import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { SagaJobsPage } from '../SagaJobsPage';
import * as sagaJobsApi from '../../../api/sagaJobs';
import * as sagaDLQApi from '../../../api/sagaDLQ';

function renderPage(initial = '/actions/default/jobs') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/actions/:ontology/jobs" element={<SagaJobsPage />} />
          <Route path="/actions/jobs" element={<SagaJobsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: SagaJobsPage status-filter tablist keyboard navigation (WAI-ARIA tabs)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(sagaJobsApi, 'listSagas').mockResolvedValue({ data: [] });
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({ entries: [] });
  });

  it('Given the All tab is focused, When ArrowRight is pressed, Then focus and selection move along the status tabs and wrap back to All', async () => {
    const user = userEvent.setup();
    renderPage();

    const tablist = await screen.findByRole('tablist', {
      name: /saga status filter/i,
    });
    const allTab = within(tablist).getByRole('tab', { name: /^all$/i });
    const runningTab = within(tablist).getByRole('tab', { name: /running/i });
    const failedTab = within(tablist).getByRole('tab', { name: /failed/i });

    allTab.focus();
    expect(allTab).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(runningTab).toHaveFocus();
    expect(runningTab).toHaveAttribute('aria-selected', 'true');

    // Step through the rest to the last tab (FAILED).
    await user.keyboard('{End}');
    expect(failedTab).toHaveFocus();
    expect(failedTab).toHaveAttribute('aria-selected', 'true');

    // Wrap-around from the last tab back to the first.
    await user.keyboard('{ArrowRight}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the All tab is focused, When ArrowLeft is pressed, Then focus and selection wrap to the last status tab', async () => {
    const user = userEvent.setup();
    renderPage();

    const tablist = await screen.findByRole('tablist', {
      name: /saga status filter/i,
    });
    const allTab = within(tablist).getByRole('tab', { name: /^all$/i });
    const failedTab = within(tablist).getByRole('tab', { name: /failed/i });

    allTab.focus();

    await user.keyboard('{ArrowLeft}');
    expect(failedTab).toHaveFocus();
    expect(failedTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given a status tab is focused, When ArrowDown/ArrowUp are pressed, Then they mirror ArrowRight/ArrowLeft', async () => {
    const user = userEvent.setup();
    renderPage();

    const tablist = await screen.findByRole('tablist', {
      name: /saga status filter/i,
    });
    const allTab = within(tablist).getByRole('tab', { name: /^all$/i });
    const runningTab = within(tablist).getByRole('tab', { name: /running/i });

    allTab.focus();

    await user.keyboard('{ArrowDown}');
    expect(runningTab).toHaveFocus();
    expect(runningTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given any status tab is focused, When Home/End are pressed, Then focus jumps to the first/last tab', async () => {
    const user = userEvent.setup();
    renderPage();

    const tablist = await screen.findByRole('tablist', {
      name: /saga status filter/i,
    });
    const allTab = within(tablist).getByRole('tab', { name: /^all$/i });
    const failedTab = within(tablist).getByRole('tab', { name: /failed/i });

    allTab.focus();

    await user.keyboard('{End}');
    expect(failedTab).toHaveFocus();
    expect(failedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(allTab).toHaveFocus();
    expect(allTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the status tablist follows the roving tabindex pattern, Then only the selected tab is tabbable and mouse clicks still work', async () => {
    const user = userEvent.setup();
    renderPage();

    const tablist = await screen.findByRole('tablist', {
      name: /saga status filter/i,
    });
    const allTab = within(tablist).getByRole('tab', { name: /^all$/i });
    const runningTab = within(tablist).getByRole('tab', { name: /running/i });

    // All is the default selection.
    expect(allTab).toHaveAttribute('tabindex', '0');
    expect(runningTab).toHaveAttribute('tabindex', '-1');

    // Mouse click still works and updates roving tabindex + selection.
    await user.click(runningTab);
    expect(runningTab).toHaveAttribute('aria-selected', 'true');
    expect(runningTab).toHaveAttribute('tabindex', '0');
    expect(allTab).toHaveAttribute('tabindex', '-1');
  });
});

describe('BDD: SagaJobsPage DLQ-filter tablist keyboard navigation (WAI-ARIA tabs)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(sagaJobsApi, 'listSagas').mockResolvedValue({ data: [] });
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({ entries: [] });
  });

  async function openDlqTablist(user: ReturnType<typeof userEvent.setup>) {
    renderPage();
    const openBtn = await screen.findByTestId('saga-jobs-open-dlq-btn');
    await user.click(openBtn);
    return screen.findByRole('tablist', { name: /dlq status filter/i });
  }

  it('Given the Pending tab is focused, When ArrowRight is pressed, Then focus and selection move to Resolved, Dropped, and wrap back', async () => {
    const user = userEvent.setup();
    const tablist = await openDlqTablist(user);

    const pendingTab = within(tablist).getByRole('tab', { name: /pending/i });
    const resolvedTab = within(tablist).getByRole('tab', { name: /resolved/i });
    const droppedTab = within(tablist).getByRole('tab', { name: /dropped/i });

    pendingTab.focus();
    expect(pendingTab).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(resolvedTab).toHaveFocus();
    expect(resolvedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(droppedTab).toHaveFocus();
    expect(droppedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(pendingTab).toHaveFocus();
    expect(pendingTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given any DLQ tab is focused, When Home/End are pressed, Then focus jumps to the first/last tab', async () => {
    const user = userEvent.setup();
    const tablist = await openDlqTablist(user);

    const pendingTab = within(tablist).getByRole('tab', { name: /pending/i });
    const droppedTab = within(tablist).getByRole('tab', { name: /dropped/i });

    pendingTab.focus();

    await user.keyboard('{End}');
    expect(droppedTab).toHaveFocus();
    expect(droppedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(pendingTab).toHaveFocus();
    expect(pendingTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the DLQ tablist follows the roving tabindex pattern, Then only the selected tab is tabbable', async () => {
    const user = userEvent.setup();
    const tablist = await openDlqTablist(user);

    const pendingTab = within(tablist).getByRole('tab', { name: /pending/i });
    const resolvedTab = within(tablist).getByRole('tab', { name: /resolved/i });
    const droppedTab = within(tablist).getByRole('tab', { name: /dropped/i });

    expect(pendingTab).toHaveAttribute('tabindex', '0');
    expect(resolvedTab).toHaveAttribute('tabindex', '-1');
    expect(droppedTab).toHaveAttribute('tabindex', '-1');
  });
});

describe('BDD: SagaJobsPage tablists are mutually independent', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(sagaJobsApi, 'listSagas').mockResolvedValue({ data: [] });
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({ entries: [] });
  });

  it("Given both tablists exist, When the DLQ tablist receives ArrowRight, Then the status tablist's selection is untouched", async () => {
    const user = userEvent.setup();
    renderPage();

    const statusTablist = await screen.findByRole('tablist', {
      name: /saga status filter/i,
    });
    const allTab = within(statusTablist).getByRole('tab', { name: /^all$/i });
    expect(allTab).toHaveAttribute('aria-selected', 'true');

    // Open the DLQ drawer and drive its tablist with the keyboard.
    await user.click(screen.getByTestId('saga-jobs-open-dlq-btn'));
    const dlqTablist = await screen.findByRole('tablist', {
      name: /dlq status filter/i,
    });
    const pendingTab = within(dlqTablist).getByRole('tab', { name: /pending/i });
    const resolvedTab = within(dlqTablist).getByRole('tab', {
      name: /resolved/i,
    });

    pendingTab.focus();
    await user.keyboard('{ArrowRight}');
    expect(resolvedTab).toHaveFocus();
    expect(resolvedTab).toHaveAttribute('aria-selected', 'true');

    // The status tablist must be unaffected by DLQ keyboard navigation.
    expect(allTab).toHaveAttribute('aria-selected', 'true');
    expect(
      within(statusTablist).getByRole('tab', { name: /running/i }),
    ).toHaveAttribute('aria-selected', 'false');
  });
});
