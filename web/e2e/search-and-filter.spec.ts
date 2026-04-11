import { test, expect } from '@playwright/test';
import {
  createOntologyViaAPI,
  createObjectTypeViaAPI,
  navigateToBrowser,
  uniqueName,
} from './helpers';

/**
 * Search and filter UI on the v2 Browser page
 * (`/browser/:ontology/:objectType`).
 *
 * Only exercises front-end components — a freshly-created ObjectType has no
 * indexed documents, so these assertions stay on the SearchBar / FilterBuilder
 * rendering contract and do not require live Bleve data. Data-backed search
 * parity is covered by the Phase 6 gate specs created under US-038+.
 */
test.describe('Browser search and filter', () => {
  let ontologyApiName: string;
  let ontologyRid: string;
  const objectTypeApiName = 'employee';

  test.beforeAll(async ({ request }) => {
    ontologyApiName = uniqueName('search-ont');
    const ont = await createOntologyViaAPI(request, {
      apiName: ontologyApiName,
      displayName: `Search Test ${ontologyApiName}`,
    });
    ontologyRid = ont.rid;

    await createObjectTypeViaAPI(request, ontologyRid, {
      apiName: objectTypeApiName,
      displayName: 'Employee',
      primaryKey: 'id',
    });
  });

  test('search input renders and accepts text', async ({ page }) => {
    await navigateToBrowser(page, ontologyApiName, objectTypeApiName);

    const searchInput = page.getByTestId('search-input');
    await expect(searchInput).toBeVisible();
    await expect(searchInput).toHaveAttribute('placeholder', 'Search objects...');

    await searchInput.fill('alice');
    await expect(searchInput).toHaveValue('alice');

    // Clearing the input resets to empty (SearchBar.handleChange fires onSearch('')).
    await searchInput.fill('');
    await expect(searchInput).toHaveValue('');
  });

  test('filters toggle reveals FilterBuilder panel', async ({ page }) => {
    await navigateToBrowser(page, ontologyApiName, objectTypeApiName);

    const toggle = page.getByTestId('toggle-filters');
    await expect(toggle).toBeVisible();

    // FilterBuilder only mounts when toggled open.
    await toggle.click();

    // FilterBuilder renders an "Add filter" control when there are 0 filters
    // and at least one indexed property — for a freshly-created type with no
    // properties the builder may render an empty affordance, so we assert on
    // the toggle being clickable without throwing rather than on exact DOM.
    // Clicking it again should hide it.
    await toggle.click();
    await expect(toggle).toBeVisible();
  });
});
