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

const thread: AIPThread = {
  id: 'thr_src',
  title: 'Source conversation',
  provider: 'mock',
  model: 'weave-mock-llm-v1',
  systemPrompt: '',
  createdBy: 'user-1',
  createdAt: '2026-05-01T08:00:00Z',
  updatedAt: '2026-05-01T08:00:00Z',
};

const forked: AIPThread = {
  id: 'thr_fork',
  title: 'Source conversation (fork)',
  provider: 'mock',
  model: 'weave-mock-llm-v1',
  systemPrompt: '',
  createdBy: 'user-1',
  createdAt: '2026-05-01T09:00:00Z',
  updatedAt: '2026-05-01T09:00:00Z',
};

function buildMessages(): AIPMessage[] {
  return [
    {
      id: 1,
      threadId: thread.id,
      role: 'user',
      content: 'Hello there.',
      createdAt: '2026-05-01T08:01:00Z',
    },
    {
      id: 2,
      threadId: thread.id,
      role: 'assistant',
      content: 'Hi! How can I help?',
      createdAt: '2026-05-01T08:01:01Z',
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

describe('ThreadsPage Unit 10 — fork + advanced send params', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('forks from a message bubble: POSTs the fork endpoint with the pivot messageId and selects the new thread', async () => {
    const listSpy = vi
      .spyOn(aipApi, 'listThreads')
      .mockResolvedValueOnce({ threads: [thread] })
      .mockResolvedValue({ threads: [thread, forked] });
    vi.spyOn(aipApi, 'listMessages').mockImplementation(async (id) => ({
      messages: id === thread.id ? buildMessages() : [],
    }));
    const forkSpy = vi.spyOn(aipApi, 'forkThread').mockResolvedValue({
      thread: forked,
      messages: buildMessages(),
    });

    // Avoid the blocking window.prompt; default the optional title.
    const promptSpy = vi
      .spyOn(window, 'prompt')
      .mockReturnValue('Branch from #2');

    renderPage();

    await waitFor(() => expect(listSpy).toHaveBeenCalled());
    const bubbles = await screen.findAllByTestId('thread-message');
    expect(bubbles).toHaveLength(2);

    // Fork from the assistant reply (message id 2).
    const assistantBubble = bubbles.find(
      (b) => b.getAttribute('data-message-id') === '2',
    )!;
    const forkBtn = within(assistantBubble).getByTestId('message-fork-btn');

    await act(async () => {
      fireEvent.click(forkBtn);
    });

    await waitFor(() => {
      expect(forkSpy).toHaveBeenCalledWith(thread.id, {
        messageId: 2,
        title: 'Branch from #2',
      });
    });

    // The freshly-created fork becomes the active thread.
    await waitFor(() => {
      const active = screen
        .getAllByTestId('thread-list-item')
        .find((el) => el.getAttribute('aria-current') === 'true');
      expect(active?.getAttribute('data-thread-id')).toBe(forked.id);
    });

    promptSpy.mockRestore();
  });

  it('omits title when the fork prompt is cancelled', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({
      messages: buildMessages(),
    });
    const forkSpy = vi.spyOn(aipApi, 'forkThread').mockResolvedValue({
      thread: forked,
      messages: buildMessages(),
    });
    const promptSpy = vi.spyOn(window, 'prompt').mockReturnValue(null);

    renderPage();

    const bubbles = await screen.findAllByTestId('thread-message');
    const userBubble = bubbles.find(
      (b) => b.getAttribute('data-message-id') === '1',
    )!;

    await act(async () => {
      fireEvent.click(within(userBubble).getByTestId('message-fork-btn'));
    });

    await waitFor(() => {
      expect(forkSpy).toHaveBeenCalledWith(thread.id, { messageId: 1 });
    });

    promptSpy.mockRestore();
  });

  it('sends temperature and maxTokens from the advanced composer row', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    vi.spyOn(aipApi, 'listMessages')
      .mockResolvedValueOnce({ messages: [] })
      .mockResolvedValue({ messages: buildMessages() });
    const sendSpy = vi.spyOn(aipApi, 'sendMessage').mockResolvedValue({
      userMessage: {
        id: 10,
        threadId: thread.id,
        role: 'user',
        content: 'Tune me',
        createdAt: '2026-05-01T08:02:00Z',
      },
      assistantMessage: {
        id: 11,
        threadId: thread.id,
        role: 'assistant',
        content: 'Tuned.',
        createdAt: '2026-05-01T08:02:01Z',
      },
    });

    renderPage();

    await screen.findByTestId('composer-input');

    // Expand the advanced controls.
    fireEvent.click(screen.getByTestId('composer-advanced-toggle'));

    const tempInput = await screen.findByTestId('composer-temperature');
    fireEvent.change(tempInput, { target: { value: '1.5' } });

    const maxTokensInput = screen.getByTestId('composer-max-tokens');
    fireEvent.change(maxTokensInput, { target: { value: '256' } });

    fireEvent.change(screen.getByTestId('composer-input'), {
      target: { value: 'Tune me' },
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('composer-send'));
    });

    await waitFor(() => {
      expect(sendSpy).toHaveBeenCalledWith(thread.id, {
        content: 'Tune me',
        temperature: 1.5,
        maxTokens: 256,
      });
    });
  });

  it('omits advanced params when they are left unset', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    vi.spyOn(aipApi, 'listMessages')
      .mockResolvedValueOnce({ messages: [] })
      .mockResolvedValue({ messages: buildMessages() });
    const sendSpy = vi.spyOn(aipApi, 'sendMessage').mockResolvedValue({
      userMessage: {
        id: 10,
        threadId: thread.id,
        role: 'user',
        content: 'Plain',
        createdAt: '2026-05-01T08:02:00Z',
      },
      assistantMessage: {
        id: 11,
        threadId: thread.id,
        role: 'assistant',
        content: 'OK.',
        createdAt: '2026-05-01T08:02:01Z',
      },
    });

    renderPage();

    await screen.findByTestId('composer-input');
    fireEvent.change(screen.getByTestId('composer-input'), {
      target: { value: 'Plain' },
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('composer-send'));
    });

    await waitFor(() => {
      expect(sendSpy).toHaveBeenCalledWith(thread.id, { content: 'Plain' });
    });
  });
});
