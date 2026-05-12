import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/admin/:ontology/actionTypes` — the ActionType editor
 * rendered by `src/components/admin/ActionTypeAdminPage.tsx`.
 *
 * Mirrors the four-state testid template established by US-021/022/026
 * (root / loading / error / empty wrappers) and stacks the modal surfaces
 * (Create / Edit / Delete — Create and Edit share the same
 * ActionTypeBuilderModal so the form testid varies by mode) on top so
 * spec scenarios can drive the page without depending on i18n labels or
 * fragile aria-only selectors.
 *
 * Row + per-row affordance locators use the composite
 * `[data-testid="action-type-{row,edit-btn,delete-btn}"]` +
 * `[data-action-type-api-name="..."]` selector pattern established by
 * US-029 (ObjectTypeAdminPage) / US-030 (LinkTypeAdminPage) so spec
 * scenarios stay decoupled from sort order or row index.
 */
export class ActionTypeAdminPage {
  readonly page: Page;
  readonly root: Locator;
  readonly noOntology: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly empty: Locator;
  readonly table: Locator;
  readonly rows: Locator;
  readonly newButton: Locator;
  readonly modalOverlay: Locator;
  readonly jsonPreview: Locator;

  // Create modal (shares the builder form with Edit; distinguished by testid).
  readonly createForm: Locator;

  // Edit modal
  readonly editForm: Locator;

  // Shared builder inputs (matched inside whichever form is open).
  readonly displayNameInput: Locator;
  readonly apiNameInput: Locator;
  readonly descriptionInput: Locator;
  readonly statusSelect: Locator;
  readonly addParameterButton: Locator;
  readonly addRuleButton: Locator;
  readonly parametersSection: Locator;
  readonly rulesSection: Locator;
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

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('action-type-admin-page');
    this.noOntology = page.getByTestId('action-type-admin-no-ontology');
    this.loading = page.getByTestId('action-type-admin-loading');
    this.error = page.getByTestId('action-type-admin-error');
    this.empty = page.getByTestId('action-type-admin-empty');
    this.table = page.getByTestId('action-type-admin-table');
    this.rows = page.getByTestId('action-type-row');
    this.newButton = page.getByTestId('action-type-new-btn');
    this.modalOverlay = page.getByTestId('modal-overlay');
    this.jsonPreview = page.getByTestId('action-json-preview');

    this.createForm = page.getByTestId('action-type-create-form');
    this.editForm = page.getByTestId('action-type-edit-form');

    // Builder inputs live inside the open form (Create or Edit). Page object
    // intentionally exposes a single locator each — only one builder modal
    // is mounted at a time per page state, so the testid is unique on the
    // visible DOM (testids never collide because Create vs Edit toggle a
    // disjoint set of form testids).
    this.displayNameInput = page.getByTestId('action-type-display-name');
    this.apiNameInput = page.getByTestId('action-type-api-name');
    this.descriptionInput = page.getByTestId('action-type-description');
    this.statusSelect = page.getByTestId('action-type-status');
    this.addParameterButton = page.getByTestId('action-type-add-parameter');
    this.addRuleButton = page.getByTestId('action-type-add-rule');
    this.parametersSection = page.getByTestId('action-type-parameters-section');
    this.rulesSection = page.getByTestId('action-type-rules-section');

    this.createSubmit = page.getByTestId('action-type-create-submit');
    this.createCancel = page.getByTestId('action-type-create-cancel');
    this.createError = page.getByTestId('action-type-create-error');
    this.editSubmit = page.getByTestId('action-type-edit-submit');
    this.editCancel = page.getByTestId('action-type-edit-cancel');
    this.editError = page.getByTestId('action-type-edit-error');

    this.deleteModal = page.getByTestId('action-type-delete-modal');
    this.deleteConfirm = page.getByTestId('action-type-delete-confirm');
    this.deleteCancel = page.getByTestId('action-type-delete-cancel');
    this.deleteError = page.getByTestId('action-type-delete-error');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(
      `/admin/${encodeURIComponent(ontologyApiName)}/actionTypes`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  rowByApiName(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="action-type-row"][data-action-type-api-name="${apiName}"]`,
    );
  }

  editButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="action-type-edit-btn"][data-action-type-api-name="${apiName}"]`,
    );
  }

  deleteButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="action-type-delete-btn"][data-action-type-api-name="${apiName}"]`,
    );
  }
}
