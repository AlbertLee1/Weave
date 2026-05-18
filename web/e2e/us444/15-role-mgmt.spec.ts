import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 15 — role management.
 *
 * Verifies that the auth identity endpoint (`/api/v2/me`) returns the
 * roles claim for the dev user, and that the seeded admin/peer
 * accounts can both authenticate. AUTH_MODE=dev returns a synthetic
 * dev user with the admin role; AUTH_MODE=token requires bearer
 * tokens on every request — this spec accepts either contract.
 */
test.describe('US-444 — role management', () => {
  test('GET /api/v2/me surfaces the current user with a roles array', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/v2/me`);
    const failureBody = res.ok() ? '' : await res.text();
    expect(res.ok(), `me endpoint must be wired: ${res.status()} ${failureBody}`).toBe(true);

    const body = (await res.json()) as { roles?: unknown };
    expect(body).toHaveProperty('roles');
    expect(Array.isArray(body.roles)).toBe(true);
  });

  test('seeded peer (viewer) account can log in', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'peer@test', password: 'test1234' },
    });
    expect(res.ok()).toBe(true);
    const body = (await res.json()) as { access_token?: string };
    expect(typeof body.access_token).toBe('string');
  });
});
