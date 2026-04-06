import { useAuthStore } from './authStore';
import { refresh as callRefresh } from './api';

/**
 * Singleton refresh promise. When concurrent requests all hit 401, they all
 * await the same promise so only one /api/auth/refresh hits the backend.
 */
let pendingRefresh: Promise<string | null> | null = null;

/**
 * resetRefreshState clears the in-flight refresh promise. Test-only helper.
 */
export function resetRefreshState(): void {
  pendingRefresh = null;
}

function isAuthEndpoint(url: string): boolean {
  return url.includes('/api/auth/login') || url.includes('/api/auth/refresh') || url.includes('/api/auth/logout');
}

async function doRefresh(): Promise<string | null> {
  if (pendingRefresh) return pendingRefresh;
  pendingRefresh = (async () => {
    try {
      const resp = await callRefresh();
      return resp.access_token;
    } catch {
      useAuthStore.getState().clear();
      return null;
    } finally {
      // Clear immediately after settles so the next 401 can fire a fresh
      // refresh (the previous access token is invalid by now).
      setTimeout(() => {
        pendingRefresh = null;
      }, 0);
    }
  })();
  return pendingRefresh;
}

function withAuth(init: RequestInit | undefined, token: string | null): RequestInit {
  const headers = new Headers(init?.headers);
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  return { ...(init ?? {}), credentials: 'include', headers };
}

/**
 * authedFetch is a fetch wrapper that:
 *   1. Adds Authorization: Bearer <token> from authStore on every request.
 *   2. On 401, fires a single refresh (serialized across concurrent calls)
 *      and retries the original request once.
 *   3. On refresh failure, clears the access token so the SPA can redirect
 *      to /login on next render.
 *   4. Skips refresh for auth endpoints themselves to avoid loops.
 */
export async function authedFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const url = typeof input === 'string' ? input : input.toString();
  const token = useAuthStore.getState().accessToken;

  const res = await fetch(input, withAuth(init, token));
  if (res.status !== 401 || isAuthEndpoint(url)) {
    return res;
  }

  // Try to refresh and retry exactly once.
  const newToken = await doRefresh();
  if (!newToken) {
    return res; // refresh failed; surface the original 401 to caller
  }
  return fetch(input, withAuth(init, newToken));
}
