import { test, expect } from '@playwright/test';
import { createOntologyViaAPI, navigateToAdmin, uniqueName } from './helpers';

test.describe('Interface Management', () => {
  let ontologyApiName: string;

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('iface-ont');
    await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `Interface Test ${ontologyApiName}`,
    });
  });

  test('navigate to Interfaces tab and see empty state', async ({ page }) => {
    await navigateToAdmin(page, ontologyApiName);

    // Switch to Interfaces tab
    await page.click('button:has-text("Interfaces")');

    // Should see empty state or interface list
    // InterfaceListPage shows "No Interfaces" as EmptyState title, or "+ Create Interface" button
    await expect(
      page.locator('text=No Interfaces').or(page.locator('text=Create Interface')).first(),
    ).toBeVisible({ timeout: 5000 });
  });

  test('create an interface via UI', async ({ page }) => {
    const ifaceName = uniqueName('geo-entity');

    await navigateToAdmin(page, ontologyApiName);
    await page.click('button:has-text("Interfaces")');

    // Click "+ Create Interface" button
    await page.click('button:has-text("Create Interface")');

    // Fill form in modal
    // InterfaceListPage form placeholders: "e.g. GeoLocatable" (API Name), "e.g. Geo-Locatable" (Display Name)
    await page.fill('input[placeholder="e.g. GeoLocatable"]', ifaceName);
    await page.fill('input[placeholder="e.g. Geo-Locatable"]', `Display ${ifaceName}`);

    // Submit — button text is "Create"
    await page.click('[data-testid="modal-overlay"] button[type="submit"]:has-text("Create")');

    // Should appear in the list
    await expect(page.locator(`text=${ifaceName}`).first()).toBeVisible({ timeout: 5000 });
  });
});
