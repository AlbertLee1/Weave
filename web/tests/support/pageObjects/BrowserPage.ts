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
  readonly timeTravelToolbar: Locator;
  readonly timeTravelToggle: Locator;
  readonly timeTravelToggleLabel: Locator;
  readonly timeTravelPicker: Locator;
  readonly timeTravelHintBanner: Locator;
  readonly timeTravelActiveBadge: Locator;
  readonly liveToggle: Locator;

  constructor(page: Page) {
    this.page = page;
    this.searchInput = page.getByTestId('search-input');
    this.toggleFilters = page.getByTestId('toggle-filters');
    this.timeTravelToolbar = page.getByTestId('time-travel-toolbar');
    this.timeTravelToggle = page.getByTestId('time-travel-toggle');
    this.timeTravelToggleLabel = page.getByTestId('time-travel-toggle-label');
    this.timeTravelPicker = page.getByTestId('time-travel-picker');
    this.timeTravelHintBanner = page.getByTestId('time-travel-hint-banner');
    this.timeTravelActiveBadge = page.getByTestId('time-travel-active-badge');
    this.liveToggle = page.getByTestId('live-toggle');
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

  async selectTimeTravelTx(txId: string): Promise<void> {
    await this.timeTravelPicker.selectOption(txId);
  }

  async toggleTimeTravel(): Promise<void> {
    // The visible toggle pill is rendered by a `sr-only` checkbox plus a
    // sibling visual <span> (pointer-events-none) — clicking the wrapping
    // <label> fires the React onChange handler the same way a user click
    // on the pill would. We click the label rather than the input so
    // Playwright's overlay/visibility checks aren't fighting the screen-
    // reader-only positioning of the input itself.
    await this.timeTravelToggleLabel.click();
  }
}
