import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MarkingAdminPage } from '../MarkingAdminPage';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

interface FetchCall {
  url: string;
  method: string;
}

function installFetch(responder: (call: FetchCall) => Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();
      return responder({ url, method });
    }),
  );
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/admin/markings']}>
        <MarkingAdminPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('MarkingAdminPage created-at', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  // Given a marking definition that carries a createdAt instant,
  // When the admin marking list renders,
  // Then the row shows the locale-formatted creation time (not the raw ISO).
  it('renders the formatted creation time for a marking carrying createdAt', async () => {
    const createdAtIso = '2026-04-19T10:00:00Z';
    installFetch((call) => {
      if (call.url.endsWith('/api/admin/markings') && call.method === 'GET') {
        return jsonResponse({
          markings: [
            {
              name: 'PUBLIC',
              displayName: 'Public',
              description: 'Unrestricted access',
              color: '#10b981',
              createdAt: createdAtIso,
            },
          ],
        });
      }
      return jsonResponse({ grants: [] });
    });

    renderPage();

    const cell = await screen.findByTestId('marking-created-PUBLIC');
    const expected = new Date(createdAtIso).toLocaleString();
    expect(cell.textContent).toBe(expected);
    // The raw ISO string must not leak through unformatted.
    expect(cell.textContent).not.toBe(createdAtIso);
  });

  // Existence guard: a marking missing createdAt must not crash the list and
  // must render an empty/placeholder cell rather than "Invalid Date".
  it('renders a placeholder when createdAt is absent', async () => {
    installFetch((call) => {
      if (call.url.endsWith('/api/admin/markings') && call.method === 'GET') {
        return jsonResponse({
          markings: [
            {
              name: 'INTERNAL',
              displayName: 'Internal',
              description: '',
              color: '#3b82f6',
            },
          ],
        });
      }
      return jsonResponse({ grants: [] });
    });

    renderPage();

    const cell = await screen.findByTestId('marking-created-INTERNAL');
    await waitFor(() => {
      expect(cell.textContent).not.toContain('Invalid');
    });
    expect(cell.textContent?.trim()).toBe('—');
  });
});
