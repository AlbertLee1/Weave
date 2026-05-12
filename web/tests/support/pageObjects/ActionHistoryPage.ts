import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/actions/:ontology/history` — the Action History page
 * rendered by `src/components/actions/ActionHistoryPage.tsx`.
 *
 * Locators target the testid matrix added on the page (root / loading /
 * error / empty / filters / per-row + per-button affordances). New
 * scenarios should add locators on demand rather than fattening this
 * surface preemptively (same convention as DashboardPage).
 */
export class ActionHistoryPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly empty: Locator;
  readonly noOntology: Locator;
  readonly filters: Locator;
  readonly filterActionType: Locator;
  readonly filterStatusAll: Locator;
  readonly filterStatusSuccess: Locator;
  readonly filterStatusFailed: Locator;
  readonly filterUserId: Locator;
  readonly list: Locator;
  readonly rows: Locator;
  readonly detail: Locator;
  readonly modalOverlay: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('action-history-page');
    this.loading = page.getByTestId('action-history-loading');
    this.error = page.getByTestId('action-history-error');
    this.empty = page.getByTestId('action-history-empty');
    this.noOntology = page.getByTestId('action-history-no-ontology');
    this.filters = page.getByTestId('action-history-filters');
    this.filterActionType = page.getByTestId('filter-action-type');
    this.filterStatusAll = page.getByTestId('filter-status-all');
    this.filterStatusSuccess = page.getByTestId('filter-status-success');
    this.filterStatusFailed = page.getByTestId('filter-status-failed');
    this.filterUserId = page.getByTestId('filter-user-id');
    this.list = page.getByTestId('action-history-list');
    this.rows = page.getByTestId('action-history-row');
    this.detail = page.getByTestId('action-history-detail');
    this.modalOverlay = page.getByTestId('modal-overlay');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(
      `/actions/${encodeURIComponent(ontologyApiName)}/history`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  rowByLogId(logId: number | string): Locator {
    return this.page.locator(`[data-testid="action-history-row"][data-log-id="${logId}"]`);
  }

  undoButton(logId: number | string): Locator {
    return this.page.locator(`[data-testid="undo-btn"][data-log-id="${logId}"]`);
  }

  viewDetailButton(logId: number | string): Locator {
    return this.rowByLogId(logId).getByTestId('view-detail-btn');
  }
}
