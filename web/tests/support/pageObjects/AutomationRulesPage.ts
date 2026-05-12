import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/automation/:ontology` — the Automation Rules
 * management page rendered by
 * `src/components/automation/AutomationRulesPage.tsx` (US-039, PC-A01).
 *
 * The page renders four mutually-exclusive states (loading / error /
 * empty / loaded) plus two drawer overlays: the editor drawer (used by
 * both Create and Edit) and the executions drawer. Selectors mirror the
 * data-testid attributes baked into the production component.
 */
export class AutomationRulesPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly emptyState: Locator;
  readonly list: Locator;
  readonly createBtn: Locator;
  readonly editorDrawer: Locator;
  readonly executionsDrawer: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('automation-rules-page');
    this.loading = page.getByTestId('automation-rules-loading');
    this.errorState = page.getByTestId('automation-rules-error');
    this.emptyState = page.getByTestId('automation-rules-empty');
    this.list = page.getByTestId('automation-rules-list');
    this.createBtn = page.getByTestId('automation-rules-create-btn');
    this.editorDrawer = page.getByTestId('automation-rule-editor-drawer');
    this.executionsDrawer = page.getByTestId(
      'automation-rule-executions-drawer',
    );
  }

  async goto(ontology: string): Promise<void> {
    await this.page.goto(`/automation/${encodeURIComponent(ontology)}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  rowByRuleId(ruleId: string): Locator {
    return this.page.locator(
      `[data-testid="automation-rule-row"][data-rule-id="${ruleId}"]`,
    );
  }

  toggleButton(ruleId: string): Locator {
    return this.page.locator(
      `[data-testid="automation-rule-toggle-btn"][data-rule-id="${ruleId}"]`,
    );
  }

  editButton(ruleId: string): Locator {
    return this.page.locator(
      `[data-testid="automation-rule-edit-btn"][data-rule-id="${ruleId}"]`,
    );
  }

  executionsButton(ruleId: string): Locator {
    return this.page.locator(
      `[data-testid="automation-rule-executions-btn"][data-rule-id="${ruleId}"]`,
    );
  }

  deleteButton(ruleId: string): Locator {
    return this.page.locator(
      `[data-testid="automation-rule-delete-btn"][data-rule-id="${ruleId}"]`,
    );
  }

  rowTriggerBadge(ruleId: string): Locator {
    return this.rowByRuleId(ruleId).getByTestId(
      'automation-rule-trigger-badge',
    );
  }

  rowStatusBadge(ruleId: string): Locator {
    return this.rowByRuleId(ruleId).getByTestId(
      'automation-rule-status-badge',
    );
  }

  // Editor drawer locators
  formName(): Locator {
    return this.page.getByTestId('automation-rule-form-name');
  }
  formDescription(): Locator {
    return this.page.getByTestId('automation-rule-form-description');
  }
  formTrigger(): Locator {
    return this.page.getByTestId('automation-rule-form-trigger');
  }
  formCondition(): Locator {
    return this.page.getByTestId('automation-rule-form-condition');
  }
  formTriggerConfig(): Locator {
    return this.page.getByTestId('automation-rule-form-trigger-config');
  }
  formEffects(): Locator {
    return this.page.getByTestId('automation-rule-form-effects');
  }
  formDebounce(): Locator {
    return this.page.getByTestId('automation-rule-form-debounce');
  }
  formThrottle(): Locator {
    return this.page.getByTestId('automation-rule-form-throttle');
  }
  formError(): Locator {
    return this.page.getByTestId('automation-rule-form-error');
  }
  formSaveBtn(): Locator {
    return this.page.getByTestId('automation-rule-form-save-btn');
  }
  formCancelBtn(): Locator {
    return this.page.getByTestId('automation-rule-form-cancel-btn');
  }

  // Executions drawer
  executionsList(): Locator {
    return this.page.getByTestId('automation-rule-executions-list');
  }
  executionsEmpty(): Locator {
    return this.page.getByTestId('automation-rule-executions-empty');
  }
  executionRows(): Locator {
    return this.page.locator(
      '[data-testid="automation-rule-execution-row"]',
    );
  }
}
