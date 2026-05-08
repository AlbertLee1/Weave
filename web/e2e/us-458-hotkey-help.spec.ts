// US-458: Playwright e2e for the global hotkey help modal.
//
// Pressing `?` (Shift+/) anywhere in the app opens the keyboard
// shortcut help modal grouped into Navigation / Editing / Search.

import { test, expect, type APIRequestContext } from '@playwright/test';

const API_BASE = 'http://localhost:9117';

async function backendReachable(request: APIRequestContext): Promise<boolean> {
  try {
    const res = await request.get(`${API_BASE}/health`, { timeout: 1500 });
    return res.ok();
  } catch {
    return false;
  }
}

test.describe('US-458 — Hotkey Help Modal', () => {
  test('? opens the help modal with Navigation/Editing/Search groups', async ({
    page,
    request,
  }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');

    await page.goto('/');
    // Click the body so focus is somewhere a global hotkey can fire.
    await page.locator('body').click();
    await page.keyboard.press('Shift+/');

    const modal = page.getByTestId('hotkey-help-modal');
    await expect(modal).toBeVisible({ timeout: 5_000 });

    await expect(page.getByTestId('hotkey-group-navigation')).toBeVisible();
    await expect(page.getByTestId('hotkey-group-editing')).toBeVisible();
    await expect(page.getByTestId('hotkey-group-search')).toBeVisible();

    // Spot-check that one entry per group renders with its description.
    await expect(page.getByTestId('hotkey-row-commandPalette')).toBeVisible();
    await expect(page.getByTestId('hotkey-row-submitForm')).toBeVisible();
    await expect(page.getByTestId('hotkey-row-goDashboard')).toBeVisible();
  });

  test('Escape closes the help modal', async ({ page, request }) => {
    test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');

    await page.goto('/');
    await page.locator('body').click();
    await page.keyboard.press('Shift+/');
    await expect(page.getByTestId('hotkey-help-modal')).toBeVisible({ timeout: 5_000 });

    await page.keyboard.press('Escape');
    await expect(page.getByTestId('hotkey-help-modal')).not.toBeVisible();
  });
});
