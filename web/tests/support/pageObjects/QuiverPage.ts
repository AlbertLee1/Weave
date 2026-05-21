import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/quiver/:ontology[/:rid]` — the Quiver Workbench page
 * rendered by `src/components/quiver/QuiverPage.tsx` plus the read-only
 * `/quiver/:ontology/:rid/view` share surface rendered by
 * `src/components/quiver/QuiverViewPage.tsx`.
 *
 * Locators follow the existing convention (US-021/022/026/029/030/031/033):
 *   - state-branch testids on wrappers (`quiver-page` / `quiver-view-page`
 *     / EmptyState wrappers)
 *   - per-row testids keyed by SeriesSpec.id for aggregate panel rows
 *   - stable testids for form inputs, add/save/share/delete buttons.
 *
 * The QuiverPage component exposes its picker form via `quiver-input-*`
 * testids, the saved list via `quiver-saved-*` / `quiver-load-*` /
 * `quiver-share-*` / `quiver-delete-*` testids keyed by dashboard RID,
 * the JSON export control via `quiver-export-json-button`, and the
 * aggregate panel via `quiver-row-{id}` / `quiver-count-{id}` /
 * `quiver-sum-{id}` / `quiver-avg-{id}` / `quiver-max-{id}` testids
 * keyed by SeriesSpec.id.
 */
export class QuiverPage {
  readonly page: Page;
  readonly root: Locator;
  readonly saveControls: Locator;
  readonly dashboardNameInput: Locator;
  readonly saveBtn: Locator;
  readonly exportJsonBtn: Locator;
  readonly newBtn: Locator;
  readonly saveError: Locator;
  readonly savedList: Locator;
  readonly addForm: Locator;
  readonly inputObjectType: Locator;
  readonly inputPrimaryKey: Locator;
  readonly inputProperty: Locator;
  readonly inputLabel: Locator;
  readonly inputBranch: Locator;
  readonly addBtn: Locator;
  readonly chartPanel: Locator;
  readonly chartWrap: Locator;
  readonly chart: Locator;
  readonly aggregatePanel: Locator;
  readonly selectionStart: Locator;
  readonly selectionEnd: Locator;
  readonly clearSelectionBtn: Locator;

  // Read-only share view locators
  readonly viewRoot: Locator;
  readonly viewTitle: Locator;
  readonly viewLoading: Locator;
  readonly viewError: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('quiver-page');
    this.saveControls = page.getByTestId('quiver-save-controls');
    this.dashboardNameInput = page.getByTestId('quiver-dashboard-name');
    this.saveBtn = page.getByTestId('quiver-save-button');
    this.exportJsonBtn = page.getByTestId('quiver-export-json-button');
    this.newBtn = page.getByTestId('quiver-new-button');
    this.saveError = page.getByTestId('quiver-save-error');
    this.savedList = page.getByTestId('quiver-saved-list');
    this.addForm = page.getByTestId('quiver-add-form');
    this.inputObjectType = page.getByTestId('quiver-input-objectType');
    this.inputPrimaryKey = page.getByTestId('quiver-input-primaryKey');
    this.inputProperty = page.getByTestId('quiver-input-property');
    this.inputLabel = page.getByTestId('quiver-input-label');
    this.inputBranch = page.getByTestId('quiver-input-branch');
    this.addBtn = page.getByTestId('quiver-add-button');
    this.chartPanel = page.getByTestId('quiver-chart-panel');
    this.chartWrap = page.getByTestId('quiver-chart-wrap');
    this.chart = page.getByTestId('quiver-chart');
    this.aggregatePanel = page.getByTestId('quiver-aggregate-panel');
    this.selectionStart = page.getByTestId('quiver-selection-start');
    this.selectionEnd = page.getByTestId('quiver-selection-end');
    this.clearSelectionBtn = page.getByTestId('quiver-clear-selection');

    this.viewRoot = page.getByTestId('quiver-view-page');
    this.viewTitle = page.getByTestId('quiver-view-title');
    this.viewLoading = page.getByTestId('quiver-view-loading');
    this.viewError = page.getByTestId('quiver-view-error');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(`/quiver/${encodeURIComponent(ontologyApiName)}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  async gotoSaved(ontologyApiName: string, rid: string): Promise<void> {
    await this.page.goto(
      `/quiver/${encodeURIComponent(ontologyApiName)}/${encodeURIComponent(rid)}`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  async gotoView(ontologyApiName: string, rid: string): Promise<void> {
    await this.page.goto(
      `/quiver/${encodeURIComponent(ontologyApiName)}/${encodeURIComponent(rid)}/view`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  /** Fill all four required + optional fields then click Add. */
  async addSeries(opts: {
    objectType: string;
    primaryKey: string;
    property: string;
    label?: string;
    branch?: string;
  }): Promise<void> {
    await this.inputObjectType.fill(opts.objectType);
    await this.inputPrimaryKey.fill(opts.primaryKey);
    await this.inputProperty.fill(opts.property);
    if (opts.label !== undefined) await this.inputLabel.fill(opts.label);
    if (opts.branch !== undefined) await this.inputBranch.fill(opts.branch);
    await this.addBtn.click();
  }

  /** Locator for the per-series aggregate row keyed by SeriesSpec.id. */
  rowForSeries(seriesId: string): Locator {
    return this.page.getByTestId(`quiver-row-${seriesId}`);
  }

  /** Locator for any aggregate row matching the (objectType, primaryKey, property) slot. */
  rowForSlot(objectType: string, primaryKey: string, property: string): Locator {
    const prefix = `${objectType}|${primaryKey}|${property}`;
    return this.page.locator(
      `[data-testid^="quiver-row-${prefix}"]`,
    );
  }

  removeRowBtn(seriesId: string): Locator {
    return this.page.getByTestId(`quiver-remove-${seriesId}`);
  }

  countCellForSeries(seriesId: string): Locator {
    return this.page.getByTestId(`quiver-count-${seriesId}`);
  }

  sumCellForSeries(seriesId: string): Locator {
    return this.page.getByTestId(`quiver-sum-${seriesId}`);
  }

  savedDashboard(rid: string): Locator {
    return this.page.getByTestId(`quiver-saved-${rid}`);
  }

  loadDashboardBtn(rid: string): Locator {
    return this.page.getByTestId(`quiver-load-${rid}`);
  }

  shareDashboardBtn(rid: string): Locator {
    return this.page.getByTestId(`quiver-share-${rid}`);
  }

  deleteDashboardBtn(rid: string): Locator {
    return this.page.getByTestId(`quiver-delete-${rid}`);
  }
}
