import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/audit` — the global Audit Report rendered by
 * `src/components/audit/AuditReportPage.tsx` (US-045, PC-A11).
 *
 * The page renders four mutually-exclusive list states (loading /
 * error / empty / loaded) above a paginated table whose rows expand
 * to reveal the underlying `diff_json` payload. Two export buttons
 * (CSV / JSON) flush the currently-loaded events into a blob
 * download.
 */
export class AuditReportPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly emptyState: Locator;
  readonly list: Locator;
  readonly filters: Locator;
  readonly applyButton: Locator;
  readonly clearButton: Locator;
  readonly exportCsvButton: Locator;
  readonly exportJsonButton: Locator;
  readonly exportStatus: Locator;
  readonly loadMoreButton: Locator;
  readonly endMarker: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('audit-report-page');
    this.loading = page.getByTestId('audit-report-loading');
    this.errorState = page.getByTestId('audit-report-error');
    this.emptyState = page.getByTestId('audit-report-empty');
    this.list = page.getByTestId('audit-report-list');
    this.filters = page.getByTestId('audit-report-filters');
    this.applyButton = page.getByTestId('audit-report-filter-apply-btn');
    this.clearButton = page.getByTestId('audit-report-filter-clear-btn');
    this.exportCsvButton = page.getByTestId('audit-report-export-csv-btn');
    this.exportJsonButton = page.getByTestId('audit-report-export-json-btn');
    this.exportStatus = page.getByTestId('audit-report-export-status');
    this.loadMoreButton = page.getByTestId('audit-report-load-more-btn');
    this.endMarker = page.getByTestId('audit-report-end-marker');
  }

  async goto(): Promise<void> {
    await this.page.goto('/audit');
    await this.page.waitForLoadState('domcontentloaded');
  }

  actorInput(): Locator {
    return this.page.getByTestId('audit-report-filter-actor');
  }
  actionInput(): Locator {
    return this.page.getByTestId('audit-report-filter-action');
  }
  resourceTypeInput(): Locator {
    return this.page.getByTestId('audit-report-filter-resource-type');
  }
  sinceInput(): Locator {
    return this.page.getByTestId('audit-report-filter-since');
  }
  untilInput(): Locator {
    return this.page.getByTestId('audit-report-filter-until');
  }

  rows(): Locator {
    return this.page.locator('[data-testid="audit-report-row"]');
  }
  rowById(id: string): Locator {
    return this.page.locator(
      `[data-testid="audit-report-row"][data-audit-id="${id}"]`,
    );
  }
  expandButtonFor(id: string): Locator {
    return this.page.locator(
      `[data-testid="audit-report-row-expand-btn"][data-audit-id="${id}"]`,
    );
  }
  payloadFor(id: string): Locator {
    return this.page.locator(
      `[data-testid="audit-report-row-payload"][data-audit-id="${id}"]`,
    );
  }
  payloadJsonFor(id: string): Locator {
    return this.payloadFor(id).getByTestId('audit-report-row-payload-json');
  }
}
