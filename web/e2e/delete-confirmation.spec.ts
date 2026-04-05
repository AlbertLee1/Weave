import { test, expect } from '@playwright/test';
import {
  createOntologyViaAPI,
  createObjectTypeViaAPI,
  navigateToAdmin,
  uniqueName,
} from './helpers';

test.describe('Delete Confirmation Flow', () => {
  let ontologyApiName: string;
  let ontologyRid: string;

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('del-ont');
    const ont = await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `Delete Test ${ontologyApiName}`,
    });
    ontologyRid = ont.rid;
  });

  test('delete object type shows confirmation modal', async ({ page, request }) => {
    const otName = uniqueName('del-confirm-ot');
    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: otName,
      displayName: `Del ${otName}`,
      primaryKey: 'id',
    });

    await navigateToAdmin(page, ontologyApiName);

    // Find the object type and click delete
    await expect(page.locator(`text=${otName}`).first()).toBeVisible({ timeout: 5000 });

    // Click trash icon — title="Delete" on the SVG button
    const row = page.locator(`text=${otName}`).first().locator('..').locator('..');
    const deleteBtn = row.locator('button[title="Delete"]');
    await deleteBtn.click();

    // Should see confirmation modal — Modal title is "Confirm Delete"
    await expect(page.locator('text=Confirm Delete')).toBeVisible();
    await expect(page.locator(`[data-testid="modal-overlay"] >> text=${otName}`).first()).toBeVisible();

    // Cancel
    await page.click('button:has-text("Cancel")');

    // Object type should still be there
    await expect(page.locator(`text=${otName}`).first()).toBeVisible();
  });

  test('confirm delete removes the object type', async ({ page, request }) => {
    const otName = uniqueName('del-real-ot');
    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: otName,
      displayName: `DelReal ${otName}`,
      primaryKey: 'id',
    });

    await navigateToAdmin(page, ontologyApiName);
    await expect(page.locator(`text=${otName}`).first()).toBeVisible({ timeout: 10000 });

    // Click delete
    const row = page.locator(`text=${otName}`).first().locator('..').locator('..');
    const deleteBtn = row.locator('button[title="Delete"]');
    await deleteBtn.click();

    // Confirm — click the "Delete" button inside the modal overlay
    await page.click('[data-testid="modal-overlay"] button:has-text("Delete")');

    // Object type should be gone
    await expect(page.locator(`text=${otName}`)).toHaveCount(0, { timeout: 5000 });
  });
});
