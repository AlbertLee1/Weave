import { describe, it, expect, beforeEach, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { useAuthStore } from '../authStore';
import { authedFetch, resetRefreshState } from '../interceptor';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => {
  server.resetHandlers();
  resetRefreshState();
});
afterAll(() => server.close());

describe('authed fetch interceptor', () => {
  beforeEach(() => {
    useAuthStore.getState().clear();
  });

  it('adds Authorization header when access token is present', async () => {
    let receivedAuth: string | null = null;
    server.use(
      http.get('/api/test', ({ request }) => {
        receivedAuth = request.headers.get('Authorization');
        return HttpResponse.json({ ok: true });
      }),
    );

    useAuthStore.getState().setAccessToken('access.123');
    const res = await authedFetch('/api/test');
    expect(res.ok).toBe(true);
    expect(receivedAuth).toBe('Bearer access.123');
  });

  it('omits Authorization header when no access token', async () => {
    let receivedAuth: string | null = 'sentinel';
    server.use(
      http.get('/api/test', ({ request }) => {
        receivedAuth = request.headers.get('Authorization');
        return HttpResponse.json({ ok: true });
      }),
    );

    const res = await authedFetch('/api/test');
    expect(res.ok).toBe(true);
    expect(receivedAuth).toBeNull();
  });

  it('on 401 fires refresh and retries the original request', async () => {
    let attempts = 0;
    server.use(
      http.get('/api/test', ({ request }) => {
        attempts++;
        const auth = request.headers.get('Authorization');
        if (auth === 'Bearer NEW') {
          return HttpResponse.json({ ok: true, retried: true });
        }
        return new HttpResponse(null, { status: 401 });
      }),
      http.post('/api/auth/refresh', () =>
        HttpResponse.json({
          access_token: 'NEW',
          refresh_token: 'r2',
          token_type: 'Bearer',
          expires_in: 900,
          user: { id: 'u', email: '', name: '', roles: [], ontologyRoles: {} },
        }),
      ),
    );

    useAuthStore.getState().setAccessToken('OLD');
    const res = await authedFetch('/api/test');
    expect(res.ok).toBe(true);
    const json = (await res.json()) as { retried: boolean };
    expect(json.retried).toBe(true);
    expect(attempts).toBe(2);
    expect(useAuthStore.getState().accessToken).toBe('NEW');
  });

  it('on refresh failure clears token and propagates 401', async () => {
    server.use(
      http.get('/api/test', () => new HttpResponse(null, { status: 401 })),
      http.post('/api/auth/refresh', () => new HttpResponse(null, { status: 401 })),
    );

    useAuthStore.getState().setAccessToken('OLD');
    const res = await authedFetch('/api/test');
    expect(res.status).toBe(401);
    expect(useAuthStore.getState().accessToken).toBeNull();
  });

  it('serializes concurrent 401s into a single refresh call', async () => {
    let refreshCount = 0;
    let okHits = 0;
    server.use(
      http.get('/api/test', ({ request }) => {
        const auth = request.headers.get('Authorization');
        if (auth === 'Bearer NEW') {
          okHits++;
          return HttpResponse.json({ ok: true });
        }
        return new HttpResponse(null, { status: 401 });
      }),
      http.post('/api/auth/refresh', () => {
        refreshCount++;
        return HttpResponse.json({
          access_token: 'NEW',
          refresh_token: 'r2',
          token_type: 'Bearer',
          expires_in: 900,
          user: { id: 'u', email: '', name: '', roles: [], ontologyRoles: {} },
        });
      }),
    );

    useAuthStore.getState().setAccessToken('OLD');
    const results = await Promise.all([
      authedFetch('/api/test'),
      authedFetch('/api/test'),
      authedFetch('/api/test'),
      authedFetch('/api/test'),
      authedFetch('/api/test'),
    ]);
    for (const r of results) expect(r.ok).toBe(true);
    expect(refreshCount).toBe(1);
    expect(okHits).toBe(5);
  });

  it('does not loop refresh on 401 from /api/auth/refresh itself', async () => {
    let calls = 0;
    server.use(
      http.post('/api/auth/refresh', () => {
        calls++;
        return new HttpResponse(null, { status: 401 });
      }),
    );

    const res = await authedFetch('/api/auth/refresh', { method: 'POST', body: '{}' });
    expect(res.status).toBe(401);
    expect(calls).toBe(1);
  });
});
