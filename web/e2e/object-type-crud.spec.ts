import { test, expect } from '@playwright/test';
import {
  createOntologyViaAPI,
  createObjectTypeViaAPI,
  navigateToAdmin,
  uniqueName,
} from './helpers';

test.describe('ObjectType CRUD', () => {
  let ontologyApiName: string;
  let ontologyRid: string;

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('ot-ont');
    const ont = await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `OT Test ${ontologyApiName}`,
    });
    ontologyRid = ont.rid;
  });

  test('create an object type via UI', async ({ page }) => {
    const otName = uniqueName('emp');

    await navigateToAdmin(page, ontologyApiName);

    // Click "+ Create" on Object Types tab
    await page.click('button:has-text("+ Create")');

    // Step 1: Fill display name (apiName auto-generates)
    await page.fill('input[placeholder="Employee"]', `Display ${otName}`);
    // Override auto-generated apiName
    await page.fill('input[placeholder="employee"]', otName);

    // Click Next to go to Step 2
    await page.click('button:has-text("Next")');

    // Step 2: Primary Key defaults to "id", override it
    await page.fill('input[placeholder="id"]', 'pk');

    // Submit (use type=submit inside the modal to avoid matching "+ Create" button)
    await page.locator('[data-testid="modal-overlay"] button[type="submit"]').click();

    // Should appear in the list
    await expect(page.locator(`text=${otName}`).first()).toBeVisible({ timeout: 5000 });
  });

  test('click an object type to navigate to detail page', async ({ page, request }) => {
    const otName = uniqueName('detail-ot');
    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: otName,
      displayName: `Detail ${otName}`,
      primaryKey: 'id',
    });

    await navigateToAdmin(page, ontologyApiName);

    // Click the object type row
    await page.click(`text=${otName}`);

    // Should navigate to detail page with tabs
    await page.waitForURL(`**/admin/${ontologyApiName}/object-types/${otName}`);
    await expect(page.locator('button:has-text("Overview")')).toBeVisible();
    await expect(page.locator('button:has-text("Properties")')).toBeVisible();
    await expect(page.locator('button:has-text("Links")')).toBeVisible();
    await expect(page.locator('button:has-text("Actions")')).toBeVisible();
    await expect(page.locator('button:has-text("Settings")')).toBeVisible();
  });

  test('detail page: edit metadata on Overview tab', async ({ page, request }) => {
    const otName = uniqueName('edit-ot');
    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: otName,
      displayName: `Edit ${otName}`,
      primaryKey: 'id',
    });

    await page.goto(`/admin/${ontologyApiName}/object-types/${otName}`);
    await page.waitForLoadState('networkidle');

    // Should be on Overview tab by default
    const descInput = page.locator('textarea').first();
    if (await descInput.isVisible()) {
      await descInput.fill('Updated description');
      await page.click('button:has-text("Save")');
    }
  });

  test('detail page: switch to Settings tab', async ({ page, request }) => {
    const otName = uniqueName('settings-ot');
    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: otName,
      displayName: `Settings ${otName}`,
      primaryKey: 'id',
    });

    await page.goto(`/admin/${ontologyApiName}/object-types/${otName}`);
    await page.waitForLoadState('networkidle');

    // Switch to Settings tab
    await page.click('button:has-text("Settings")');

    // Should see status and visibility dropdowns
    await expect(page.locator('select').first()).toBeVisible();
  });
});
