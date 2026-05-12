import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/import/:ontology` — the CSV Import Wizard rendered by
 * `src/components/import/ImportWizardPage.tsx`.
 *
 * The wizard is a 4-step flow (Upload → Map → Preview → Import) backed
 * exclusively by testid affordances on the production page; per-step
 * controls share a single page object instead of fragmenting into one PO
 * per step because the wizard reuses the same component tree and the
 * scenarios need to walk all four steps in a single navigation.
 *
 * State-branch wrappers (heading / step indicator / dropzone) follow the
 * US-021/022 template; per-row controls (`map-{header}` selects in step 2,
 * `warn-{row}-{header}` badges in step 3) carry per-key testids so scenarios
 * can target specific CSV columns without nth-child fragility.
 */
export class ImportWizardPage {
  readonly page: Page;
  readonly heading: Locator;
  readonly stepIndicator: Locator;
  readonly dropzone: Locator;
  readonly fileInput: Locator;
  readonly browseBtn: Locator;
  readonly fileName: Locator;
  readonly parseSummary: Locator;
  readonly objectTypeSelect: Locator;
  readonly noCreateAction: Locator;
  readonly rowCount: Locator;
  readonly startImport: Locator;
  readonly resetBtn: Locator;
  readonly progressBar: Locator;
  readonly processedCount: Locator;
  readonly successCount: Locator;
  readonly failureCount: Locator;
  readonly failureSummary: Locator;
  readonly next2Btn: Locator;
  readonly next3Btn: Locator;
  readonly next4Btn: Locator;

  constructor(page: Page) {
    this.page = page;
    this.heading = page.getByTestId('import-wizard-heading');
    this.stepIndicator = page.getByTestId('step-indicator');
    this.dropzone = page.getByTestId('dropzone');
    this.fileInput = page.getByTestId('file-input');
    this.browseBtn = page.getByTestId('browse-button');
    this.fileName = page.getByTestId('file-name');
    this.parseSummary = page.getByTestId('parse-summary');
    this.objectTypeSelect = page.getByTestId('object-type-select');
    this.noCreateAction = page.getByTestId('no-create-action');
    this.rowCount = page.getByTestId('row-count');
    this.startImport = page.getByTestId('start-import');
    this.resetBtn = page.getByTestId('reset');
    this.progressBar = page.getByTestId('progress-bar');
    this.processedCount = page.getByTestId('processed-count');
    this.successCount = page.getByTestId('success-count');
    this.failureCount = page.getByTestId('failure-count');
    this.failureSummary = page.getByTestId('failure-summary');
    this.next2Btn = page.getByTestId('next-2');
    this.next3Btn = page.getByTestId('next-3');
    this.next4Btn = page.getByTestId('next-4');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(`/import/${encodeURIComponent(ontologyApiName)}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  /**
   * Upload a CSV file via the hidden `<input type="file">`. Playwright's
   * `setInputFiles` works on hidden inputs and bypasses the dropzone's
   * dragover hint, which is the same code path the production "Browse…"
   * button triggers (it just calls `fileInputRef.current?.click()`).
   */
  async uploadCsv(name: string, contents: string): Promise<void> {
    await this.fileInput.setInputFiles({
      name,
      mimeType: 'text/csv',
      buffer: Buffer.from(contents, 'utf8'),
    });
  }

  stepBadge(step: 1 | 2 | 3 | 4): Locator {
    return this.page.getByTestId(`step-${step}`);
  }

  mappingSelect(header: string): Locator {
    return this.page.getByTestId(`map-${header}`);
  }

  warningBadge(rowIndex: number, header: string): Locator {
    return this.page.getByTestId(`warn-${rowIndex}-${header}`);
  }
}
