import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ThreadsPage, computeBranchPath } from '../ThreadsPage';
import * as aipApi from '../../../api/aip';
import type {
  AIPMessage,
  AIPMessageTreeNode,
  AIPThread,
  ThreadTreeResponse,
} from '../../../api/aip';

const thread: AIPThread = {
  id: 'thr_main',
  title: 'Tree thread',
  provider: 'mock',
  model: 'weave-mock-llm-v1',
  systemPrompt: '',
  createdBy: 'user-1',
  createdAt: '2026-04-29T08:00:00Z',
  updatedAt: '2026-04-29T08:00:00Z',
};

// Build a forking history:
//   1 (user "hello")
//     └─ 2 (assistant "hi-A")
//          └─ 4 (user "follow-A")
//               └─ 5 (assistant "reply-A")
//     └─ 3 (assistant "hi-B")          <-- alternate branch
//          └─ 6 (user "follow-B")
//               └─ 7 (assistant "reply-B")
function fixtureMessages(): AIPMessage[] {
  return [
    msg(1, 'user', 'hello', null),
    msg(2, 'assistant', 'hi-A', 1),
    msg(3, 'assistant', 'hi-B', 1),
    msg(4, 'user', 'follow-A', 2),
    msg(5, 'assistant', 'reply-A', 4),
    msg(6, 'user', 'follow-B', 3),
    msg(7, 'assistant', 'reply-B', 6),
  ];
}

function fixtureTree(): ThreadTreeResponse {
  const byId: Record<number, AIPMessageTreeNode> = {};
  for (const m of fixtureMessages()) {
    byId[m.id] = { ...m, children: [] };
  }
  for (const m of fixtureMessages()) {
    if (m.parentMessageId != null) {
      byId[m.parentMessageId].children!.push(byId[m.id]);
    }
  }
  return {
    threadId: thread.id,
    roots: [byId[1]],
  };
}

function msg(
  id: number,
  role: AIPMessage['role'],
  content: string,
  parentId: number | null,
): AIPMessage {
  return {
    id,
    threadId: thread.id,
    role,
    content,
    parentMessageId: parentId,
    branchId: 'main',
    createdAt: `2026-04-29T08:0${id}:00Z`,
  };
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

describe('computeBranchPath', () => {
  it('returns empty when tip is null', () => {
    expect(computeBranchPath(fixtureMessages(), null).size).toBe(0);
  });

  it('walks parent chain from tip to root', () => {
    const path = computeBranchPath(fixtureMessages(), 5);
    expect([...path].sort()).toEqual([1, 2, 4, 5]);
  });

  it('walks the alternate branch when tip is on the right side', () => {
    const path = computeBranchPath(fixtureMessages(), 7);
    expect([...path].sort()).toEqual([1, 3, 6, 7]);
  });

  it('terminates cleanly when parent id points outside the slice', () => {
    const orphans: AIPMessage[] = [
      msg(10, 'user', 'orphan', 999),
      msg(11, 'assistant', 'reply', 10),
    ];
    const path = computeBranchPath(orphans, 11);
    expect([...path].sort()).toEqual([10, 11]);
  });
});

describe('ThreadsPage US-375 tree panel', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders the tree panel with every message node', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({
      messages: fixtureMessages(),
    });
    vi.spyOn(aipApi, 'getThreadTree').mockResolvedValue(fixtureTree());

    renderPage();

    await screen.findByTestId('thread-tree-panel');
    const nodes = await screen.findAllByTestId('thread-tree-node');
    expect(nodes).toHaveLength(7);
    const ids = nodes
      .map((n) => Number(n.getAttribute('data-message-id')))
      .sort((a, b) => a - b);
    expect(ids).toEqual([1, 2, 3, 4, 5, 6, 7]);
  });

  it('defaults the active branch tip to the latest message', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({
      messages: fixtureMessages(),
    });
    vi.spyOn(aipApi, 'getThreadTree').mockResolvedValue(fixtureTree());

    renderPage();

    await screen.findByTestId('thread-tree-panel');
    await waitFor(() => {
      const tip = screen
        .getAllByTestId('thread-tree-node')
        .find((el) => el.getAttribute('data-active-tip') === 'true');
      expect(tip).toBeDefined();
      expect(tip!.getAttribute('data-message-id')).toBe('7');
    });

    const onBranch = screen
      .getAllByTestId('thread-tree-node')
      .filter((el) => el.getAttribute('data-on-branch') === 'true')
      .map((el) => Number(el.getAttribute('data-message-id')))
      .sort((a, b) => a - b);
    expect(onBranch).toEqual([1, 3, 6, 7]);

    // Conversation pane reflects the active branch path only.
    const visibleIds = (await screen.findAllByTestId('thread-message'))
      .map((el) => Number(el.getAttribute('data-message-id')))
      .sort((a, b) => a - b);
    expect(visibleIds).toEqual([1, 3, 6, 7]);
  });

  it('switches the active branch when a tree node is clicked', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({
      messages: fixtureMessages(),
    });
    vi.spyOn(aipApi, 'getThreadTree').mockResolvedValue(fixtureTree());

    renderPage();

    await screen.findByTestId('thread-tree-panel');
    await waitFor(() => {
      expect(
        screen
          .getAllByTestId('thread-tree-node')
          .find((el) => el.getAttribute('data-active-tip') === 'true'),
      ).toBeDefined();
    });

    const nodes = screen.getAllByTestId('thread-tree-node');
    const targetA = nodes.find(
      (n) => n.getAttribute('data-message-id') === '5',
    )!;
    await act(async () => {
      fireEvent.click(targetA);
    });

    await waitFor(() => {
      const tip = screen
        .getAllByTestId('thread-tree-node')
        .find((el) => el.getAttribute('data-active-tip') === 'true');
      expect(tip!.getAttribute('data-message-id')).toBe('5');
    });

    const visibleIds = (await screen.findAllByTestId('thread-message'))
      .map((el) => Number(el.getAttribute('data-message-id')))
      .sort((a, b) => a - b);
    expect(visibleIds).toEqual([1, 2, 4, 5]);
  });

  it('shows an empty placeholder when the tree query returns no roots', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({ messages: [] });
    vi.spyOn(aipApi, 'getThreadTree').mockResolvedValue({
      threadId: thread.id,
      roots: [],
    });

    renderPage();
    expect(await screen.findByTestId('thread-tree-empty')).toBeInTheDocument();
  });

  it('exposes branch_id badges for non-default branches', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    const branched: AIPMessage[] = [
      { ...msg(1, 'user', 'hello', null), branchId: 'main' },
      { ...msg(2, 'assistant', 'forked', 1), branchId: 'feature-x' },
    ];
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({ messages: branched });
    vi.spyOn(aipApi, 'getThreadTree').mockResolvedValue({
      threadId: thread.id,
      roots: [
        {
          ...branched[0],
          children: [{ ...branched[1], children: [] }],
        } as AIPMessageTreeNode,
      ],
    });

    renderPage();

    const branchBadges = await screen.findAllByTestId('thread-tree-node-branch');
    expect(branchBadges).toHaveLength(1);
    expect(branchBadges[0].textContent).toBe('feature-x');
  });
});
