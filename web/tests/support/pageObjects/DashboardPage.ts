import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/` — the Dashboard landing page rendered by
 * `src/components/dashboard/DashboardPage.tsx`.
 *
 * The Dashboard composes three regions: a hero header with the stats bar
 * (`StatsBar`, with `stat-ontologies` / `stat-object-types` cells), the
 * "Ontologies" section heading, and the ontology grid (or an empty state
 * when no ontologies are registered). Locators here cover only what the
 * BDD spec interacts with — new scenarios should add locators on demand
 * rather than fattening this surface preemptively.
 *
 * Selectors prefer `data-testid` per the convention pinned in
 * `web/e2e/README.md` and reinforced by US-002.
 */
export class DashboardPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly statsBar: Locator;
  readonly statOntologies: Locator;
  readonly statObjectTypes: Locator;
  readonly ontologyGrid: Locator;
  readonly emptyState: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('dashboard-page');
    this.loading = page.getByTestId('dashboard-loading');
    this.error = page.getByTestId('dashboard-error');
    this.statsBar = page.getByTestId('dashboard-stats-bar');
    this.statOntologies = page
      .getByTestId('stat-ontologies')
      .getByTestId('stat-value');
    this.statObjectTypes = page
      .getByTestId('stat-object-types')
      .getByTestId('stat-value');
    this.ontologyGrid = page.getByTestId('dashboard-ontology-grid');
    this.emptyState = page.getByTestId('dashboard-empty-state');
  }

  async goto(): Promise<void> {
    await this.page.goto('/');
    await this.page.waitForLoadState('domcontentloaded');
  }

  /** Locator for a single ontology card identified by its apiName. */
  ontologyCard(apiName: string): Locator {
    return this.page
      .getByTestId('dashboard-ontology-card-wrapper')
      .filter({ has: this.page.locator(`[data-ontology-api-name="${apiName}"]`) })
      .or(this.page.locator(`[data-ontology-api-name="${apiName}"]`));
  }

  /** Click an ontology card to navigate into its Explorer page. */
  async openOntology(apiName: string): Promise<void> {
    await this.page
      .locator(`[data-ontology-api-name="${apiName}"]`)
      .getByRole('button', { name: /open ontology/i })
      .first()
      .click();
  }
}
