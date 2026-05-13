import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/methods/:ontology/:objectType/:primaryKey` — the
 * Interface Methods console rendered by
 * `src/components/browser/InterfaceMethodsConsolePage.tsx` (US-047,
 * PC-A04). Entry-point button lives on the Browser detail panel
 * (`object-detail-interface-methods-btn`).
 *
 * Surfaces:
 *   - Header carries ontology / object-type / primary-key data
 *     attributes for stable row-level assertions.
 *   - Left interfaces rail with `data-interface-rid` +
 *     `data-interface-api-name` per row.
 *   - Middle methods list with `data-method-rid`/`data-method-name`/
 *     `data-method-param-count` per row.
 *   - Right invoke panel with the parameter form + Submit + audit-log
 *     link. Result panel exposes `data-action-type-api-name` so BDD
 *     can lock the polymorphic dispatch wire-shape independently of
 *     visible copy.
 */
export class InterfaceMethodsPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly header: Locator;
  readonly backButton: Locator;
  readonly rail: Locator;
  readonly railLoading: Locator;
  readonly railError: Locator;
  readonly railEmpty: Locator;
  readonly pane: Locator;
  readonly paneEmpty: Locator;
  readonly methodList: Locator;
  readonly methodListEmpty: Locator;
  readonly methodListError: Locator;
  readonly methodListLoading: Locator;
  readonly invoke: Locator;
  readonly invokeEmpty: Locator;
  readonly invokeForm: Locator;
  readonly invokeSubmit: Locator;
  readonly invokeNoParamsNote: Locator;
  readonly paramError: Locator;
  readonly serverError: Locator;
  readonly result: Locator;
  readonly resultAction: Locator;
  readonly resultBody: Locator;
  readonly auditLink: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('interface-methods-page');
    this.loading = page.getByTestId('interface-methods-loading');
    this.errorState = page.getByTestId('interface-methods-error');
    this.header = page.getByTestId('interface-methods-header');
    this.backButton = page.getByTestId('interface-methods-back-btn');
    this.rail = page.getByTestId('interface-methods-rail');
    this.railLoading = page.getByTestId('interface-methods-rail-loading');
    this.railError = page.getByTestId('interface-methods-rail-error');
    this.railEmpty = page.getByTestId('interface-methods-rail-empty');
    this.pane = page.getByTestId('interface-methods-pane');
    this.paneEmpty = page.getByTestId('interface-methods-pane-empty');
    this.methodList = page.getByTestId('interface-methods-list');
    this.methodListEmpty = page.getByTestId('interface-methods-list-empty');
    this.methodListError = page.getByTestId('interface-methods-list-error');
    this.methodListLoading = page.getByTestId(
      'interface-methods-list-loading',
    );
    this.invoke = page.getByTestId('interface-methods-invoke');
    this.invokeEmpty = page.getByTestId('interface-methods-invoke-empty');
    this.invokeForm = page.getByTestId('interface-methods-invoke-form');
    this.invokeSubmit = page.getByTestId('interface-methods-invoke-submit');
    this.invokeNoParamsNote = page.getByTestId('interface-methods-no-params');
    this.paramError = page.getByTestId('interface-methods-param-error');
    this.serverError = page.getByTestId('interface-methods-server-error');
    this.result = page.getByTestId('interface-methods-result');
    this.resultAction = page.getByTestId('interface-methods-result-action');
    this.resultBody = page.getByTestId('interface-methods-result-body');
    this.auditLink = page.getByTestId('interface-methods-audit-link');
  }

  async goto(
    ontology: string,
    objectType: string,
    primaryKey: string,
  ): Promise<void> {
    await this.page.goto(
      `/methods/${encodeURIComponent(ontology)}/${encodeURIComponent(objectType)}/${encodeURIComponent(primaryKey)}`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  interfaceRows(): Locator {
    return this.page.locator('[data-testid="interface-methods-rail-row"]');
  }
  interfaceRowByApiName(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="interface-methods-rail-row"][data-interface-api-name="${apiName}"]`,
    );
  }
  interfaceButtonByApiName(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="interface-methods-rail-btn"][data-interface-api-name="${apiName}"]`,
    );
  }
  methodRows(): Locator {
    return this.page.locator('[data-testid="interface-methods-list-row"]');
  }
  methodRowByName(name: string): Locator {
    return this.page.locator(
      `[data-testid="interface-methods-list-row"][data-method-name="${name}"]`,
    );
  }
  methodButtonByName(name: string): Locator {
    return this.page.locator(
      `[data-testid="interface-methods-list-btn"][data-method-name="${name}"]`,
    );
  }
  paramInput(name: string): Locator {
    return this.page.locator(
      `[data-testid="interface-methods-param-input"][data-param-name="${name}"]`,
    );
  }
}
