import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/admin/datasets/:dataset/rollback` — the Dataset
 * Rollback wizard rendered by
 * `src/components/admin/DatasetRollbackPage.tsx` (US-053 / PC-A10).
 *
 * The page walks the operator through four explicit gates so the
 * destructive POST /api/v2/datasets/{rid}/rollback never fires
 * accidentally:
 *
 *   1. Pick — radio table of recorded transactions.
 *   2. Preview — impact summary (newer-tx + edits count).
 *   3. Confirm — re-type dataset apiName to unlock submit.
 *   4. Success — server-returned summary counts.
 *
 * Mirrors the wizard testid template established by the Import wizard
 * (US-030) — page root, `data-step` on root, step ribbon, per-step
 * container, and back / next buttons keyed by step prefix.
 */
export class DatasetRollbackPage {
  readonly page: Page;
  readonly root: Locator;
  readonly stepRibbon: Locator;
  readonly noDataset: Locator;
  readonly loading: Locator;
  readonly error: Locator;

  // Pick step
  readonly pickStep: Locator;
  readonly txTable: Locator;
  readonly txRows: Locator;
  readonly emptyTransactions: Locator;
  readonly pickNext: Locator;

  // Preview step
  readonly previewStep: Locator;
  readonly previewTarget: Locator;
  readonly impactTxCount: Locator;
  readonly impactEditsCount: Locator;
  readonly impactEmpty: Locator;
  readonly impactList: Locator;
  readonly impactRows: Locator;
  readonly previewBack: Locator;
  readonly previewNext: Locator;

  // Confirm step
  readonly confirmStep: Locator;
  readonly confirmInput: Locator;
  readonly confirmSubmit: Locator;
  readonly confirmBack: Locator;
  readonly confirmError: Locator;

  // Progress modal (mounted while mutation is pending)
  readonly progressModal: Locator;
  readonly progressBar: Locator;

  // Success step
  readonly successStep: Locator;
  readonly successRolledBack: Locator;
  readonly successRestored: Locator;
  readonly successDeleted: Locator;
  readonly successNewTx: Locator;
  readonly successClose: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('dataset-rollback-page');
    this.stepRibbon = page.getByTestId('dataset-rollback-step-ribbon');
    this.noDataset = page.getByTestId('dataset-rollback-no-dataset');
    this.loading = page.getByTestId('dataset-rollback-loading');
    this.error = page.getByTestId('dataset-rollback-error');

    this.pickStep = page.getByTestId('dataset-rollback-pick-step');
    this.txTable = page.getByTestId('dataset-rollback-tx-table');
    this.txRows = page.getByTestId('dataset-rollback-tx-row');
    this.emptyTransactions = page.getByTestId('dataset-rollback-empty');
    this.pickNext = page.getByTestId('dataset-rollback-pick-next');

    this.previewStep = page.getByTestId('dataset-rollback-preview-step');
    this.previewTarget = page.getByTestId('dataset-rollback-preview-target');
    this.impactTxCount = page.getByTestId('dataset-rollback-impact-tx-count');
    this.impactEditsCount = page.getByTestId('dataset-rollback-impact-edits-count');
    this.impactEmpty = page.getByTestId('dataset-rollback-impact-empty');
    this.impactList = page.getByTestId('dataset-rollback-impact-list');
    this.impactRows = page.getByTestId('dataset-rollback-impact-row');
    this.previewBack = page.getByTestId('dataset-rollback-preview-back');
    this.previewNext = page.getByTestId('dataset-rollback-preview-next');

    this.confirmStep = page.getByTestId('dataset-rollback-confirm-step');
    this.confirmInput = page.getByTestId('dataset-rollback-confirm-input');
    this.confirmSubmit = page.getByTestId('dataset-rollback-confirm-submit');
    this.confirmBack = page.getByTestId('dataset-rollback-confirm-back');
    this.confirmError = page.getByTestId('dataset-rollback-confirm-error');

    this.progressModal = page.getByTestId('dataset-rollback-progress-modal');
    this.progressBar = page.getByTestId('dataset-rollback-progress-bar');

    this.successStep = page.getByTestId('dataset-rollback-success-step');
    this.successRolledBack = page.getByTestId(
      'dataset-rollback-success-rolled-back',
    );
    this.successRestored = page.getByTestId('dataset-rollback-success-restored');
    this.successDeleted = page.getByTestId('dataset-rollback-success-deleted');
    this.successNewTx = page.getByTestId('dataset-rollback-success-new-tx');
    this.successClose = page.getByTestId('dataset-rollback-success-close');
  }

  async goto(dataset: string): Promise<void> {
    await this.page.goto(
      `/admin/datasets/${encodeURIComponent(dataset)}/rollback`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  txRowFor(txId: string): Locator {
    return this.page.locator(
      `[data-testid="dataset-rollback-tx-row"][data-tx-id="${txId}"]`,
    );
  }

  txRadioFor(txId: string): Locator {
    return this.page.locator(
      `[data-testid="dataset-rollback-tx-radio"][data-tx-id="${txId}"]`,
    );
  }

  impactRowFor(txId: string): Locator {
    return this.page.locator(
      `[data-testid="dataset-rollback-impact-row"][data-tx-id="${txId}"]`,
    );
  }
}
