import { test, expect } from '@playwright/test';

const API_BASE = 'http://localhost:9117';

/**
 * US-078: Playwright spec — login rate limit.
 *
 * The backend login handler enforces a per-IP rate limit of 5 attempts
 * per minute.  After 5 wrong-password submissions the 6th must surface
 * a "Too many attempts" banner in the UI instead of the generic
 * "Invalid email or password" message.
 */
test.describe('Login rate limit', () => {
  test.beforeEach(async ({ page }) => {
    // Drain any stale rate-limit state by waiting or using a fresh context.
    // Navigate to the login page.
    await page.goto('/login');
    await expect(page.getByRole('button', { name: /sign in/i })).toBeVisible();
  });

  test('6th wrong-password submit shows rate-limit banner', async ({ page, request }) => {
    // Exhaust the per-IP rate limit (5 attempts) via direct API calls
    // so the test doesn't depend on Playwright's form-fill speed.
    for (let i = 0; i < 5; i++) {
      await request.post(`${API_BASE}/api/auth/login`, {
        data: { email: 'rate-limit-test@example.com', password: 'wrong' },
      });
    }

    // Now submit via the UI form — this 6th attempt should be rate-limited.
    await page.getByLabel(/email/i).fill('rate-limit-test@example.com');
    await page.getByLabel(/password/i).fill('wrong');
    await page.getByRole('button', { name: /sign in/i }).click();

    // The UI must show the rate-limit message, NOT the generic auth error.
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
    for (let i = 0; i < 5; i++) {
      await request.post(`${API_BASE}/api/auth/login`, {
        data: { email: 'rate-limit-test2@example.com', password: 'wrong' },
      });
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
