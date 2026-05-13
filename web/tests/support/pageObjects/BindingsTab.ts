import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for the Bindings tab inside the ObjectType edit modal
 * (rendered by `src/components/admin/BindingsEditor.tsx` — US-052 /
 * PC-A06).
 *
 * The Bindings tab lives under `/admin/:ontology/objectTypes` once the
 * user opens the ObjectType edit modal; consumers should drive
 * `ObjectTypeAdminPage.goto(...)`, open the edit modal, then click
 * `bindingsTab` to enter this surface.
 */
export class BindingsTab {
  readonly page: Page;
  readonly editTabBindings: Locator;
  readonly editor: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly empty: Locator;
  readonly table: Locator;
  readonly rows: Locator;
  readonly count: Locator;
  readonly newButton: Locator;

  // Create modal
  readonly createForm: Locator;
  readonly createDatasetRid: Locator;
  readonly createBranch: Locator;
  readonly createIsPrimary: Locator;
  readonly createSubmit: Locator;
  readonly createCancel: Locator;
  readonly createError: Locator;

  // Edit modal
  readonly editForm: Locator;
  readonly editDatasetRid: Locator;
  readonly editBranch: Locator;
  readonly editIsPrimary: Locator;
  readonly editSubmit: Locator;
  readonly editCancel: Locator;
  readonly editError: Locator;

  // Delete modal
  readonly deleteModal: Locator;
  readonly deleteSubmit: Locator;
  readonly deleteCancel: Locator;
  readonly deleteError: Locator;

  constructor(page: Page) {
    this.page = page;
    this.editTabBindings = page.getByTestId('object-type-edit-tab-bindings');
    this.editor = page.getByTestId('bindings-editor');
    this.loading = page.getByTestId('bindings-loading');
    this.error = page.getByTestId('bindings-error');
    this.empty = page.getByTestId('bindings-empty');
    this.table = page.getByTestId('bindings-table');
    this.rows = page.getByTestId('bindings-row');
    this.count = page.getByTestId('bindings-count');
    this.newButton = page.getByTestId('bindings-new-btn');

    this.createForm = page.getByTestId('bindings-create-form');
    this.createDatasetRid = page.getByTestId('bindings-create-dataset-rid');
    this.createBranch = page.getByTestId('bindings-create-branch');
    this.createIsPrimary = page.getByTestId('bindings-create-is-primary');
    this.createSubmit = page.getByTestId('bindings-create-submit');
    this.createCancel = page.getByTestId('bindings-create-cancel');
    this.createError = page.getByTestId('bindings-create-error');

    this.editForm = page.getByTestId('bindings-edit-form');
    this.editDatasetRid = page.getByTestId('bindings-edit-dataset-rid');
    this.editBranch = page.getByTestId('bindings-edit-branch');
    this.editIsPrimary = page.getByTestId('bindings-edit-is-primary');
    this.editSubmit = page.getByTestId('bindings-edit-submit');
    this.editCancel = page.getByTestId('bindings-edit-cancel');
    this.editError = page.getByTestId('bindings-edit-error');

    this.deleteModal = page.getByTestId('bindings-delete-modal');
    this.deleteSubmit = page.getByTestId('bindings-delete-submit');
    this.deleteCancel = page.getByTestId('bindings-delete-cancel');
    this.deleteError = page.getByTestId('bindings-delete-error');
  }

  rowByRid(rid: string): Locator {
    return this.page.locator(
      `[data-testid="bindings-row"][data-binding-rid="${rid}"]`,
    );
  }

  editButtonFor(rid: string): Locator {
    return this.page.locator(
      `[data-testid="bindings-edit-btn"][data-binding-rid="${rid}"]`,
    );
  }

  deleteButtonFor(rid: string): Locator {
    return this.page.locator(
      `[data-testid="bindings-delete-btn"][data-binding-rid="${rid}"]`,
    );
  }

  mappingPropertyAt(index: number): Locator {
    return this.page.locator(
      `[data-testid="bindings-mapping-property"][data-mapping-index="${index}"]`,
    );
  }

  mappingColumnAt(index: number): Locator {
    return this.page.locator(
      `[data-testid="bindings-mapping-column"][data-mapping-index="${index}"]`,
    );
  }

  get mappingAddButton(): Locator {
    return this.page.getByTestId('bindings-mapping-add');
  }
}
