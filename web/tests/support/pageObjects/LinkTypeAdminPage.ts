import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/admin/:ontology/linkTypes` — the LinkType editor
 * rendered by `src/components/admin/LinkTypeAdminPage.tsx`.
 *
 * Mirrors the four-state testid template established by US-021/022/026
 * (root / loading / error / empty wrappers) and stacks the modal surfaces
 * (Create / Edit / Delete) on top so spec scenarios can drive the page
 * without depending on i18n labels or fragile aria-only selectors.
 *
 * Row + per-row affordance locators use the composite
 * `[data-testid="link-type-{row,edit-btn,delete-btn}"]` +
 * `[data-link-type-api-name="..."]` selector pattern established by
 * US-029 (ObjectTypeAdminPage) so spec scenarios stay decoupled from
 * sort order or row index.
 */
export class LinkTypeAdminPage {
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
  readonly createDescription: Locator;
  readonly createSource: Locator;
  readonly createTarget: Locator;
  readonly createCardinality: Locator;
  readonly createForeignKey: Locator;
  readonly createRequired: Locator;
  readonly createSubmit: Locator;
  readonly createCancel: Locator;
  readonly createError: Locator;

  // Edit modal
  readonly editForm: Locator;
  readonly editApiName: Locator;
  readonly editRelationship: Locator;
  readonly editDisplayName: Locator;
  readonly editDescription: Locator;
  readonly editRequired: Locator;
  readonly editSubmit: Locator;
  readonly editCancel: Locator;
  readonly editError: Locator;

  // Delete modal
  readonly deleteModal: Locator;
  readonly deleteConfirm: Locator;
  readonly deleteCancel: Locator;
  readonly deleteError: Locator;
  readonly deleteImpactActions: Locator;
  readonly deleteImpactSearchAround: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('link-type-admin-page');
    this.noOntology = page.getByTestId('link-type-admin-no-ontology');
    this.loading = page.getByTestId('link-type-admin-loading');
    this.error = page.getByTestId('link-type-admin-error');
    this.empty = page.getByTestId('link-type-admin-empty');
    this.table = page.getByTestId('link-type-admin-table');
    this.rows = page.getByTestId('link-type-row');
    this.newButton = page.getByTestId('link-type-new-btn');
    this.modalOverlay = page.getByTestId('modal-overlay');

    this.createForm = page.getByTestId('link-type-create-form');
    this.createDisplayName = page.getByTestId('link-type-create-display-name');
    this.createApiName = page.getByTestId('link-type-create-api-name');
    this.createDescription = page.getByTestId('link-type-create-description');
    this.createSource = page.getByTestId('link-type-create-source');
    this.createTarget = page.getByTestId('link-type-create-target');
    this.createCardinality = page.getByTestId('link-type-create-cardinality');
    this.createForeignKey = page.getByTestId('link-type-create-foreign-key');
    this.createRequired = page.getByTestId('link-type-create-required');
    this.createSubmit = page.getByTestId('link-type-create-submit');
    this.createCancel = page.getByTestId('link-type-create-cancel');
    this.createError = page.getByTestId('link-type-create-error');

    this.editForm = page.getByTestId('link-type-edit-form');
    this.editApiName = page.getByTestId('link-type-edit-api-name');
    this.editRelationship = page.getByTestId('link-type-edit-relationship');
    this.editDisplayName = page.getByTestId('link-type-edit-display-name');
    this.editDescription = page.getByTestId('link-type-edit-description');
    this.editRequired = page.getByTestId('link-type-edit-required');
    this.editSubmit = page.getByTestId('link-type-edit-submit');
    this.editCancel = page.getByTestId('link-type-edit-cancel');
    this.editError = page.getByTestId('link-type-edit-error');

    this.deleteModal = page.getByTestId('link-type-delete-modal');
    this.deleteConfirm = page.getByTestId('link-type-delete-confirm');
    this.deleteCancel = page.getByTestId('link-type-delete-cancel');
    this.deleteError = page.getByTestId('link-type-delete-error');
    this.deleteImpactActions = page.getByTestId('delete-impact-actions');
    this.deleteImpactSearchAround = page.getByTestId(
      'delete-impact-search-around',
    );
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(
      `/admin/${encodeURIComponent(ontologyApiName)}/linkTypes`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  rowByApiName(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="link-type-row"][data-link-type-api-name="${apiName}"]`,
    );
  }

  editButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="link-type-edit-btn"][data-link-type-api-name="${apiName}"]`,
    );
  }

  deleteButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="link-type-delete-btn"][data-link-type-api-name="${apiName}"]`,
    );
  }
}
