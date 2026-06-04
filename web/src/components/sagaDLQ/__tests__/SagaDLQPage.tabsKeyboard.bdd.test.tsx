import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { SagaDLQPage } from '../SagaDLQPage';
import * as sagaDLQApi from '../../../api/sagaDLQ';

function renderPage(initial = '/admin/default/saga-dlq') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route path="/admin/:ontology/saga-dlq" element={<SagaDLQPage />} />
          <Route path="/admin/saga-dlq" element={<SagaDLQPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: SagaDLQPage status-filter tablist keyboard navigation (WAI-ARIA tabs)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(sagaDLQApi, 'listSagaDLQ').mockResolvedValue({ entries: [] });
  });

  it('Given the Pending tab is focused, When ArrowRight is pressed, Then focus and selection move to Resolved, Dropped, and wrap back to Pending', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const resolvedTab = screen.getByRole('tab', { name: /resolved/i });
    const droppedTab = screen.getByRole('tab', { name: /dropped/i });

    pendingTab.focus();
    expect(pendingTab).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(resolvedTab).toHaveFocus();
    expect(resolvedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowRight}');
    expect(droppedTab).toHaveFocus();
    expect(droppedTab).toHaveAttribute('aria-selected', 'true');

    // Wrap-around from the last tab back to the first.
    await user.keyboard('{ArrowRight}');
    expect(pendingTab).toHaveFocus();
    expect(pendingTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the Pending tab is focused, When ArrowLeft is pressed, Then focus and selection wrap to the last tab (Dropped) and move backwards', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const resolvedTab = screen.getByRole('tab', { name: /resolved/i });
    const droppedTab = screen.getByRole('tab', { name: /dropped/i });

    pendingTab.focus();

    // Wrap-around from the first tab back to the last.
    await user.keyboard('{ArrowLeft}');
    expect(droppedTab).toHaveFocus();
    expect(droppedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowLeft}');
    expect(resolvedTab).toHaveFocus();
    expect(resolvedTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given a tab is focused, When ArrowDown/ArrowUp are pressed, Then they mirror ArrowRight/ArrowLeft', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const resolvedTab = screen.getByRole('tab', { name: /resolved/i });

    pendingTab.focus();

    await user.keyboard('{ArrowDown}');
    expect(resolvedTab).toHaveFocus();
    expect(resolvedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(pendingTab).toHaveFocus();
    expect(pendingTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given any tab is focused, When Home/End are pressed, Then focus and selection jump to the first/last tab', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const droppedTab = screen.getByRole('tab', { name: /dropped/i });

    pendingTab.focus();

    await user.keyboard('{End}');
    expect(droppedTab).toHaveFocus();
    expect(droppedTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(pendingTab).toHaveFocus();
    expect(pendingTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the tablist follows the roving tabindex pattern, Then only the selected tab is in the tab order and mouse clicks still work', async () => {
    const user = userEvent.setup();
    renderPage();

    const pendingTab = await screen.findByRole('tab', { name: /pending/i });
    const resolvedTab = screen.getByRole('tab', { name: /resolved/i });
    const droppedTab = screen.getByRole('tab', { name: /dropped/i });

    // Pending is the default selection.
    expect(pendingTab).toHaveAttribute('tabindex', '0');
    expect(resolvedTab).toHaveAttribute('tabindex', '-1');
    expect(droppedTab).toHaveAttribute('tabindex', '-1');

    // Mouse click still works and updates roving tabindex + selection.
    await user.click(resolvedTab);
    expect(resolvedTab).toHaveAttribute('aria-selected', 'true');
    expect(resolvedTab).toHaveAttribute('tabindex', '0');
    expect(pendingTab).toHaveAttribute('tabindex', '-1');
    expect(droppedTab).toHaveAttribute('tabindex', '-1');
  });
});
