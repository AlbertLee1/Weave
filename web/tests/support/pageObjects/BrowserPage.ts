import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/browser/:ontology/:objectType` — the v2 Browser page
 * where SearchBar and FilterBuilder live.
 *
 * Locators are deliberately limited to elements the BDD smoke specs
 * interact with today; new scenarios should add locators on demand rather
 * than over-fitting up front. Selectors prefer `data-testid` per the
 * convention documented in `web/e2e/README.md`.
 */
export class BrowserPage {
  readonly page: Page;
  readonly searchInput: Locator;
  readonly toggleFilters: Locator;

  constructor(page: Page) {
    this.page = page;
    this.searchInput = page.getByTestId('search-input');
    this.toggleFilters = page.getByTestId('toggle-filters');
  }

  async goto(ontologyApiName: string, objectTypeApiName: string): Promise<void> {
    await this.page.goto(`/browser/${ontologyApiName}/${objectTypeApiName}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  async typeSearch(value: string): Promise<void> {
    await this.searchInput.fill(value);
  }

  async clearSearch(): Promise<void> {
    await this.searchInput.fill('');
  }

  async openFilters(): Promise<void> {
    await this.toggleFilters.click();
  }
}
