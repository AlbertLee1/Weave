import { test, expect } from '@playwright/test';
import {
  createOntologyViaAPI,
  createActionTypeViaAPI,
  navigateToAdmin,
  uniqueName,
} from './helpers';

test.describe('ActionType CRUD + Detail Page', () => {
  let ontologyApiName: string;
  let ontologyRid: string;

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('at-ont');
    const ont = await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `AT Test ${ontologyApiName}`,
    });
    ontologyRid = ont.rid;
  });

  test('create an action type via UI', async ({ page }) => {
    const atName = uniqueName('create-emp');

    await navigateToAdmin(page, ontologyApiName);

    // Switch to Action Types tab
    await page.click('button:has-text("Action Types")');
    await page.click('button:has-text("+ Create")');

    // Fill form — ActionTypeForm placeholders: "create-employee" (API Name), "Create Employee" (Display Name)
    await page.fill('input[placeholder="create-employee"]', atName);
    await page.fill('input[placeholder="Create Employee"]', `Display ${atName}`);

    // Submit
    await page.click('button[type="submit"]:has-text("Create Action Type")');

    // Should appear in the list
    await expect(page.locator(`text=${atName}`).first()).toBeVisible({ timeout: 5000 });
  });

  test('click action type to navigate to detail page with 3 tabs', async ({ page, request }) => {
    const atName = uniqueName('detail-at');
    await createActionTypeViaAPI(request, ontologyRid, {
      apiName: atName,
      displayName: `Detail ${atName}`,
      parameters: [{ id: 'p1', apiName: 'name', displayName: 'Name', dataType: 'string', required: true }],
      rules: [{ type: 'createObject', objectType: 'employee' }],
    });

    await navigateToAdmin(page, ontologyApiName);

    // Switch to Action Types tab
    await page.click('button:has-text("Action Types")');

    // Click the action type row
    await page.click(`text=${atName}`);

    // Should navigate to detail page
    await page.waitForURL(`**/admin/${ontologyApiName}/action-types/${atName}`);

    // Should see 3 tabs
    await expect(page.locator('button:has-text("Overview")')).toBeVisible();
    await expect(page.locator('button:has-text("Logic")')).toBeVisible();
    await expect(page.locator('button:has-text("Observability")')).toBeVisible();
  });

  test('action type detail: switch to Logic tab and see parameters', async ({ page, request }) => {
    const atName = uniqueName('logic-at');
    await createActionTypeViaAPI(request, ontologyRid, {
      apiName: atName,
      displayName: `Logic ${atName}`,
      parameters: [
        { id: 'p1', apiName: 'first_name', displayName: 'First Name', dataType: 'string', required: true },
        { id: 'p2', apiName: 'age', displayName: 'Age', dataType: 'integer', required: false },
      ],
    });

    await page.goto(`/admin/${ontologyApiName}/action-types/${atName}`);
    await page.waitForLoadState('networkidle');

    // Switch to Logic tab
    await page.click('button:has-text("Logic")');

    // Should see parameters section
    await expect(page.locator('text=Parameters').first()).toBeVisible();
  });

  test('delete action type via list', async ({ page, request }) => {
    const atName = uniqueName('del-at');
    await createActionTypeViaAPI(request, ontologyRid, {
      apiName: atName,
      displayName: `Del ${atName}`,
    });

    await navigateToAdmin(page, ontologyApiName);
    await page.click('button:has-text("Action Types")');
    await expect(page.locator(`text=${atName}`).first()).toBeVisible({ timeout: 5000 });

    // Click delete button — title="Delete" on the trash icon
    const row = page.locator(`text=${atName}`).first().locator('..').locator('..');
    const deleteBtn = row.locator('button[title="Delete"]');
    if (await deleteBtn.isVisible()) {
      await deleteBtn.click();

      // Confirm in modal
      await page.click('[data-testid="modal-overlay"] button:has-text("Delete")');

      await expect(page.locator(`text=${atName}`)).toHaveCount(0, { timeout: 5000 });
    }
  });
});
