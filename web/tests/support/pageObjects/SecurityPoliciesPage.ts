import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/admin/:ontology/security` — the Security Policies UI
 * rendered by `src/components/securityPolicies/SecurityPoliciesPage.tsx`
 * (US-041, PC-A07a). Selectors mirror the data-testid attributes baked
 * into the production component.
 */
export class SecurityPoliciesPage {
  readonly page: Page;
  readonly root: Locator;
  readonly tabs: Locator;
  readonly tabPanel: Locator;
  readonly rowPoliciesTab: Locator;
  readonly columnMasksPlaceholder: Locator;
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

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('security-policies-page');
    this.tabs = page.getByTestId('security-policies-tabs');
    this.tabPanel = page.getByTestId('security-policies-tab-panel');
    this.rowPoliciesTab = page.getByTestId('row-policies-tab');
    this.columnMasksPlaceholder = page.getByTestId(
      'security-policies-column-placeholder',
    );
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
}
