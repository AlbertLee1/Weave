import { describe, it, expect, beforeEach, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { useAuthStore } from '../authStore';
import { login, logout, refresh } from '../api';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('auth/api', () => {
  beforeEach(() => {
    useAuthStore.getState().clear();
  });

  it('login posts credentials and stores access token', async () => {
    server.use(
      http.post('/api/auth/login', async ({ request }) => {
        const body = (await request.json()) as { email: string; password: string };
        if (body.email === 'alice@example.com' && body.password === 'letmein') {
          return HttpResponse.json({
            access_token: 'jwt.access.123',
            refresh_token: 'opaque.refresh',
            token_type: 'Bearer',
            expires_in: 900,
            user: {
              id: 'user:alice@example.com',
              email: 'alice@example.com',
              name: 'Alice',
              roles: ['editor'],
              ontologyRoles: {},
            },
          });
        }
        return new HttpResponse(
          JSON.stringify({ errorCode: 'UNAUTHORIZED', errorName: 'InvalidCredentials' }),
          { status: 401 },
        );
      }),
    );

    const resp = await login('alice@example.com', 'letmein');
    expect(resp.access_token).toBe('jwt.access.123');
    expect(useAuthStore.getState().accessToken).toBe('jwt.access.123');
  });

  it('login throws on 401', async () => {
    server.use(
      http.post('/api/auth/login', () =>
        new HttpResponse(
          JSON.stringify({ errorCode: 'UNAUTHORIZED', errorName: 'InvalidCredentials' }),
          { status: 401 },
        ),
      ),
    );
    await expect(login('alice@example.com', 'wrong')).rejects.toThrow();
    expect(useAuthStore.getState().accessToken).toBeNull();
  });

  it('refresh updates the access token', async () => {
    server.use(
      http.post('/api/auth/refresh', () =>
        HttpResponse.json({
          access_token: 'jwt.access.NEW',
          refresh_token: 'opaque.NEW',
          token_type: 'Bearer',
          expires_in: 900,
          user: { id: 'user:alice', email: '', name: '', roles: [], ontologyRoles: {} },
        }),
      ),
    );
    const resp = await refresh();
    expect(resp.access_token).toBe('jwt.access.NEW');
    expect(useAuthStore.getState().accessToken).toBe('jwt.access.NEW');
  });

  it('logout clears the access token', async () => {
    useAuthStore.getState().setAccessToken('something');
    server.use(http.post('/api/auth/logout', () => new HttpResponse(null, { status: 204 })));
    await logout();
    expect(useAuthStore.getState().accessToken).toBeNull();
  });
});
