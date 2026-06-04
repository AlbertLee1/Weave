import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { NotificationsPage } from '../NotificationsPage';
import * as notificationsApi from '../../../api/notifications';
import type { Notification } from '../../../api/notifications';

// BDD: WAI-ARIA keyboard navigation for the NotificationsPage filters.
//
// The page exposes two ARIA widgets that previously had role + aria state but
// zero keyboard support (click-only):
//   1. the type-filter `role="tablist"` (All / Mentions / Watches / Approvals
//      / System) — WAI-ARIA tabs pattern (Arrow + Home/End + roving tabindex,
//      automatic activation).
//   2. the read-status `role="radiogroup"` (All / Unread only) — WAI-ARIA radio
//      pattern (Arrow moves focus *and* selects, roving tabindex, Space/Enter).
//
// These scenarios lock the keyboard contract so it cannot regress, and assert
// the existing click behaviour (tab switch / radio selection) is preserved.

const notifications: Notification[] = [
  {
    id: 'n1',
    userId: 'u:alice',
    title: 'You were mentioned',
    body: 'In a comment',
    type: 'mention',
    read: false,
    createdAt: '2026-05-01T10:00:00Z',
  },
  {
    id: 'n2',
    userId: 'u:alice',
    title: 'Watched object changed',
    body: 'Object updated',
    type: 'watch',
    read: true,
    createdAt: '2026-05-01T09:00:00Z',
  },
];

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/notifications']}>
        <NotificationsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

/** Returns the five type-filter tabs in DOM order. */
async function getTypeTabs() {
  const tablist = await screen.findByRole('tablist', {
    name: 'Notification type filter',
  });
  return {
    tablist,
    all: screen.getByTestId('notifications-tab-all') as HTMLButtonElement,
    mention: screen.getByTestId(
      'notifications-tab-mention',
    ) as HTMLButtonElement,
    watch: screen.getByTestId('notifications-tab-watch') as HTMLButtonElement,
    approval: screen.getByTestId(
      'notifications-tab-approval',
    ) as HTMLButtonElement,
    system: screen.getByTestId('notifications-tab-system') as HTMLButtonElement,
  };
}

/** Returns the two read-status radios in DOM order: [All, Unread only]. */
async function getReadRadios() {
  const group = await screen.findByRole('radiogroup', {
    name: 'Read status filter',
  });
  return {
    group,
    all: screen.getByTestId('notifications-read-all') as HTMLButtonElement,
    unread: screen.getByTestId('notifications-read-unread') as HTMLButtonElement,
  };
}

describe('BDD: NotificationsPage type tablist keyboard navigation', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(notificationsApi, 'listNotifications').mockResolvedValue({
      data: notifications,
    });
  });

  it('Given the selected tab is focused, When ArrowRight is pressed, Then focus and selection move to the next tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, mention } = await getTypeTabs();

    // Initially "All" is selected (roving tabindex 0); the others are -1.
    expect(all).toHaveAttribute('aria-selected', 'true');
    expect(all).toHaveAttribute('tabindex', '0');
    expect(mention).toHaveAttribute('tabindex', '-1');

    all.focus();
    expect(all).toHaveFocus();

    await user.keyboard('{ArrowRight}');

    expect(mention).toHaveFocus();
    expect(mention).toHaveAttribute('aria-selected', 'true');
    expect(mention).toHaveAttribute('tabindex', '0');
    expect(all).toHaveAttribute('aria-selected', 'false');
    expect(all).toHaveAttribute('tabindex', '-1');
  });

  it('Given the first tab is focused, When ArrowLeft is pressed, Then it wraps to the last tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, system } = await getTypeTabs();

    all.focus();
    await user.keyboard('{ArrowLeft}');

    expect(system).toHaveFocus();
    expect(system).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the last tab is focused, When ArrowRight is pressed, Then it wraps to the first tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, system } = await getTypeTabs();

    all.focus();
    await user.keyboard('{ArrowLeft}'); // wrap to System (last)
    expect(system).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');
  });

  it('ArrowDown/ArrowUp mirror ArrowRight/ArrowLeft', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, mention } = await getTypeTabs();

    all.focus();
    await user.keyboard('{ArrowDown}');
    expect(mention).toHaveFocus();
    expect(mention).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');
  });

  it('Home jumps to the first tab and End jumps to the last tab', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, mention, system } = await getTypeTabs();

    all.focus();
    await user.keyboard('{ArrowRight}');
    expect(mention).toHaveFocus();

    await user.keyboard('{End}');
    expect(system).toHaveFocus();
    expect(system).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-selected', 'true');
  });

  it('mouse click still selects a tab (existing behavior preserved)', async () => {
    const user = userEvent.setup();
    renderPage();
    const { mention } = await getTypeTabs();

    await user.click(mention);

    expect(mention).toHaveAttribute('aria-selected', 'true');
    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toHaveAttribute(
        'data-active-tab',
        'mention',
      );
    });
  });
});

describe('BDD: NotificationsPage read-status radiogroup keyboard navigation', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(notificationsApi, 'listNotifications').mockResolvedValue({
      data: notifications,
    });
  });

  it('Given the checked radio is focused, When ArrowDown is pressed, Then focus and selection move to the next radio', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, unread } = await getReadRadios();

    // Initially "All" is checked (roving tabindex 0); "Unread only" is -1.
    expect(all).toHaveAttribute('aria-checked', 'true');
    expect(all).toHaveAttribute('tabindex', '0');
    expect(unread).toHaveAttribute('tabindex', '-1');

    all.focus();
    expect(all).toHaveFocus();

    await user.keyboard('{ArrowDown}');

    expect(unread).toHaveFocus();
    expect(unread).toHaveAttribute('aria-checked', 'true');
    expect(unread).toHaveAttribute('tabindex', '0');
    expect(all).toHaveAttribute('aria-checked', 'false');
    expect(all).toHaveAttribute('tabindex', '-1');

    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toHaveAttribute(
        'data-read-filter',
        'unread',
      );
    });
  });

  it('ArrowRight mirrors ArrowDown and wraps; ArrowUp/ArrowLeft move backwards and wrap', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, unread } = await getReadRadios();

    all.focus();
    await user.keyboard('{ArrowRight}');
    expect(unread).toHaveFocus();
    expect(unread).toHaveAttribute('aria-checked', 'true');

    // Wrap forward from last -> first.
    await user.keyboard('{ArrowRight}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-checked', 'true');

    // Wrap backward from first -> last.
    await user.keyboard('{ArrowUp}');
    expect(unread).toHaveFocus();
    expect(unread).toHaveAttribute('aria-checked', 'true');

    await user.keyboard('{ArrowLeft}');
    expect(all).toHaveFocus();
    expect(all).toHaveAttribute('aria-checked', 'true');
  });

  it('Space/Enter selects the focused radio', async () => {
    const user = userEvent.setup();
    renderPage();
    const { all, unread } = await getReadRadios();

    // Focus the (initially unchecked) "Unread only" radio without selecting it.
    unread.focus();
    await user.keyboard(' ');

    expect(unread).toHaveAttribute('aria-checked', 'true');
    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toHaveAttribute(
        'data-read-filter',
        'unread',
      );
    });

    all.focus();
    await user.keyboard('{Enter}');
    expect(all).toHaveAttribute('aria-checked', 'true');
    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toHaveAttribute(
        'data-read-filter',
        'all',
      );
    });
  });

  it('mouse click still selects a read-status radio (existing behavior preserved)', async () => {
    const user = userEvent.setup();
    renderPage();
    const { unread } = await getReadRadios();

    await user.click(unread);

    expect(unread).toHaveAttribute('aria-checked', 'true');
    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toHaveAttribute(
        'data-read-filter',
        'unread',
      );
    });
  });
});
