import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ThreadsPage } from '../ThreadsPage';
import * as aipApi from '../../../api/aip';
import type { AIPThread } from '../../../api/aip';

// BDD (a11y, heading structure): the Threads page is a standalone route, so —
// like every other page in the app — it must expose exactly one page-level
// <h1> as its main heading. The heading is visually hidden (sr-only) so the
// existing layout is untouched, but it remains reachable for screen readers.
//
// Given the Threads page is rendered,
// Then there is exactly one level-1 heading, named for the page ("Threads").

const thread: AIPThread = {
  id: 'thr_aaa',
  title: 'Mock greeting',
  provider: 'mock',
  model: 'weave-mock-llm-v1',
  systemPrompt: '',
  createdBy: 'user-1',
  createdAt: '2026-04-28T08:00:00Z',
  updatedAt: '2026-04-28T08:00:00Z',
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

describe('ThreadsPage page-level heading (a11y)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders exactly one level-1 heading named for the page', async () => {
    vi.spyOn(aipApi, 'listThreads').mockResolvedValue({ threads: [thread] });
    vi.spyOn(aipApi, 'listMessages').mockResolvedValue({ messages: [] });

    renderPage();

    // Wait for the page shell to be present (independent of data load).
    await screen.findByTestId('threads-page');

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/threads/i);
  });

  it('still exposes a single h1 even before any thread loads', () => {
    // Never resolve the list query so the page is in its initial state.
    vi.spyOn(aipApi, 'listThreads').mockReturnValue(new Promise(() => {}));
    vi.spyOn(aipApi, 'listMessages').mockReturnValue(new Promise(() => {}));

    renderPage();

    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0]).toHaveAccessibleName(/threads/i);
  });
});
