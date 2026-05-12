import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/aggregation/:ontology/:objectType` — the Aggregation
 * page rendered by `src/components/aggregation/AggregationPage.tsx`.
 *
 * Locators follow the state-branch + per-row testid template established
 * by US-021/022/026/029/030/031. State branches (no-params /
 * typeloading / loading / error / empty / results) each have an
 * exclusive wrapper testid; mutation buttons (`aggregation-execute`,
 * `metric-add`, `metric-{i}-remove`, `groupby-add`, `groupby-{i}-remove`)
 * use stable testids; per-row config inputs (`metric-{i}-{type|field|name}`
 * and the pre-existing `groupby-{i}-{field|type}`) carry per-row testids
 * so scenarios can drive multi-row config without nth-child fragility.
 */
export class AggregationPage {
  readonly page: Page;
  readonly root: Locator;
  readonly noParams: Locator;
  readonly typeLoading: Locator;
  readonly configPanel: Locator;
  readonly metricsSection: Locator;
  readonly groupBySection: Locator;
  readonly executeBtn: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly emptyState: Locator;
  readonly results: Locator;
  readonly bucketTree: Locator;
  readonly emptyResults: Locator;
  readonly accuracyBadge: Locator;
  readonly chart: Locator;
  readonly metricAddBtn: Locator;
  readonly groupByAddBtn: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('aggregation-page');
    this.noParams = page.getByTestId('aggregation-no-params');
    this.typeLoading = page.getByTestId('aggregation-typeloading');
    this.configPanel = page.getByTestId('aggregation-config-panel');
    this.metricsSection = page.getByTestId('metrics-section');
    this.groupBySection = page.getByTestId('groupby-section');
    this.executeBtn = page.getByTestId('aggregation-execute');
    this.loading = page.getByTestId('aggregation-loading');
    this.error = page.getByTestId('aggregation-error');
    this.emptyState = page.getByTestId('aggregation-empty-state');
    this.results = page.getByTestId('aggregation-results');
    this.bucketTree = page.getByTestId('aggregation-bucket-tree');
    this.emptyResults = page.getByTestId('aggregation-empty-results');
    this.accuracyBadge = page.getByTestId('aggregation-accuracy-badge');
    this.chart = page.getByTestId('aggregation-chart');
    this.metricAddBtn = page.getByTestId('metric-add');
    this.groupByAddBtn = page.getByTestId('groupby-add');
  }

  async goto(ontologyApiName: string, objectTypeApiName: string): Promise<void> {
    await this.page.goto(
      `/aggregation/${encodeURIComponent(ontologyApiName)}/${encodeURIComponent(objectTypeApiName)}`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  metricRow(index: number): Locator {
    return this.page.getByTestId(`metric-row-${index}`);
  }

  metricTypeSelect(index: number): Locator {
    return this.page.getByTestId(`metric-${index}-type`);
  }

  metricFieldSelect(index: number): Locator {
    return this.page.getByTestId(`metric-${index}-field`);
  }

  metricNameInput(index: number): Locator {
    return this.page.getByTestId(`metric-${index}-name`);
  }

  metricRemoveBtn(index: number): Locator {
    return this.page.getByTestId(`metric-${index}-remove`);
  }

  groupByFieldSelect(index: number): Locator {
    return this.page.getByTestId(`groupby-${index}-field`);
  }

  groupByTypeSelect(index: number): Locator {
    return this.page.getByTestId(`groupby-${index}-type`);
  }

  groupByRemoveBtn(index: number): Locator {
    return this.page.getByTestId(`groupby-${index}-remove`);
  }
}
