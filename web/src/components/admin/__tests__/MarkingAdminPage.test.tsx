import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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
  body?: string;
}

type Responder = (call: FetchCall) => Response;

function installFetch(responder: Responder): FetchCall[] {
  const calls: FetchCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();
      const body =
        typeof init?.body === 'string' ? init.body : undefined;
      const call: FetchCall = { url, method, body };
      calls.push(call);
      return responder(call);
    }),
  );
  return calls;
}

const MARKINGS = [
  {
    name: 'PUBLIC',
    displayName: 'Public',
    description: 'Unrestricted access',
    color: '#10b981',
  },
  {
    name: 'PII',
    displayName: 'PII',
    description: 'Personally identifiable information',
    color: '#ef4444',
  },
];

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

describe('MarkingAdminPage', () => {
  beforeEach(() => {});

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders the marking list and auto-selects the first marking', async () => {
    installFetch((call) => {
      if (call.url.endsWith('/api/admin/markings') && call.method === 'GET') {
        return jsonResponse({ markings: MARKINGS });
      }
      if (call.url.includes('/api/admin/markings/PUBLIC/grants')) {
        return jsonResponse({
          grants: [
            {
              userId: 'user:alice@example.com',
              markingName: 'PUBLIC',
              grantedAt: '2026-04-19T10:00:00Z',
              grantedBy: 'user:admin@example.com',
            },
          ],
        });
      }
      return jsonResponse({ grants: [] });
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByTestId('marking-row-PUBLIC')).toBeInTheDocument();
      expect(screen.getByTestId('marking-row-PII')).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByTestId('grant-row-user:alice@example.com'))
        .toBeInTheDocument();
    });
  });

  it('switches the grant roster when a marking is selected', async () => {
    installFetch((call) => {
      if (call.url.endsWith('/api/admin/markings') && call.method === 'GET') {
        return jsonResponse({ markings: MARKINGS });
      }
      if (call.url.includes('/api/admin/markings/PII/grants')) {
        return jsonResponse({
          grants: [
            {
              userId: 'user:bob@example.com',
              markingName: 'PII',
              grantedAt: '2026-04-19T11:00:00Z',
              grantedBy: 'user:admin@example.com',
            },
          ],
        });
      }
      return jsonResponse({ grants: [] });
    });

    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('marking-row-PII')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByTestId('marking-row-PII'));

    await waitFor(() => {
      expect(screen.getByTestId('grant-row-user:bob@example.com'))
        .toBeInTheDocument();
    });
  });

  it('grants a marking via POST /api/admin/users/{id}/markings', async () => {
    const calls = installFetch((call) => {
      if (call.url.endsWith('/api/admin/markings') && call.method === 'GET') {
        return jsonResponse({ markings: MARKINGS });
      }
      if (
        call.url.includes('/api/admin/markings/PUBLIC/grants') &&
        call.method === 'GET'
      ) {
        return jsonResponse({ grants: [] });
      }
      if (
        call.url.includes('/api/admin/users/') &&
        call.url.endsWith('/markings') &&
        call.method === 'POST'
      ) {
        return jsonResponse({
          grants: [{ userId: 'user:charlie@example.com', markingName: 'PUBLIC' }],
        });
      }
      return jsonResponse({ grants: [] });
    });

    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('grants-empty')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByTestId('grant-button'));
    const input = await screen.findByTestId('grant-user-input');
    await userEvent.type(input, 'user:charlie@example.com');
    await userEvent.click(screen.getByRole('button', { name: /^Grant$/i }));

    await waitFor(() => {
      const postCall = calls.find(
        (c) =>
          c.method === 'POST' &&
          c.url.includes('/api/admin/users/') &&
          c.url.endsWith('/markings'),
      );
      expect(postCall).toBeDefined();
      expect(postCall!.body).toContain('"marking":"PUBLIC"');
      expect(postCall!.url).toContain(
        encodeURIComponent('user:charlie@example.com'),
      );
    });
  });

  it('revokes only after confirming in the dialog', async () => {
    const calls = installFetch((call) => {
      if (call.url.endsWith('/api/admin/markings') && call.method === 'GET') {
        return jsonResponse({ markings: MARKINGS });
      }
      if (
        call.url.includes('/api/admin/markings/PUBLIC/grants') &&
        call.method === 'GET'
      ) {
        return jsonResponse({
          grants: [
            {
              userId: 'user:alice@example.com',
              markingName: 'PUBLIC',
              grantedAt: '2026-04-19T10:00:00Z',
              grantedBy: 'user:admin@example.com',
            },
          ],
        });
      }
      if (call.method === 'DELETE') {
        return new Response(null, { status: 204 });
      }
      return jsonResponse({ grants: [] });
    });

    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('grant-row-user:alice@example.com'))
        .toBeInTheDocument();
    });

    await userEvent.click(
      screen.getByTestId('revoke-button-user:alice@example.com'),
    );
    expect(screen.getByTestId('revoke-dialog')).toBeInTheDocument();

    // No DELETE fired yet — only after confirm.
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false);

    await userEvent.click(screen.getByTestId('confirm-revoke'));

    await waitFor(() => {
      const del = calls.find((c) => c.method === 'DELETE');
      expect(del).toBeDefined();
      expect(del!.url).toContain(encodeURIComponent('user:alice@example.com'));
      expect(del!.url).toContain('/markings/PUBLIC');
    });
  });

  it('renders an empty state when no grants exist', async () => {
    installFetch((call) => {
      if (call.url.endsWith('/api/admin/markings') && call.method === 'GET') {
        return jsonResponse({ markings: MARKINGS });
      }
      return jsonResponse({ grants: [] });
    });

    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('grants-empty')).toBeInTheDocument();
    });
    const empty = screen.getByTestId('grants-empty');
    expect(within(empty).getByText('PUBLIC')).toBeInTheDocument();
  });
});
