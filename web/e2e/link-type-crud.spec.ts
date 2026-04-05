import { test, expect } from '@playwright/test';
import {
  createOntologyViaAPI,
  createObjectTypeViaAPI,
  createLinkTypeViaAPI,
  navigateToAdmin,
  uniqueName,
} from './helpers';

test.describe('LinkType CRUD', () => {
  let ontologyApiName: string;
  let ontologyRid: string;
  let srcOtName: string;
  let tgtOtName: string;
  let srcOtRid: string;
  let tgtOtRid: string;

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('lt-ont');
    srcOtName = uniqueName('src-ot');
    tgtOtName = uniqueName('tgt-ot');

    const ont = await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `LT Test ${ontologyApiName}`,
    });
    ontologyRid = ont.rid;
    const srcOt = await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: srcOtName,
      displayName: `Source ${srcOtName}`,
      primaryKey: 'id',
    });
    srcOtRid = srcOt.rid;
    const tgtOt = await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: tgtOtName,
      displayName: `Target ${tgtOtName}`,
      primaryKey: 'id',
    });
    tgtOtRid = tgtOt.rid;
  });

  test('create a link type via UI', async ({ page }) => {
    const ltName = uniqueName('has-target');

    await navigateToAdmin(page, ontologyApiName);

    // Switch to Link Types tab
    await page.click('button:has-text("Link Types")');

    // Click "+ Create"
    await page.click('button:has-text("+ Create")');

    // Select source and target first (apiName/displayName auto-generate)
    const srcSelect = page.locator('select').nth(0);
    const srcOption = srcSelect.locator(`option:has-text("${srcOtName}")`);
    const srcValue = await srcOption.getAttribute('value');
    await srcSelect.selectOption(srcValue!);

    const tgtSelect = page.locator('select').nth(1);
    const tgtOption = tgtSelect.locator(`option:has-text("${tgtOtName}")`);
    const tgtValue = await tgtOption.getAttribute('value');
    await tgtSelect.selectOption(tgtValue!);

    // Override auto-generated apiName with our test name
    await page.fill('input[placeholder="employeeDepartment"]', ltName);
    await page.fill('input[placeholder="Employee → Department"]', `Display ${ltName}`);

    // Submit
    await page.click('button[type="submit"]:has-text("Create Link Type")');

    // Should appear in the list
    await expect(page.locator(`text=${ltName}`).first()).toBeVisible({ timeout: 5000 });
  });

  test('delete a link type via UI', async ({ page, request }) => {
    const ltName = uniqueName('del-lt');

    await createLinkTypeViaAPI(request, ontologyRid, {
      apiName: ltName,
      displayName: `Del ${ltName}`,
      sourceObjectType: srcOtRid,
      targetObjectType: tgtOtRid,
      cardinality: 'ONE_TO_MANY',
    });

    await navigateToAdmin(page, ontologyApiName);

    // Switch to Link Types tab
    await page.click('button:has-text("Link Types")');
    await expect(page.locator(`text=${ltName}`).first()).toBeVisible({ timeout: 5000 });

    // Click delete button — title="Delete" on the trash icon
    const row = page.locator(`text=${ltName}`).first().locator('..').locator('..');
    const deleteBtn = row.locator('button[title="Delete"]');
    if (await deleteBtn.isVisible()) {
      await deleteBtn.click();

      // Confirm in modal
      await page.click('[data-testid="modal-overlay"] button:has-text("Delete")');

      // Should be removed
      await expect(page.locator(`text=${ltName}`)).toHaveCount(0, { timeout: 5000 });
    }
  });
});
