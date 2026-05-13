import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/queries/:ontology` — the QueryTypes sandbox rendered by
 * `src/components/queries/QueryTypesSandboxPage.tsx` (US-055, PC-A14).
 *
 * Mirrors the four-state testid template established by US-021 onwards
 * (root / loading / error / empty) plus the right-pane sandbox surfaces:
 * the parameter form (auto-generated from the selected QueryType schema)
 * and the result panel with its Table / JSON tab pair.
 */
export class QueryTypesSandboxPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly empty: Locator;
  readonly list: Locator;
  readonly rows: Locator;
  readonly count: Locator;
  readonly detail: Locator;
  readonly detailEmpty: Locator;
  readonly displayName: Locator;
  readonly parameterForm: Locator;
  readonly executeButton: Locator;
  readonly running: Locator;
  readonly resultPanel: Locator;
  readonly resultTableTab: Locator;
  readonly resultJsonTab: Locator;
  readonly resultTable: Locator;
  readonly resultTableEmpty: Locator;
  readonly resultJson: Locator;
  readonly resultError: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('query-types-sandbox-page');
    this.loading = page.getByTestId('query-types-sandbox-loading');
    this.error = page.getByTestId('query-types-sandbox-error');
    this.empty = page.getByTestId('query-types-sandbox-empty');
    this.list = page.getByTestId('query-types-list');
    this.rows = page.locator('[data-testid="query-type-row"]');
    this.count = page.getByTestId('query-types-count');
    this.detail = page.getByTestId('query-type-detail');
    this.detailEmpty = page.getByTestId('query-type-detail-empty');
    this.displayName = page.getByTestId('query-type-display-name');
    this.parameterForm = page.getByTestId('query-type-parameter-form');
    this.executeButton = page.getByTestId('query-type-execute-button');
    this.running = page.getByTestId('query-type-running');
    this.resultPanel = page.getByTestId('query-result-panel');
    this.resultTableTab = page.getByTestId('query-result-tab-table');
    this.resultJsonTab = page.getByTestId('query-result-tab-json');
    this.resultTable = page.getByTestId('query-result-table');
    this.resultTableEmpty = page.getByTestId('query-result-table-empty');
    this.resultJson = page.getByTestId('query-result-json');
    this.resultError = page.getByTestId('query-result-error');
  }

  async goto(ontology: string): Promise<void> {
    await this.page.goto(`/queries/${encodeURIComponent(ontology)}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  rowByApiName(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="query-type-row"][data-query-api-name="${apiName}"]`,
    );
  }

  selectButton(apiName: string): Locator {
    return this.page.getByTestId(`query-type-select-${apiName}`);
  }

  paramInput(name: string): Locator {
    return this.page.locator(`#param-${name}`);
  }

  resultColumns(): Locator {
    return this.page.locator('[data-testid^="query-result-column-"]');
  }

  resultRows(): Locator {
    return this.page.locator('[data-testid="query-result-row"]');
  }
}
