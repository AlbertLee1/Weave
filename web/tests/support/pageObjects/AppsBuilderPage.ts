import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for the App Editor (`/apps`, `/apps/:rid`) rendered by
 * `src/components/apps/AppEditorPage.tsx`.
 *
 * The editor composes (from left to right at the ≥lg breakpoint):
 *   - the Component Palette (`app-palette` + per-type
 *     `app-palette-item-<type>` buttons that double as drag sources),
 *   - the Variables panel below it,
 *   - the Canvas (`app-canvas`) where dropped instances render as
 *     `app-canvas-instance`,
 *   - the right-hand Property panel (`app-property-panel`).
 *
 * On the new-App route the Template picker (`app-template-picker`) is
 * auto-shown above the canvas; the "Start blank" button dismisses it,
 * and each card carries an `app-template-use-<id>` button that applies
 * the scaffold.
 *
 * The Preview mode toggle (`app-mode-toggle`) swaps the edit chrome for
 * the runtime view (`app-runtime-view`), within which `{{var}}`
 * substitution and onClick dispatch happen.
 *
 * Selectors here cover only what the BDD spec interacts with; new
 * scenarios should grow this surface on demand rather than pre-emptive
 * coverage.
 */
export class AppsBuilderPage {
  readonly page: Page;
  readonly root: Locator;
  readonly nameInput: Locator;
  readonly modeToggle: Locator;
  readonly templatesToggle: Locator;
  readonly saveButton: Locator;
  readonly saveStatus: Locator;
  readonly palette: Locator;
  readonly canvas: Locator;
  readonly canvasEmpty: Locator;
  readonly propertyPanel: Locator;
  readonly variablesPanel: Locator;
  readonly variablesAdd: Locator;
  readonly templatePicker: Locator;
  readonly templatePickerBlank: Locator;
  readonly runtimeView: Locator;
  readonly runtimeState: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('app-editor-page');
    this.nameInput = page.getByTestId('app-name-input');
    this.modeToggle = page.getByTestId('app-mode-toggle');
    this.templatesToggle = page.getByTestId('app-templates-toggle');
    this.saveButton = page.getByTestId('app-save');
    this.saveStatus = page.getByTestId('app-save-status');
    this.palette = page.getByTestId('app-palette');
    this.canvas = page.getByTestId('app-canvas');
    this.canvasEmpty = page.getByTestId('app-canvas-empty');
    this.propertyPanel = page.getByTestId('app-property-panel');
    this.variablesPanel = page.getByTestId('app-variables-panel');
    this.variablesAdd = page.getByTestId('app-variables-add');
    this.templatePicker = page.getByTestId('app-template-picker');
    this.templatePickerBlank = page.getByTestId('app-template-picker-blank');
    this.runtimeView = page.getByTestId('app-runtime-view');
    this.runtimeState = page.getByTestId('app-runtime-state');
  }

  async goto(rid?: string): Promise<void> {
    const path = rid ? `/apps/${rid}` : '/apps';
    await this.page.goto(path);
    await this.page.waitForLoadState('domcontentloaded');
  }

  paletteItem(type: 'table' | 'form' | 'chart' | 'button' | 'objectCard' | 'text'): Locator {
    return this.page.getByTestId(`app-palette-item-${type}`);
  }

  /**
   * Adds an instance of the given component type to the canvas.
   *
   * The palette button's click handler calls the same `addInstance(type)`
   * function the canvas drop handler invokes (see
   * AppEditorPage.tsx:692-694 vs :336-338), so a click here exercises the
   * identical code path as a true HTML5 drop while sidestepping
   * Playwright's flaky native-DnD bridge. Drag-source semantics
   * (`draggable`, the palette MIME) are verified separately via
   * attribute assertions in the spec.
   */
  async addFromPalette(
    type: 'table' | 'form' | 'chart' | 'button' | 'objectCard' | 'text',
  ): Promise<void> {
    await this.paletteItem(type).click();
  }

  canvasInstances(): Locator {
    return this.canvas.getByTestId('app-canvas-instance');
  }

  canvasInstance(componentType: string): Locator {
    return this.canvas.locator(
      `[data-testid="app-canvas-instance"][data-component-type="${componentType}"]`,
    );
  }

  removeButton(componentType: string): Locator {
    return this.canvasInstance(componentType).getByTestId(
      'app-canvas-instance-remove',
    );
  }

  templateCard(id: 'crm-dashboard' | 'approval-console' | 'object-browser'): Locator {
    return this.page.getByTestId(`app-template-card-${id}`);
  }

  templateUseButton(
    id: 'crm-dashboard' | 'approval-console' | 'object-browser',
  ): Locator {
    return this.page.getByTestId(`app-template-use-${id}`);
  }

  variableRow(index: number): Locator {
    return this.page
      .getByTestId('app-variable-row')
      .nth(index);
  }

  variableName(index: number): Locator {
    return this.page.getByTestId(`app-variable-name-${index}`);
  }

  variableDefault(index: number): Locator {
    return this.page.getByTestId(`app-variable-default-${index}`);
  }

  /**
   * Property field locator scoped by `<componentType>.<propertyKey>` —
   * see AppEditorPage.tsx's `prop-<componentType>-<key>` testid scheme.
   */
  propField(
    componentType: 'table' | 'form' | 'chart' | 'button' | 'objectCard' | 'text',
    key: string,
  ): Locator {
    return this.page.getByTestId(`prop-${componentType}-${key}`);
  }
}
