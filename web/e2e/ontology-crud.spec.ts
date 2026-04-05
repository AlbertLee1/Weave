import { test, expect } from '@playwright/test';
import { navigateToAdmin, uniqueName } from './helpers';

test.describe('Ontology CRUD', () => {
  test('create a new ontology and see it in the sidebar', async ({ page }) => {
    const name = uniqueName('test-ont');

    await navigateToAdmin(page);

    // Click "+ New" button in the sidebar
    await page.click('button:has-text("+ New")');

    // Fill the form — OntologyForm placeholders: "my-ontology", "My Ontology"
    await page.fill('input[placeholder="my-ontology"]', name);
    await page.fill('input[placeholder="My Ontology"]', `Display ${name}`);

    // Submit
    await page.click('button[type="submit"]:has-text("Create Ontology")');

    // Wait for modal to close and ontology to appear in sidebar
    await expect(page.locator(`text=${name}`).first()).toBeVisible({ timeout: 5000 });
  });

  test('select an ontology and see tabs', async ({ page, request }) => {
    const name = uniqueName('ont-tabs');
    await request.post('http://localhost:8080/api/admin/ontologies', {
      data: { apiName: name, displayName: `Tabs ${name}` },
    });

    await navigateToAdmin(page);

    // Click the ontology in sidebar
    await page.click(`text=${name}`);

    // Should see tabs
    await expect(page.locator('button:has-text("Object Types")')).toBeVisible();
    await expect(page.locator('button:has-text("Link Types")')).toBeVisible();
    await expect(page.locator('button:has-text("Action Types")')).toBeVisible();
    await expect(page.locator('button:has-text("Interfaces")')).toBeVisible();
  });
});
