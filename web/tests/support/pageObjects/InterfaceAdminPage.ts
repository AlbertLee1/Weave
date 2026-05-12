import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/admin/:ontology/interfaces` — the Interface editor
 * rendered by `src/components/admin/InterfaceAdminPage.tsx`.
 *
 * Mirrors the four-state testid template established by US-021/022/026
 * (root / loading / error / empty wrappers) and stacks the modal surfaces
 * (Create / Edit / Delete / Implementing) on top so spec scenarios can
 * drive the page without depending on i18n labels or fragile aria-only
 * selectors.
 *
 * Row + per-row affordance locators use the composite
 * `[data-testid="interface-{row,edit-btn,delete-btn,manage-btn}"]` +
 * `[data-interface-api-name="..."]` selector pattern established by
 * US-029 (ObjectTypeAdminPage) / US-030 (LinkTypeAdminPage) / US-031
 * (ActionTypeAdminPage) so spec scenarios stay decoupled from sort order
 * or row index. Implementing modal rows likewise expose
 * `data-object-type-api-name` for stable per-attachment lookup.
 */
export class InterfaceAdminPage {
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

  // Create modal (shares the builder form with Edit; distinguished by testid
  // — same pattern as US-031 ActionTypeBuilderModal).
  readonly createForm: Locator;
  // Edit modal
  readonly editForm: Locator;

  // Shared builder inputs (matched inside whichever form is open — only one
  // builder modal is mounted at a time per page state so the testids never
  // collide).
  readonly displayNameInput: Locator;
  readonly apiNameInput: Locator;
  readonly descriptionInput: Locator;
  readonly extendsSelect: Locator;
  readonly addSharedPropertyButton: Locator;
  readonly addLinkTypeButton: Locator;
  readonly sharedPropertiesSection: Locator;
  readonly linkTypesSection: Locator;
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

  // Implementing modal (Manage attachments by ObjectType — "implement by
  // ObjectType" + "resolved view" AC).
  readonly implementingModal: Locator;
  readonly implementingList: Locator;
  readonly implementingEmpty: Locator;
  readonly implementingAttachSelect: Locator;
  readonly implementingAttachButton: Locator;
  readonly implementingClose: Locator;
  readonly implementingError: Locator;
  readonly implementingRows: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('interface-admin-page');
    this.noOntology = page.getByTestId('interface-admin-no-ontology');
    this.loading = page.getByTestId('interface-admin-loading');
    this.error = page.getByTestId('interface-admin-error');
    this.empty = page.getByTestId('interface-admin-empty');
    this.table = page.getByTestId('interface-admin-table');
    this.rows = page.getByTestId('interface-row');
    this.newButton = page.getByTestId('interface-new-btn');
    this.searchInput = page.getByTestId('interface-search-input');
    this.modalOverlay = page.getByTestId('modal-overlay');

    this.createForm = page.getByTestId('interface-create-form');
    this.editForm = page.getByTestId('interface-edit-form');

    this.displayNameInput = page.getByTestId('interface-display-name');
    this.apiNameInput = page.getByTestId('interface-api-name');
    this.descriptionInput = page.getByTestId('interface-description');
    this.extendsSelect = page.getByTestId('interface-extends');
    this.addSharedPropertyButton = page.getByTestId(
      'interface-add-shared-property',
    );
    this.addLinkTypeButton = page.getByTestId('interface-add-link-type');
    this.sharedPropertiesSection = page.getByTestId(
      'interface-shared-properties-section',
    );
    this.linkTypesSection = page.getByTestId('interface-link-types-section');

    this.createSubmit = page.getByTestId('interface-create-submit');
    this.createCancel = page.getByTestId('interface-create-cancel');
    this.createError = page.getByTestId('interface-create-error');
    this.editSubmit = page.getByTestId('interface-edit-submit');
    this.editCancel = page.getByTestId('interface-edit-cancel');
    this.editError = page.getByTestId('interface-edit-error');

    this.deleteModal = page.getByTestId('interface-delete-modal');
    this.deleteConfirm = page.getByTestId('interface-delete-confirm');
    this.deleteCancel = page.getByTestId('interface-delete-cancel');
    this.deleteError = page.getByTestId('interface-delete-error');

    this.implementingModal = page.getByTestId('interface-implementing-modal');
    this.implementingList = page.getByTestId('interface-implementing-list');
    this.implementingEmpty = page.getByTestId('interface-implementing-empty');
    this.implementingAttachSelect = page.getByTestId(
      'interface-implementing-attach-select',
    );
    this.implementingAttachButton = page.getByTestId(
      'interface-implementing-attach-btn',
    );
    this.implementingClose = page.getByTestId('interface-implementing-close');
    this.implementingError = page.getByTestId('interface-implementing-error');
    this.implementingRows = page.getByTestId('interface-implementing-row');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(
      `/admin/${encodeURIComponent(ontologyApiName)}/interfaces`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  rowByApiName(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="interface-row"][data-interface-api-name="${apiName}"]`,
    );
  }

  editButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="interface-edit-btn"][data-interface-api-name="${apiName}"]`,
    );
  }

  deleteButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="interface-delete-btn"][data-interface-api-name="${apiName}"]`,
    );
  }

  manageButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="interface-manage-btn"][data-interface-api-name="${apiName}"]`,
    );
  }

  implementingRowByObjectType(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="interface-implementing-row"][data-object-type-api-name="${apiName}"]`,
    );
  }

  detachButtonFor(apiName: string): Locator {
    return this.page.locator(
      `[data-testid="interface-implementing-detach-btn"][data-object-type-api-name="${apiName}"]`,
    );
  }
}
