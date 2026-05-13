import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/admin/:ontology/valueTypes` — the ValueType editor
 * rendered by `src/components/admin/ValueTypeAdminPage.tsx` (US-051 /
 * PC-A05).
 *
 * ValueTypes are reusable typed primitives (e.g. EmailAddress, Currency)
 * with optional constraints — pattern / range / enum — that Property
 * `base_type` references by apiName. The Admin UI gives the operator a
 * single surface to List / Create / Update / Delete them, edit their
 * constraints, and inspect which Properties currently reference them
 * ("Used by" reverse lookup).
 *
 * Mirrors the four-state testid template (root / loading / error /
 * empty) and modal-overlay convention established by US-029 ~ US-032.
 * Row + per-row affordance locators use the composite
 * `[data-testid="value-type-{row,edit-btn,delete-btn,usages-btn}"]` +
 * `[data-value-type-api-name="..."]` selector pattern so scenarios stay
 * decoupled from sort order or row index.
 */
export class ValueTypeAdminPage {
  readonly page: Page;
  readonly root: Locator;
  readonly noOntology: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly empty: Locator;
  readonly table: Locator;
  readonly rows: Locator;
  readonly newButton: Locator;
  readonly searchInput: Locator;
  readonly modalOverlay: Locator;

  // Create modal (shares builder with Edit; distinguished by testid prefix).
  readonly createForm: Locator;
  // Edit modal
  readonly editForm: Locator;

  // Shared builder inputs.
  readonly displayNameInput: Locator;
  readonly apiNameInput: Locator;
  readonly baseTypeSelect: Locator;
  readonly constraintKindSelect: Locator;
  readonly patternInput: Locator;
  readonly minInput: Locator;
  readonly maxInput: Locator;
  readonly enumInput: Locator;
  readonly constraintPreview: Locator;
  readonly createSubmit: Locator;
  readonly createCancel: Locator;
  readonly createError: Locator;
  readonly editSubmit: Locator;
  readonly editCancel: Locator;
  readonly editError: Locator;

  // Delete modal
  readonly deleteModal: Locator;
  readonly deleteConfirm: Locator;
  readonly deleteCancel: Locator;
  readonly deleteError: Locator;

  // Usages ("Used by") modal — reverse references to Properties whose
  // base_type equals this ValueType's apiName.
  readonly usagesModal: Locator;
  readonly usagesEmpty: Locator;
  readonly usagesList: Locator;
  readonly usagesRows: Locator;
  readonly usagesClose: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('value-type-admin-page');
    this.noOntology = page.getByTestId('value-type-admin-no-ontology');
    this.loading = page.getByTestId('value-type-admin-loading');
    this.error = page.getByTestId('value-type-admin-error');
    this.empty = page.getByTestId('value-type-admin-empty');
    this.table = page.getByTestId('value-type-admin-table');
    this.rows = page.getByTestId('value-type-row');
    this.newButton = page.getByTestId('value-type-new-btn');
    this.searchInput = page.getByTestId('value-type-search-input');
    this.modalOverlay = page.getByTestId('modal-overlay');

    this.createForm = page.getByTestId('value-type-create-form');
    this.editForm = page.getByTestId('value-type-edit-form');

    this.displayNameInput = page.getByTestId('value-type-display-name');
    this.apiNameInput = page.getByTestId('value-type-api-name');
    this.baseTypeSelect = page.getByTestId('value-type-base-type');
    this.constraintKindSelect = page.getByTestId('value-type-constraint-kind');
    this.patternInput = page.getByTestId('value-type-constraint-pattern');
    this.minInput = page.getByTestId('value-type-constraint-min');
    this.maxInput = page.getByTestId('value-type-constraint-max');
    this.enumInput = page.getByTestId('value-type-constraint-enum');
    this.constraintPreview = page.getByTestId('value-type-constraint-preview');

    this.createSubmit = page.getByTestId('value-type-create-submit');
    this.createCancel = page.getByTestId('value-type-create-cancel');
    this.createError = page.getByTestId('value-type-create-error');
    this.editSubmit = page.getByTestId('value-type-edit-submit');
    this.editCancel = page.getByTestId('value-type-edit-cancel');
    this.editError = page.getByTestId('value-type-edit-error');

    this.deleteModal = page.getByTestId('value-type-delete-modal');
    this.deleteConfirm = page.getByTestId('value-type-delete-confirm');
    this.deleteCancel = page.getByTestId('value-type-delete-cancel');
    this.deleteError = page.getByTestId('value-type-delete-error');

    this.usagesModal = page.getByTestId('value-type-usages-modal');
    this.usagesEmpty = page.getByTestId('value-type-usages-empty');
    this.usagesList = page.getByTestId('value-type-usages-list');
    this.usagesRows = page.getByTestId('value-type-usage-row');
    this.usagesClose = page.getByTestId('value-type-usages-close');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(
      `/admin/${encodeURIComponent(ontologyApiName)}/valueTypes`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  rowByApiName(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="value-type-row"][data-value-type-api-name="${apiName}"]`,
    );
  }

  editButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="value-type-edit-btn"][data-value-type-api-name="${apiName}"]`,
    );
  }

  deleteButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="value-type-delete-btn"][data-value-type-api-name="${apiName}"]`,
    );
  }

  usagesButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="value-type-usages-btn"][data-value-type-api-name="${apiName}"]`,
    );
  }
}
