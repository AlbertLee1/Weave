import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ThreadsPage } from '../ThreadsPage';
import * as aipApi from '../../../api/aip';
import type { AIPMessage, AIPThread } from '../../../api/aip';

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

function buildMessages(): AIPMessage[] {
  return [
    {
      id: 1,
      threadId: threadA.id,
      role: 'user',
      content: 'Hello there.',
      createdAt: '2026-04-28T08:01:00Z',
    },
    {
      id: 2,
      threadId: threadA.id,
      role: 'assistant',
      content: 'Hi! How can I help?',
      createdAt: '2026-04-28T08:01:01Z',
    },
  ];
}

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

describe('ThreadsPage (US-280)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows the empty state when no threads exist', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [] });
    renderPage();
    expect(
      await screen.findByText(/no threads yet/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/no thread selected/i)).toBeInTheDocument();
  });

  it('renders a "Start a thread" CTA inside the empty state (dogfood #6)', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [] });
    renderPage();
    const emptyBlock = await screen.findByTestId('thread-list-empty');
    const cta = within(emptyBlock).getByRole('button', { name: /start a thread/i });
    expect(cta).toBeInTheDocument();
  });

  it('lists threads and auto-selects the first conversation', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({
      threads: [threadA, threadB],
    });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({
      messages: buildMessages(),
    });

    renderPage();

    const items = await screen.findAllByTestId('thread-list-item');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveAttribute('aria-current', 'true');

    await waitFor(() => {
      expect(screen.getByTestId('thread-conversation')).toBeInTheDocument();
    });
    const messages = await screen.findAllByTestId('thread-message');
    expect(messages).toHaveLength(2);
    expect(messages[0]).toHaveAttribute('data-role', 'user');
    expect(messages[1]).toHaveAttribute('data-role', 'assistant');
    expect(within(messages[1]).getByTestId('message-content').textContent).toContain(
      'Hi! How can I help?',
    );
  });

  it('switches the active thread when a list item is clicked', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({
      threads: [threadA, threadB],
    });
    const listMessagesSpy = vi
      .spyOn(aipApi, 'listMessages')
      .mockImplementation(async (id) => ({
        messages: id === threadA.id ? buildMessages() : [],
      }));

    renderPage();

    const items = await screen.findAllByTestId('thread-list-item');
    await act(async () => {
      fireEvent.click(items[1]);
    });

    await waitFor(() => {
      expect(items[1]).toHaveAttribute('aria-current', 'true');
    });
    await waitFor(() => {
      expect(listMessagesSpy).toHaveBeenCalledWith(threadB.id);
    });
  });

  it('creates a new thread via the modal and selects it', async () => {
    const listSpy = vi
      .spyOn(aipApi, 'listThreads')
      .mockResolvedValueOnce({ threads: [] })
      .mockResolvedValueOnce({ threads: [threadA] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({ messages: [] });
    const createSpy = vi
      .spyOn(aipApi, 'createThread')
      .mockResolvedValue(threadA);

    renderPage();

    await waitFor(() => expect(listSpy).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByTestId('new-thread-btn'));
    const overlay = await screen.findByTestId('modal-overlay');

    fireEvent.change(within(overlay).getByTestId('new-thread-title'), {
      target: { value: 'Mock greeting' },
    });
    fireEvent.change(within(overlay).getByTestId('new-thread-provider'), {
      target: { value: 'mock' },
    });
    await act(async () => {
      fireEvent.click(within(overlay).getByTestId('new-thread-submit'));
    });

    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledWith({
        title: 'Mock greeting',
        provider: 'mock',
        model: undefined,
        systemPrompt: undefined,
      });
    });
    await waitFor(() => {
      expect(screen.queryByTestId('modal-overlay')).not.toBeInTheDocument();
    });
  });

  it('sends a message and reveals the assistant reply with a streaming cursor', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [threadA] });
    const listMessagesSpy = vi
      .spyOn(aipApi, 'listMessages')
      .mockResolvedValueOnce({ messages: [] })
      .mockResolvedValue({
        messages: [
          {
            id: 10,
            threadId: threadA.id,
            role: 'user',
            content: 'Hi',
            createdAt: '2026-04-28T08:02:00Z',
          },
          {
            id: 11,
            threadId: threadA.id,
            role: 'assistant',
            content: 'Hello!',
            createdAt: '2026-04-28T08:02:01Z',
          },
        ],
      });
    const sendSpy = vi
      .spyOn(aipApi, 'sendMessage')
      .mockResolvedValue({
        userMessage: {
          id: 10,
          threadId: threadA.id,
          role: 'user',
          content: 'Hi',
          createdAt: '2026-04-28T08:02:00Z',
        },
        assistantMessage: {
          id: 11,
          threadId: threadA.id,
          role: 'assistant',
          content: 'Hello!',
          createdAt: '2026-04-28T08:02:01Z',
        },
      });

    renderPage();

    await screen.findByTestId('composer-input');

    fireEvent.change(screen.getByTestId('composer-input'), {
      target: { value: 'Hi' },
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('composer-send'));
    });

    await waitFor(() => {
      expect(sendSpy).toHaveBeenCalledWith(threadA.id, { content: 'Hi' });
    });
    await waitFor(() => {
      expect(listMessagesSpy.mock.calls.length).toBeGreaterThanOrEqual(2);
    });

    await waitFor(() => {
      const items = screen.getAllByTestId('thread-message');
      expect(items.length).toBeGreaterThanOrEqual(2);
    });

    // Streaming cursor visible while typewriter effect is active.
    expect(screen.getByTestId('streaming-cursor')).toBeInTheDocument();

    // Drive the typewriter to completion. STREAM_TICK_MS is 18; six chars
    // ('Hello!') needs at least 6 ticks but advance generously to allow
    // React state to settle.
    await act(async () => {
      vi.advanceTimersByTime(500);
    });

    await waitFor(() => {
      const assistantBubble = screen
        .getAllByTestId('thread-message')
        .find((el) => el.getAttribute('data-role') === 'assistant')!;
      expect(
        within(assistantBubble).getByTestId('message-content').textContent,
      ).toContain('Hello!');
      expect(screen.queryByTestId('streaming-cursor')).not.toBeInTheDocument();
    });
  });

  it('rejects sending an empty message with an inline error', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [threadA] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({ messages: [] });
    const sendSpy = vi.spyOn(aipApi, 'sendMessage');

    renderPage();
    await screen.findByTestId('composer-input');

    await act(async () => {
      fireEvent.click(screen.getByTestId('composer-send'));
    });

    expect(await screen.findByTestId('composer-error')).toHaveTextContent(
      /please enter a message/i,
    );
    expect(sendSpy).not.toHaveBeenCalled();
  });
});
