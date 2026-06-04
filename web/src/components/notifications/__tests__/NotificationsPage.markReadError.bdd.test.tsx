import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { NotificationsPage } from '../NotificationsPage';
import { Toaster } from '../../common/Toaster';
import { useToastStore } from '../../../stores/toastStore';
import * as notificationsApi from '../../../api/notifications';
import type { Notification } from '../../../api/notifications';

// BDD: when marking a notification (or the whole scope) as read fails on the
// wire, the page must surface a user-visible error toast. Previously both
// `handleMarkAll` and `handleRowClick` called `.mutate(...)` with no onError,
// and the hooks themselves only wired onSuccess — so a 5xx/timeout left the
// user believing the action succeeded (silent failure). These scenarios lock
// the per-call onError → pushToast contract, and assert the success paths stay
// toast-free.

const unreadNotifications: Notification[] = [
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
    read: false,
    createdAt: '2026-05-01T09:00:00Z',
  },
];

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/notifications']}>
        <NotificationsPage />
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: NotificationsPage surfaces mark-read failures', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    // Reset the global toast queue between scenarios so a leftover toast from a
    // prior test cannot satisfy a later assertion.
    useToastStore.getState().clear();
    vi.spyOn(notificationsApi, 'listNotifications').mockResolvedValue({
      data: unreadNotifications,
    });
  });

  it('Given mark-all fails, When the user clicks "Mark all read", Then an error toast appears', async () => {
    const user = userEvent.setup();
    const markAllSpy = vi
      .spyOn(notificationsApi, 'markAllNotificationsRead')
      .mockRejectedValue(new Error('network down'));

    renderPage();

    const btn = await screen.findByTestId('notifications-mark-all-btn');
    await user.click(btn);

    const toast = await screen.findByTestId('toast');
    expect(toast).toBeInTheDocument();
    expect(toast).toHaveTextContent(/network down/i);
    // Error toasts interrupt the screen reader immediately.
    expect(toast).toHaveAttribute('aria-live', 'assertive');
    expect(markAllSpy).toHaveBeenCalledTimes(1);
  });

  it('Given marking one notification read fails, When the user clicks a row, Then an error toast appears', async () => {
    const user = userEvent.setup();
    const markReadSpy = vi
      .spyOn(notificationsApi, 'markNotificationRead')
      .mockRejectedValue(new Error('boom'));

    renderPage();

    const row = await screen.findByTestId('notifications-row-n1');
    await user.click(row);

    const toast = await screen.findByTestId('toast');
    expect(toast).toBeInTheDocument();
    expect(toast).toHaveTextContent(/boom/i);
    expect(markReadSpy).toHaveBeenCalledTimes(1);
    expect(markReadSpy).toHaveBeenCalledWith('n1');
  });

  it('Given mark-all succeeds, When the user clicks "Mark all read", Then no error toast appears', async () => {
    const user = userEvent.setup();
    vi.spyOn(notificationsApi, 'markAllNotificationsRead').mockResolvedValue({
      updated: 2,
    });

    renderPage();

    const btn = await screen.findByTestId('notifications-mark-all-btn');
    await user.click(btn);

    await waitFor(() => {
      expect(
        notificationsApi.markAllNotificationsRead,
      ).toHaveBeenCalledTimes(1);
    });
    // Any erroneously-queued toast would have flushed by the time the mutation
    // settled above; assert none is present.
    expect(screen.queryByTestId('toast')).not.toBeInTheDocument();
  });

  it('Given marking one notification read succeeds, When the user clicks a row, Then no error toast appears', async () => {
    const user = userEvent.setup();
    vi.spyOn(notificationsApi, 'markNotificationRead').mockResolvedValue(
      undefined,
    );

    renderPage();

    const row = await screen.findByTestId('notifications-row-n1');
    await user.click(row);

    await waitFor(() => {
      expect(notificationsApi.markNotificationRead).toHaveBeenCalledWith('n1');
    });
    expect(screen.queryByTestId('toast')).not.toBeInTheDocument();
  });
});
