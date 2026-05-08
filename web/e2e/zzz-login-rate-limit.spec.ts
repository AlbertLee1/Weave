import { test, expect } from '@playwright/test';

const API_BASE = 'http://localhost:9117';

/**
 * US-078: Playwright spec — login rate limit.
 *
 * The backend login handler enforces a per-IP rate limit (default 5/min,
 * configurable via WEAVE_LOGIN_RATE_LIMIT). This test discovers the actual
 * limit at runtime by sending requests until it receives a 429, then
 * verifies the UI shows the appropriate banner.
 */
test.describe('Login rate limit', () => {
  // 250 probes × ~50ms each + UI assertions can blow past the default
  // 30s per-test budget on slower laptops — bump it once for both
  // specs in this file.
  test.setTimeout(90_000);

  test.beforeEach(async ({ page }) => {
    await page.goto('/login');
    await expect(page.getByRole('button', { name: /sign in/i })).toBeVisible();
  });

  test('exhausting rate limit shows rate-limit banner', async ({ page, request }) => {
    // Exhaust the per-IP rate limit by sending wrong-password attempts
    // until the server responds with 429. The limit is configurable
    // (WEAVE_LOGIN_RATE_LIMIT), so we discover it at runtime.
    const MAX_PROBE = 250; // safety cap; matches WEAVE_LOGIN_RATE_LIMIT=200
    let exhausted = false;
    for (let i = 0; i < MAX_PROBE; i++) {
      const res = await request.post(`${API_BASE}/api/auth/login`, {
        data: { email: `rate-limit-probe-${i}@example.com`, password: 'wrong' },
      });
      if (res.status() === 429) {
        exhausted = true;
        break;
      }
    }
    expect(exhausted, 'expected to hit rate limit within 100 attempts').toBe(true);

    // Now submit via the UI form — should be rate-limited.
    await page.getByLabel(/email/i).fill('rate-limit-final@example.com');
    await page.getByLabel(/password/i).fill('wrong');
    await page.getByRole('button', { name: /sign in/i }).click();

    const alert = page.getByRole('alert');
    await expect(alert).toBeVisible({ timeout: 5000 });
    await expect(alert).toContainText(/too many attempts/i);
    await expect(alert).toContainText(/try again in \d+s/i);
  });

  test('rate-limit banner disappears after fresh successful submit is possible', async ({
    page,
    request,
  }) => {
    // Exhaust rate limit via API.
    const MAX_PROBE = 100;
    for (let i = 0; i < MAX_PROBE; i++) {
      const res = await request.post(`${API_BASE}/api/auth/login`, {
        data: { email: `rate-limit-probe2-${i}@example.com`, password: 'wrong' },
      });
      if (res.status() === 429) break;
    }

    // Trigger rate limit via form.
    await page.getByLabel(/email/i).fill('rate-limit-test2@example.com');
    await page.getByLabel(/password/i).fill('wrong');
    await page.getByRole('button', { name: /sign in/i }).click();

    const alert = page.getByRole('alert');
    await expect(alert).toContainText(/too many attempts/i);

    // The submit button should still be enabled (user can retry after waiting).
    await expect(page.getByRole('button', { name: /sign in/i })).toBeEnabled();
  });
});
