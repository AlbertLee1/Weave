import { test, expect } from '@playwright/test';
import {
  createOntologyViaAPI,
  createObjectTypeViaAPI,
  navigateToAdmin,
  uniqueName,
} from './helpers';

test.describe('Search and Filter', () => {
  let ontologyApiName: string;
  let ontologyRid: string;

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('search-ont');
    const ont = await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `Search Test ${ontologyApiName}`,
    });
    ontologyRid = ont.rid;

    // Create several object types for filtering
    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: 'employee',
      displayName: 'Employee',
      primaryKey: 'id',
    });
    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: 'department',
      displayName: 'Department',
      primaryKey: 'id',
    });
    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: 'customer',
      displayName: 'Customer',
      primaryKey: 'id',
    });
  });

  test('filter object types list by text', async ({ page }) => {
    await navigateToAdmin(page, ontologyApiName);

    // All 3 should be visible initially
    await expect(page.locator('text=employee').first()).toBeVisible();
    await expect(page.locator('text=department').first()).toBeVisible();
    await expect(page.locator('text=customer').first()).toBeVisible();

    // Type in the filter input — placeholder is "Filter..."
    await page.fill('input[placeholder="Filter..."]', 'emp');

    // Only employee should be visible in the list
    await expect(page.locator('.flex.flex-col.gap-2').locator('text=employee').first()).toBeVisible();
    await expect(page.locator('.flex.flex-col.gap-2').getByText('department', { exact: true })).toHaveCount(0);
    await expect(page.locator('.flex.flex-col.gap-2').getByText('customer', { exact: true })).toHaveCount(0);

    // Clear filter
    await page.fill('input[placeholder="Filter..."]', '');

    // All should be visible again
    await expect(page.locator('text=employee').first()).toBeVisible();
    await expect(page.locator('text=department').first()).toBeVisible();
  });

  test('Cmd+K opens command palette', async ({ page }) => {
    await navigateToAdmin(page, ontologyApiName);

    // Press Cmd+K
    await page.keyboard.press('Meta+k');

    // Command palette should be visible — it has a search input with placeholder
    // "Search ontologies, object types, links, actions..."
    await expect(
      page.locator('input[placeholder*="Search ontologies"]'),
    ).toBeVisible({ timeout: 3000 });

    // Type to search
    const searchInput = page.locator('input[placeholder*="Search ontologies"]');
    await searchInput.fill('employee');

    // Should show results
    await expect(page.locator('text=employee').first()).toBeVisible();

    // Escape to close
    await page.keyboard.press('Escape');
  });
});
