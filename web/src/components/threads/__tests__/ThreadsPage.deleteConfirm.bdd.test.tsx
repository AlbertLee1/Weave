import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ThreadsPage } from '../ThreadsPage';
import * as aipApi from '../../../api/aip';
import type { AIPThread } from '../../../api/aip';

// BDD: deleting an AIP thread must go through the styled shared Modal
// confirmation dialog — NOT the native, unstyleable window.confirm.
//
// Given the Threads page with two threads,
// When the user clicks a thread's "Delete" button,
// Then window.confirm is NOT invoked and a styled Modal confirmation appears.
//   - Clicking "Cancel" closes the Modal and deletes nothing.
//   - Only after clicking the Modal's destructive "Delete" button is the
//     delete API called and the thread removed from the list.

const threadA: AIPThread = {
  id: 'thr_aaa',
  title: 'Mock greeting',
  provider: 'mock',
  model: 'weave-mock-llm-v1',
  systemPrompt: '',
  createdBy: 'user-1',
  createdAt: '2026-04-28T08:00:00Z',
  updatedAt: '2026-04-28T08:00:00Z',
};

const threadB: AIPThread = {
  id: 'thr_bbb',
  title: 'OpenAI exploration',
  provider: 'openai',
  model: 'gpt-4o-mini',
  systemPrompt: 'You are a helpful assistant.',
  createdBy: 'user-1',
  createdAt: '2026-04-28T09:00:00Z',
  updatedAt: '2026-04-28T09:00:00Z',
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/threads']}>
        <Routes>
          <Route path="/threads" element={<ThreadsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ThreadsPage delete confirmation (styled Modal, no window.confirm)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('opens a styled Modal instead of calling window.confirm when Delete is clicked', async () => {
    const user = userEvent.setup();
    // Spy on window.confirm so we can prove it is never invoked.
    const confirmSpy = vi
      .spyOn(window, 'confirm')
      .mockReturnValue(true);
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({
      threads: [threadA, threadB],
    });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({ messages: [] });
    const deleteSpy = vi.spyOn(aipApi, 'deleteThread').mockResolvedValue();

    renderPage();

    const items = await screen.findAllByTestId('thread-list-item');
    expect(items).toHaveLength(2);

    // When: click the second thread's Delete button.
    const deleteBtn = within(items[1]).getByTestId('thread-delete-btn');
    await user.click(deleteBtn);

    // Then: native confirm is never used, and a styled Modal is shown.
    expect(confirmSpy).not.toHaveBeenCalled();
    const overlay = await screen.findByTestId('modal-overlay');
    expect(
      within(overlay).getByText(/cannot be undone/i),
    ).toBeInTheDocument();
    // The destructive confirm action is present in the dialog.
    expect(
      within(overlay).getByTestId('confirm-delete-thread'),
    ).toBeInTheDocument();
    // No delete has happened yet — confirmation is still pending.
    expect(deleteSpy).not.toHaveBeenCalled();
  });

  it('cancels without deleting when the Modal Cancel button is clicked', async () => {
    const user = userEvent.setup();
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({
      threads: [threadA, threadB],
    });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({ messages: [] });
    const deleteSpy = vi.spyOn(aipApi, 'deleteThread').mockResolvedValue();

    renderPage();

    const items = await screen.findAllByTestId('thread-list-item');
    await user.click(within(items[1]).getByTestId('thread-delete-btn'));

    const overlay = await screen.findByTestId('modal-overlay');
    await user.click(
      within(overlay).getByRole('button', { name: /cancel/i }),
    );

    // Modal closes, nothing deleted, both threads still present.
    await waitFor(() => {
      expect(screen.queryByTestId('modal-overlay')).not.toBeInTheDocument();
    });
    expect(deleteSpy).not.toHaveBeenCalled();
    expect(screen.getAllByTestId('thread-list-item')).toHaveLength(2);
  });

  it('deletes the thread only after confirming in the Modal', async () => {
    const user = userEvent.setup();
    // First load returns both threads; after delete the list refetch
    // returns just the surviving thread.
    const listSpy = vi
      .spyOn(aipApi, 'listThreads')
      .mockResolvedValueOnce({ threads: [threadA, threadB] })
      .mockResolvedValue({ threads: [threadA] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({ messages: [] });
    const deleteSpy = vi.spyOn(aipApi, 'deleteThread').mockResolvedValue();

    renderPage();

    const items = await screen.findAllByTestId('thread-list-item');
    expect(items).toHaveLength(2);
    await user.click(within(items[1]).getByTestId('thread-delete-btn'));

    const overlay = await screen.findByTestId('modal-overlay');
    await user.click(within(overlay).getByTestId('confirm-delete-thread'));

    // The delete API is called with the chosen thread id.
    await waitFor(() => {
      expect(deleteSpy).toHaveBeenCalledWith(threadB.id);
    });
    // Modal closes after a successful delete.
    await waitFor(() => {
      expect(screen.queryByTestId('modal-overlay')).not.toBeInTheDocument();
    });
    // The list refetches and the deleted thread disappears.
    await waitFor(() => {
      expect(listSpy.mock.calls.length).toBeGreaterThanOrEqual(2);
    });
    await waitFor(() => {
      expect(screen.getAllByTestId('thread-list-item')).toHaveLength(1);
    });
    expect(
      screen.queryByText('OpenAI exploration'),
    ).not.toBeInTheDocument();
  });
});
