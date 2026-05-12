import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/objectsets/:ontology` — the ObjectSet Composer rendered
 * by `src/components/objectsets/ObjectSetPage.tsx`.
 *
 * Mirrors the state-branch testid template established by US-021/022/026
 * (root / loading / no-ontology + browse-pane loading/error/empty/pending).
 * Scenarios drive the composer through ARIA-labeled `<select>` controls
 * (objectset type, object type, link, direction, where type/field/value)
 * to mutate the wire definition, click Execute, and assert that the
 * captured POST body matches the operator under test.
 */
export class ObjectSetBuilderPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly noOntology: Locator;
  readonly executeBtn: Locator;
  readonly saveAsBtn: Locator;
  readonly results: Locator;
  readonly resultsInitial: Locator;
  readonly browseLoading: Locator;
  readonly browseError: Locator;
  readonly browseEmpty: Locator;
  readonly browsePending: Locator;
  readonly statusLine: Locator;
  readonly dataTable: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('objectset-page');
    this.loading = page.getByTestId('objectset-loading');
    this.noOntology = page.getByTestId('objectset-no-ontology');
    this.executeBtn = page.getByTestId('objectset-execute-btn');
    this.saveAsBtn = page.getByTestId('objectset-saveas-btn');
    this.results = page.getByTestId('objectset-results');
    this.resultsInitial = page.getByTestId('objectset-results-initial');
    this.browseLoading = page.getByTestId('objectset-browse-loading');
    this.browseError = page.getByTestId('objectset-browse-error');
    this.browseEmpty = page.getByTestId('objectset-browse-empty');
    this.browsePending = page.getByTestId('objectset-browse-pending');
    this.statusLine = page.getByTestId('objectset-status-line');
    this.dataTable = page.getByTestId('data-table');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(`/objectsets/${encodeURIComponent(ontologyApiName)}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  /** The outermost `<select aria-label="objectset type">` at depth=0. */
  rootTypeSelect(): Locator {
    return this.page.getByLabel('objectset type').first();
  }

  /** The first object-type select at depth=0 (the leaf of a base node). */
  rootObjectTypeSelect(): Locator {
    return this.page.getByLabel('object type').first();
  }

  /** The nested type select for child #index of a multi-branch operator. */
  branchTypeSelect(index: number): Locator {
    // Each ObjectSetBuilder renders one `<select aria-label="objectset type">`.
    // The root composer is at index 0, branches start at index 1.
    return this.page.getByLabel('objectset type').nth(index);
  }

  /** Object-type select inside child #index (1-based for branches). */
  branchObjectTypeSelect(index: number): Locator {
    return this.page.getByLabel('object type').nth(index);
  }

  whereTypeSelect(): Locator {
    return this.page.getByLabel('where type');
  }

  whereFieldInput(): Locator {
    return this.page.getByLabel('where field');
  }

  whereValueInput(): Locator {
    return this.page.getByLabel('where value');
  }

  linkInput(): Locator {
    return this.page.getByLabel('link').first();
  }

  directionSelect(): Locator {
    return this.page.getByLabel('direction').first();
  }
}
