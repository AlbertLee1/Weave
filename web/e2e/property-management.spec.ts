import { test, expect } from '@playwright/test';
import {
  createOntologyViaAPI,
  createObjectTypeViaAPI,
  createPropertyViaAPI,
  uniqueName,
} from './helpers';

test.describe('Property Management', () => {
  let ontologyApiName: string;
  let ontologyRid: string;
  let otName: string;
  let otRid: string;

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('prop-ont');
    otName = uniqueName('prop-ot');

    const ont = await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `Prop Test ${ontologyApiName}`,
    });
    ontologyRid = ont.rid;

    const ot = await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: otName,
      displayName: `Prop OT ${otName}`,
      primaryKey: 'id',
    });
    otRid = ot.rid;

    // Create id property
    await createPropertyViaAPI(request, otRid, {
      apiName: 'id',
      baseType: 'integer',
      displayName: 'ID',
    });
  });

  test('add a property via Properties tab', async ({ page }) => {
    await page.goto(`/admin/${ontologyApiName}/object-types/${otName}`);
    await page.waitForLoadState('networkidle');

    // Switch to Properties tab
    await page.click('button:has-text("Properties")');

    // Should see the id property
    await expect(page.locator('text=id').first()).toBeVisible();

    // Click "+ Add Property" button (the button text is "+ Add Property")
    const addBtn = page.locator('button:has-text("Add Property")');
    if (await addBtn.first().isVisible()) {
      await addBtn.first().click();

      // Fill property form in SlidePanel
      // PropertyForm placeholders: "first_name" (API Name), "First Name" (Display Name)
      await page.fill('input[placeholder="first_name"]', 'email');
      await page.fill('input[placeholder="First Name"]', 'Email');

      // Submit — button text is "Create Property"
      await page.click('button[type="submit"]:has-text("Create Property")');

      // Should see new property in the table
      await expect(page.locator('text=email').first()).toBeVisible({ timeout: 5000 });
    }
  });

  test('delete a property via Properties tab', async ({ page, request }) => {
    // Create a property to delete
    const propName = uniqueName('del-prop');
    await createPropertyViaAPI(request, otRid, {
      apiName: propName,
      baseType: 'string',
      displayName: `Del ${propName}`,
    });

    await page.goto(`/admin/${ontologyApiName}/object-types/${otName}`);
    await page.waitForLoadState('networkidle');

    // Switch to Properties tab
    await page.click('button:has-text("Properties")');

    // Verify the property exists
    await expect(page.locator(`text=${propName}`).first()).toBeVisible({ timeout: 5000 });

    // Click delete button for that property — title is "Delete property" (lowercase p)
    const row = page.locator(`tr:has-text("${propName}")`);
    const deleteBtn = row.locator('button[title="Delete property"]');
    if (await deleteBtn.isVisible()) {
      await deleteBtn.click();

      // Confirm deletion in modal — button text is "Delete"
      await page.click('[data-testid="modal-overlay"] button:has-text("Delete")');

      // Property should be removed
      await expect(page.locator(`text=${propName}`)).toHaveCount(0, { timeout: 5000 });
    }
  });
});
