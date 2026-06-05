import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider } from '../../../auth/AuthContext';
import { Shell } from '../Shell';

// The Shell mounts the Sidebar (reads /api/v2/me via AuthProvider) and the
// Topbar (fetches the notification unread count). Stub fetch so the whole
// component tree renders without network noise.
function stubFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/notifications/unread-count')) {
        return new Response(JSON.stringify({ count: 0 }), { status: 200 });
      }
      if (url.includes('/api/v2/me')) {
        return new Response(null, { status: 401 });
      }
      return new Response('{}', { status: 200 });
    }),
  );
}

function renderShell() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <AuthProvider>
          <Routes>
            <Route element={<Shell />}>
              <Route path="/" element={<div>home content</div>} />
            </Route>
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: Shell "Skip to main content" bypass link (WCAG 2.4.1)', () => {
  beforeEach(() => {
    stubFetch();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  // Given the global app shell is rendered
  // Then a "Skip to main content" link exists, targeting the main region
  it('renders a skip link pointing at #main-content', () => {
    renderShell();
    const skip = screen.getByRole('link', { name: /skip to main content/i });
    expect(skip).toBeInTheDocument();
    expect(skip).toHaveAttribute('href', '#main-content');
  });

  // Given the shell is rendered
  // Then the <main> region carries the matching id so the anchor resolves,
  // and is focusable (tabIndex=-1) so focus lands there after the jump.
  it('marks the <main> region with id="main-content" and makes it focusable', () => {
    renderShell();
    const main = document.querySelector('main');
    expect(main).not.toBeNull();
    expect(main).toHaveAttribute('id', 'main-content');
    expect(main).toHaveAttribute('tabindex', '-1');
  });

  // Given a keyboard user lands on the page
  // When they press Tab once
  // Then the very first focusable element is the skip link (the whole point
  // of WCAG 2.4.1 — bypass the nav before anything else).
  it('makes the skip link the first focusable element on Tab', async () => {
    const user = userEvent.setup();
    renderShell();
    const skip = screen.getByRole('link', { name: /skip to main content/i });
    await user.tab();
    expect(skip).toHaveFocus();
  });
});
