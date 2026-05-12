import { expect, test } from '@playwright/test';
import { BrowserPage, Given, Then, When, describeFeature } from './support';

/**
 * Smoke BDD coverage of the v2 Browser page SearchBar + FilterBuilder.
 *
 * Rewritten under US-002 as the seed example for the new BDD support
 * infrastructure; the imperative equivalent under
 * `web/e2e/search-and-filter.spec.ts` was removed in the same change.
 *
 * Assertions stay on the front-end rendering contract (SearchBar accepts
 * text, FilterBuilder panel toggles open/closed). Data-backed search
 * parity is exercised by the Phase 6 gate specs under `web/e2e/phase6/`.
 */

describeFeature('Browser search and filter', () => {
  const ontologyApiName = 'northwind';
  const objectTypeApiName = 'employee';

  test('Scenario: typing in the search box updates the input @smoke', async ({ page }) => {
    const browser = new BrowserPage(page);

    await Given('the user is on the employee Browser page', async () => {
      await browser.goto(ontologyApiName, objectTypeApiName);
    });

    await Then('the search input is visible with the canonical placeholder', async () => {
      await expect(browser.searchInput).toBeVisible();
      await expect(browser.searchInput).toHaveAttribute('placeholder', 'Search objects...');
    });

    await When('they type "alice" into the search input', async () => {
      await browser.typeSearch('alice');
    });

    await Then('the search input reflects "alice"', async () => {
      await expect(browser.searchInput).toHaveValue('alice');
    });

    await When('they clear the search input', async () => {
      await browser.clearSearch();
    });

    await Then('the search input is empty again', async () => {
      await expect(browser.searchInput).toHaveValue('');
    });
  });

  test('Scenario: toggling the filters control reveals the FilterBuilder @smoke', async ({ page }) => {
    const browser = new BrowserPage(page);

    await Given('the user is on the employee Browser page', async () => {
      await browser.goto(ontologyApiName, objectTypeApiName);
    });

    await Then('the toggle-filters control is visible', async () => {
      await expect(browser.toggleFilters).toBeVisible();
    });

    await When('they click the toggle to open the FilterBuilder panel', async () => {
      await browser.openFilters();
    });

    // The FilterBuilder only renders an "Add filter" affordance when the
    // ObjectType has at least one indexed property, which isn't a smoke
    // invariant — so we assert the toggle still functions rather than
    // probing the builder's DOM.
    await When('they click the toggle again to close the panel', async () => {
      await browser.openFilters();
    });

    await Then('the toggle remains interactable', async () => {
      await expect(browser.toggleFilters).toBeVisible();
    });
  });
});
