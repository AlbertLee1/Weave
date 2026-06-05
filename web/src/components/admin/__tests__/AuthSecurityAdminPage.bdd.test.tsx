import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  vi,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthSecurityAdminPage } from '../AuthSecurityAdminPage';

// BDD: the global Auth Security admin page surfaces the two low-level auth
// operations the backend already exposes — JWT signing-key rotation
// (POST /api/admin/auth/keys/rotate) and per-JTI token revocation
// (POST /api/auth/tokens/{jti}/revoke). Each scenario asserts both the
// rendered behaviour and — for mutations — the exact HTTP method/path/body the
// SPA fires, so the wire contract is locked from the outside.

interface Call {
  method: string;
  path: string;
  body: Record<string, unknown> | null;
}

let calls: Call[] = [];
let failRotate = false;
let failRevoke = false;

const server = setupServer(
  http.post('/api/admin/auth/keys/rotate', async ({ request }) => {
    let body: Record<string, unknown> | null = null;
    try {
      body = (await request.json()) as Record<string, unknown>;
    } catch {
      body = null;
    }
    calls.push({ method: 'POST', path: '/api/admin/auth/keys/rotate', body });
    if (failRotate) {
      return HttpResponse.json(
        {
          errorCode: 'INTERNAL',
          errorName: 'KeyRotationFailed',
          parameters: { reason: 'keyring is locked' },
        },
        { status: 500 },
      );
    }
    return HttpResponse.json(
      {
        activeKeyId: 'kid-new',
        keyIds: ['kid-old', 'kid-new'],
        rotatedAt: '2026-06-05T12:00:00Z',
      },
      { status: 200 },
    );
  }),
  http.post('/api/auth/tokens/:jti/revoke', async ({ request, params }) => {
    const jti = String(params.jti);
    let body: Record<string, unknown> | null = null;
    try {
      body = (await request.json()) as Record<string, unknown>;
    } catch {
      body = null;
    }
    calls.push({
      method: 'POST',
      path: `/api/auth/tokens/${jti}/revoke`,
      body,
    });
    if (failRevoke) {
      return HttpResponse.json(
        {
          errorCode: 'INTERNAL',
          errorName: 'TokenRevokeFailed',
          parameters: { reason: 'store unavailable' },
        },
        { status: 500 },
      );
    }
    return HttpResponse.json(
      { jti, revokedAt: '2026-06-05T12:05:00Z' },
      { status: 200 },
    );
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => {
  server.resetHandlers();
  calls = [];
  failRotate = false;
  failRevoke = false;
  vi.restoreAllMocks();
});
afterAll(() => server.close());

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return render(
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(
        MemoryRouter,
        { initialEntries: ['/admin/auth-security'] },
        createElement(AuthSecurityAdminPage, null),
      ),
    ),
  );
}

describe('BDD: AuthSecurityAdminPage', () => {
  it('exposes an h1 landmark and the pre-rotation placeholder for the keyring', async () => {
    renderPage();
    expect(
      await screen.findByRole('heading', { level: 1, name: /auth security/i }),
    ).toBeInTheDocument();
    // No GET endpoint exists, so before any rotation the keyring section shows
    // a hint rather than a list.
    expect(
      screen.getByTestId('auth-keys-placeholder'),
    ).toBeInTheDocument();
  });

  it('Given the operator clicks Rotate, Then a confirmation modal appears and nothing fires until confirmed', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByTestId('auth-keys-rotate-btn'));

    // A second-step confirmation modal appears; no POST yet.
    await screen.findByTestId('auth-keys-rotate-confirm');
    expect(calls.find((c) => c.method === 'POST')).toBeFalsy();
  });

  it('When rotation is confirmed, Then a POST is sent and the returned keyring is shown', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByTestId('auth-keys-rotate-btn'));
    await screen.findByTestId('auth-keys-rotate-confirm');
    await user.click(screen.getByTestId('auth-keys-rotate-confirm-btn'));

    await waitFor(() => {
      const post = calls.find(
        (c) => c.path === '/api/admin/auth/keys/rotate',
      );
      expect(post).toBeTruthy();
    });
    const post = calls.find((c) => c.path === '/api/admin/auth/keys/rotate')!;
    expect(post.method).toBe('POST');

    // The new active kid is highlighted and every kid in the ring is listed.
    const active = await screen.findByTestId('auth-keys-active-id');
    expect(active).toHaveTextContent('kid-new');
    const ring = screen.getByTestId('auth-keys-ring');
    expect(ring).toHaveTextContent('kid-old');
    expect(ring).toHaveTextContent('kid-new');
    expect(screen.getByTestId('auth-keys-rotated-at')).toBeInTheDocument();
  });

  it('When rotation fails, Then an error is surfaced and no keyring is shown', async () => {
    failRotate = true;
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByTestId('auth-keys-rotate-btn'));
    await screen.findByTestId('auth-keys-rotate-confirm');
    await user.click(screen.getByTestId('auth-keys-rotate-confirm-btn'));

    expect(
      await screen.findByTestId('auth-keys-rotate-error'),
    ).toHaveTextContent(/keyring is locked/i);
    expect(screen.queryByTestId('auth-keys-active-id')).toBeNull();
  });

  it('Given an empty jti, Then the revoke submit button is disabled', async () => {
    renderPage();
    const submit = await screen.findByTestId('auth-revoke-submit');
    expect(submit).toBeDisabled();
  });

  it('When a jti is supplied and submitted, Then a POST is sent with optional fields and revokedAt is shown', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.type(screen.getByTestId('auth-revoke-jti'), 'jti-123');
    await user.type(screen.getByTestId('auth-revoke-user-id'), 'user:alice');
    await user.type(
      screen.getByTestId('auth-revoke-reason'),
      'compromised laptop',
    );

    const submit = screen.getByTestId('auth-revoke-submit');
    expect(submit).toBeEnabled();
    await user.click(submit);

    await waitFor(() => {
      const post = calls.find((c) => c.path.endsWith('/revoke'));
      expect(post).toBeTruthy();
    });
    const post = calls.find((c) => c.path.endsWith('/revoke'))!;
    expect(post.method).toBe('POST');
    expect(post.path).toBe('/api/auth/tokens/jti-123/revoke');
    expect(post.body).toMatchObject({
      userId: 'user:alice',
      reason: 'compromised laptop',
    });

    const success = await screen.findByTestId('auth-revoke-success');
    expect(success).toHaveTextContent('jti-123');
    expect(success).toHaveTextContent(/2026-06-05/);

    // The form clears after a successful revoke.
    await waitFor(() =>
      expect(screen.getByTestId('auth-revoke-jti')).toHaveValue(''),
    );
  });

  it('When an expiresAt is supplied, Then it is forwarded as an RFC3339 (UTC) timestamp', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.type(screen.getByTestId('auth-revoke-jti'), 'jti-exp');
    // datetime-local yields a "YYYY-MM-DDTHH:MM" value; the page must convert
    // it to an RFC3339 Z-suffixed instant the backend can parse.
    const dt = screen.getByTestId('auth-revoke-expires-at');
    await user.clear(dt);
    await user.type(dt, '2026-12-31T23:59');
    await user.click(screen.getByTestId('auth-revoke-submit'));

    await waitFor(() => {
      const post = calls.find((c) => c.path.endsWith('/revoke'));
      expect(post).toBeTruthy();
    });
    const post = calls.find((c) => c.path.endsWith('/revoke'))!;
    expect(post.body).toMatchObject({
      expiresAt: '2026-12-31T23:59:00.000Z',
    });
  });

  it('When revoke fails, Then an error is surfaced', async () => {
    failRevoke = true;
    const user = userEvent.setup();
    renderPage();

    await user.type(screen.getByTestId('auth-revoke-jti'), 'jti-fail');
    await user.click(screen.getByTestId('auth-revoke-submit'));

    expect(
      await screen.findByTestId('auth-revoke-error'),
    ).toHaveTextContent(/store unavailable/i);
    expect(screen.queryByTestId('auth-revoke-success')).toBeNull();
  });
});
