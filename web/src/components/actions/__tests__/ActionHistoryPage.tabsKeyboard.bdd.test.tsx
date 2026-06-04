import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ActionHistoryPage } from '../ActionHistoryPage';
import * as actionHistoryApi from '../../../api/actionHistory';
import * as ontologiesApi from '../../../api/ontologies';
import type { ActionHistoryEntry } from '../../../api/actionHistory';
import type { ActionType } from '../../../api/types';

// BDD: WAI-ARIA keyboard navigation for the ActionHistoryPage status-filter
// tablist. The tablist has role="tablist" + role="tab" + aria-selected; this
// scenario locks the keyboard contract (Arrow keys + Home/End + roving
// tabindex with automatic activation) so it is not regressed.

const successEntry: ActionHistoryEntry = {
  id: 1,
  actionTypeRid: 'rid:at:create',
  userId: 'user:alice',
  parameters: { name: 'Alice' },
  edits: [{ type: 'createObject' }],
  status: 'SUCCESS',
  createdAt: '2026-04-28T14:00:00Z',
};

const actionTypes: ActionType[] = [
  {
    rid: 'rid:at:create',
    apiName: 'createEmployee',
    displayName: 'Create Employee',
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

/** Returns the three status-filter tabs in DOM order: [All, Success, Failed]. */
async function getStatusTabs() {
  const tablist = await screen.findByRole('tablist', { name: 'Status filter' });
  return {
    tablist,
    all: screen.getByTestId('filter-status-all') as HTMLButtonElement,
    success: screen.getByTestId('filter-status-success') as HTMLButtonElement,
    failed: screen.getByTestId('filter-status-failed') as HTMLButtonElement,
  };
}

describe('BDD: ActionHistoryPage status tablist keyboard navigation', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(ontologiesApi, 'listActionTypes').mockResolvedValue(actionTypes);
    vi.spyOn(actionHistoryApi, 'listActionHistory').mockResolvedValue({
      data: [successEntry],
      total: 1,
    });
  });

  it('Given the selected tab is focused, When ArrowRight is pressed, Then focus and selection move to the next tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, success } = await getStatusTabs();

    // Initially "All" is selected (roving tabindex 0); the others are -1.
    expect(all).toHaveAttribute('aria-selected', 'true');
    expect(all).toHaveAttribute('tabindex', '0');
    expect(success).toHaveAttribute('tabindex', '-1');

    all.focus();
    expect(all).toHaveFocus();

    await user.keyboard('{ArrowRight}');

    expect(success).toHaveFocus();
    expect(success).toHaveAttribute('aria-selected', 'true');
    expect(success).toHaveAttribute('tabindex', '0');
    expect(all).toHaveAttribute('aria-selected', 'false');
    expect(all).toHaveAttribute('tabindex', '-1');
  });

  it('Given the first tab is focused, When ArrowLeft is pressed, Then it wraps to the last tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, failed } = await getStatusTabs();

    all.focus();
    await user.keyboard('{ArrowLeft}');

    expect(failed).toHaveFocus();
    expect(failed).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the last tab is focused, When ArrowRight is pressed, Then it wraps to the first tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, failed } = await getStatusTabs();

    // Navigate to the last tab first.
    all.focus();
    await user.keyboard('{ArrowLeft}'); // wrap to Failed (last)
    expect(failed).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');
  });

  it('ArrowDown/ArrowUp mirror ArrowRight/ArrowLeft', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, success } = await getStatusTabs();

    all.focus();
    await user.keyboard('{ArrowDown}');
    expect(success).toHaveFocus();
    expect(success).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');
  });

  it('Home jumps to the first tab and End jumps to the last tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, success, failed } = await getStatusTabs();

    // Move off the first tab.
    all.focus();
    await user.keyboard('{ArrowRight}');
    expect(success).toHaveFocus();

    await user.keyboard('{End}');
    expect(failed).toHaveFocus();
    expect(failed).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');
  });

  it('mouse click still selects a tab (existing behavior preserved)', async () => {
    const user = userEvent.setup();
    const listSpy = vi.spyOn(actionHistoryApi, 'listActionHistory');
    renderPage();
    const { failed } = await getStatusTabs();

    await user.click(failed);

    expect(failed).toHaveAttribute('aria-selected', 'true');
    // The list query re-runs with the FAILED status filter.
    await waitFor(() => {
      const lastCall = listSpy.mock.calls[listSpy.mock.calls.length - 1];
      expect(lastCall?.[1]).toMatchObject({ status: 'FAILED' });
    });
  });
});
