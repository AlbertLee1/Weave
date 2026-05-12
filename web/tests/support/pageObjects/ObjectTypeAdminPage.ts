import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/admin/:ontology/objectTypes` — the ObjectType editor
 * rendered by `src/components/admin/ObjectTypeAdminPage.tsx`.
 *
 * Mirrors the four-state testid template established by US-021/022/026
 * (root / loading / error / empty wrappers) and stacks the modal surfaces
 * (Create / Edit / Delete) on top so spec scenarios can drive the page
 * without depending on i18n labels or fragile aria-only selectors.
 */
export class ObjectTypeAdminPage {
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

  // Create modal
  readonly createForm: Locator;
  readonly createDisplayName: Locator;
  readonly createApiName: Locator;
  readonly createPrimaryKey: Locator;
  readonly createSubmit: Locator;
  readonly createCancel: Locator;
  readonly createError: Locator;

  // Edit modal
  readonly editForm: Locator;
  readonly editTabs: Locator;
  readonly editTabDetails: Locator;
  readonly editTabProperties: Locator;
  readonly editDisplayName: Locator;
  readonly editApiName: Locator;
  readonly editSubmit: Locator;
  readonly editCancel: Locator;
  readonly editError: Locator;

  // Delete modal
  readonly deleteModal: Locator;
  readonly deleteConfirm: Locator;
  readonly deleteCancel: Locator;
  readonly deleteError: Locator;
  readonly deleteImpactLinks: Locator;
  readonly deleteImpactActions: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('object-type-admin-page');
    this.noOntology = page.getByTestId('object-type-admin-no-ontology');
    this.loading = page.getByTestId('object-type-admin-loading');
    this.error = page.getByTestId('object-type-admin-error');
    this.empty = page.getByTestId('object-type-admin-empty');
    this.table = page.getByTestId('object-type-admin-table');
    this.rows = page.getByTestId('object-type-row');
    this.newButton = page.getByTestId('object-type-new-btn');
    this.modalOverlay = page.getByTestId('modal-overlay');

    this.createForm = page.getByTestId('object-type-create-form');
    this.createDisplayName = page.getByTestId('object-type-create-display-name');
    this.createApiName = page.getByTestId('object-type-create-api-name');
    this.createPrimaryKey = page.getByTestId('object-type-create-primary-key');
    this.createSubmit = page.getByTestId('object-type-create-submit');
    this.createCancel = page.getByTestId('object-type-create-cancel');
    this.createError = page.getByTestId('object-type-create-error');

    this.editForm = page.getByTestId('object-type-edit-form');
    this.editTabs = page.getByTestId('object-type-edit-tabs');
    this.editTabDetails = page.getByTestId('object-type-edit-tab-details');
    this.editTabProperties = page.getByTestId('object-type-edit-tab-properties');
    this.editDisplayName = page.getByTestId('object-type-edit-display-name');
    this.editApiName = page.getByTestId('object-type-edit-api-name');
    this.editSubmit = page.getByTestId('object-type-edit-submit');
    this.editCancel = page.getByTestId('object-type-edit-cancel');
    this.editError = page.getByTestId('object-type-edit-error');

    this.deleteModal = page.getByTestId('object-type-delete-modal');
    this.deleteConfirm = page.getByTestId('object-type-delete-confirm');
    this.deleteCancel = page.getByTestId('object-type-delete-cancel');
    this.deleteError = page.getByTestId('object-type-delete-error');
    this.deleteImpactLinks = page.getByTestId('delete-impact-links');
    this.deleteImpactActions = page.getByTestId('delete-impact-actions');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(
      `/admin/${encodeURIComponent(ontologyApiName)}/objectTypes`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  rowByApiName(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="object-type-row"][data-object-type-api-name="${apiName}"]`,
    );
  }

  editButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="object-type-edit-btn"][data-object-type-api-name="${apiName}"]`,
    );
  }

  deleteButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="object-type-delete-btn"][data-object-type-api-name="${apiName}"]`,
    );
  }
}
