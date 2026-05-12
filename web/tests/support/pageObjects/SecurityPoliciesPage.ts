import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/admin/:ontology/security` — the Security Policies UI
 * rendered by `src/components/securityPolicies/SecurityPoliciesPage.tsx`
 * (US-041 row policies, US-042 column masks). Selectors mirror the
 * data-testid attributes baked into the production component.
 */
export class SecurityPoliciesPage {
  readonly page: Page;
  readonly root: Locator;
  readonly tabs: Locator;
  readonly tabPanel: Locator;
  readonly rowPoliciesTab: Locator;
  readonly columnMasksTab: Locator;
  readonly cellMasksPlaceholder: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly emptyState: Locator;
  readonly list: Locator;
  readonly createBtn: Locator;
  readonly simulatorToggleBtn: Locator;
  readonly simulator: Locator;
  readonly editor: Locator;
  readonly editorForm: Locator;
  readonly deleteDialog: Locator;
  // Column Masks (US-042) — own list / editor / simulator / delete-dialog
  // surface so per-tab BDD specs can scope independently without cross
  // contamination from the row-policies testid namespace.
  readonly columnMasksLoading: Locator;
  readonly columnMasksErrorState: Locator;
  readonly columnMasksEmptyState: Locator;
  readonly columnMasksList: Locator;
  readonly columnMasksCreateBtn: Locator;
  readonly columnMasksSimulatorToggleBtn: Locator;
  readonly columnMasksSimulator: Locator;
  readonly columnMaskEditor: Locator;
  readonly columnMaskEditorForm: Locator;
  readonly columnMaskDeleteDialog: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('security-policies-page');
    this.tabs = page.getByTestId('security-policies-tabs');
    this.tabPanel = page.getByTestId('security-policies-tab-panel');
    this.rowPoliciesTab = page.getByTestId('row-policies-tab');
    this.columnMasksTab = page.getByTestId('column-masks-tab');
    this.cellMasksPlaceholder = page.getByTestId(
      'security-policies-cell-placeholder',
    );
    this.loading = page.getByTestId('row-policies-loading');
    this.errorState = page.getByTestId('row-policies-error');
    this.emptyState = page.getByTestId('row-policies-empty');
    this.list = page.getByTestId('row-policies-list');
    this.createBtn = page.getByTestId('row-policies-create-btn');
    this.simulatorToggleBtn = page.getByTestId('row-policies-simulator-toggle');
    this.simulator = page.getByTestId('row-policies-simulator');
    this.editor = page.getByTestId('row-policy-editor');
    this.editorForm = page.getByTestId('row-policy-editor-form');
    this.deleteDialog = page.getByTestId('row-policy-delete-dialog');
    this.columnMasksLoading = page.getByTestId('column-masks-loading');
    this.columnMasksErrorState = page.getByTestId('column-masks-error');
    this.columnMasksEmptyState = page.getByTestId('column-masks-empty');
    this.columnMasksList = page.getByTestId('column-masks-list');
    this.columnMasksCreateBtn = page.getByTestId('column-masks-create-btn');
    this.columnMasksSimulatorToggleBtn = page.getByTestId(
      'column-masks-simulator-toggle',
    );
    this.columnMasksSimulator = page.getByTestId('column-masks-simulator');
    this.columnMaskEditor = page.getByTestId('column-mask-editor');
    this.columnMaskEditorForm = page.getByTestId('column-mask-editor-form');
    this.columnMaskDeleteDialog = page.getByTestId('column-mask-delete-dialog');
  }

  async goto(ontology: string): Promise<void> {
    await this.page.goto(
      `/admin/${encodeURIComponent(ontology)}/security`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  tabBtn(id: 'row' | 'column' | 'cell'): Locator {
    return this.page.locator(
      `[data-testid="security-policies-tab"][data-tab-id="${id}"]`,
    );
  }

  rowByPolicyRid(rid: string): Locator {
    return this.page.locator(
      `[data-testid="row-policies-row"][data-policy-rid="${rid}"]`,
    );
  }

  editButton(rid: string): Locator {
    return this.page.locator(
      `[data-testid="row-policies-edit-btn"][data-policy-rid="${rid}"]`,
    );
  }

  deleteButton(rid: string): Locator {
    return this.page.locator(
      `[data-testid="row-policies-delete-btn"][data-policy-rid="${rid}"]`,
    );
  }

  // Editor sub-locators
  editorObjectTypeSelect(): Locator {
    return this.page.getByTestId('row-policy-editor-objectType');
  }
  editorPredicate(): Locator {
    return this.page.getByTestId('row-policy-editor-predicate');
  }
  editorPredicateError(): Locator {
    return this.page.getByTestId('row-policy-editor-predicate-error');
  }
  editorPredicateOk(): Locator {
    return this.page.getByTestId('row-policy-editor-predicate-ok');
  }
  editorRoles(): Locator {
    return this.page.getByTestId('row-policy-editor-roles');
  }
  editorGroups(): Locator {
    return this.page.getByTestId('row-policy-editor-groups');
  }
  editorUsers(): Locator {
    return this.page.getByTestId('row-policy-editor-users');
  }
  editorDescription(): Locator {
    return this.page.getByTestId('row-policy-editor-description');
  }
  editorSubmitBtn(): Locator {
    return this.page.getByTestId('row-policy-editor-submit-btn');
  }
  editorCancelBtn(): Locator {
    return this.page.getByTestId('row-policy-editor-cancel-btn');
  }

  // Delete dialog sub-locators
  deleteConfirmBtn(): Locator {
    return this.page.getByTestId('row-policy-delete-confirm-btn');
  }
  deleteCancelBtn(): Locator {
    return this.page.getByTestId('row-policy-delete-cancel-btn');
  }

  // Simulator sub-locators
  simulatorUserId(): Locator {
    return this.page.getByTestId('row-policies-simulator-user-id');
  }
  simulatorEmail(): Locator {
    return this.page.getByTestId('row-policies-simulator-email');
  }
  simulatorRoles(): Locator {
    return this.page.getByTestId('row-policies-simulator-roles');
  }
  simulatorGroups(): Locator {
    return this.page.getByTestId('row-policies-simulator-groups');
  }
  simulatorMatchCount(): Locator {
    return this.page.getByTestId('row-policies-simulator-match-count');
  }
  simulatorDecisionRow(rid: string): Locator {
    return this.page.locator(
      `[data-testid="row-policies-simulator-decision-row"][data-policy-rid="${rid}"]`,
    );
  }

  // ----- Column Masks (US-042) -----

  columnMaskRowByRid(rid: string): Locator {
    return this.page.locator(
      `[data-testid="column-masks-row"][data-mask-rid="${rid}"]`,
    );
  }
  columnMaskEditButton(rid: string): Locator {
    return this.page.locator(
      `[data-testid="column-masks-edit-btn"][data-mask-rid="${rid}"]`,
    );
  }
  columnMaskDeleteButton(rid: string): Locator {
    return this.page.locator(
      `[data-testid="column-masks-delete-btn"][data-mask-rid="${rid}"]`,
    );
  }

  // Column Mask editor sub-locators
  columnMaskEditorObjectTypeSelect(): Locator {
    return this.page.getByTestId('column-mask-editor-objectType');
  }
  columnMaskEditorPropertyInput(): Locator {
    return this.page.getByTestId('column-mask-editor-property');
  }
  columnMaskEditorRuleSelect(): Locator {
    return this.page.getByTestId('column-mask-editor-rule');
  }
  columnMaskEditorRoles(): Locator {
    return this.page.getByTestId('column-mask-editor-roles');
  }
  columnMaskEditorGroups(): Locator {
    return this.page.getByTestId('column-mask-editor-groups');
  }
  columnMaskEditorUsers(): Locator {
    return this.page.getByTestId('column-mask-editor-users');
  }
  columnMaskEditorDescription(): Locator {
    return this.page.getByTestId('column-mask-editor-description');
  }
  columnMaskEditorSubmitBtn(): Locator {
    return this.page.getByTestId('column-mask-editor-submit-btn');
  }
  columnMaskEditorCancelBtn(): Locator {
    return this.page.getByTestId('column-mask-editor-cancel-btn');
  }

  // Column Mask delete-dialog sub-locators
  columnMaskDeleteConfirmBtn(): Locator {
    return this.page.getByTestId('column-mask-delete-confirm-btn');
  }
  columnMaskDeleteCancelBtn(): Locator {
    return this.page.getByTestId('column-mask-delete-cancel-btn');
  }

  // Column Mask simulator sub-locators
  columnMaskSimulatorUserId(): Locator {
    return this.page.getByTestId('column-masks-simulator-user-id');
  }
  columnMaskSimulatorEmail(): Locator {
    return this.page.getByTestId('column-masks-simulator-email');
  }
  columnMaskSimulatorRoles(): Locator {
    return this.page.getByTestId('column-masks-simulator-roles');
  }
  columnMaskSimulatorGroups(): Locator {
    return this.page.getByTestId('column-masks-simulator-groups');
  }
  columnMaskSimulatorExemptCount(): Locator {
    return this.page.getByTestId('column-masks-simulator-exempt-count');
  }
  columnMaskSimulatorMaskedCount(): Locator {
    return this.page.getByTestId('column-masks-simulator-masked-count');
  }
  columnMaskSimulatorDecisionRow(rid: string): Locator {
    return this.page.locator(
      `[data-testid="column-masks-simulator-decision-row"][data-mask-rid="${rid}"]`,
    );
  }
}
