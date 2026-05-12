import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for the Explorer-page branch surfaces:
 *
 *   - `BranchPicker` rendered in the global `Topbar` whenever the URL
 *     carries an `:ontology` param (`src/components/layout/BranchPicker.tsx`).
 *   - `BranchDiffPage` at `/explorer/:ontology/branches/:branch/diff`
 *     (`src/components/explorer/BranchDiffPage.tsx`).
 *   - `BranchReconcilePage` at `/explorer/:ontology/branches/:branch/reconcile`
 *     (`src/components/explorer/BranchReconcilePage.tsx`).
 *
 * The single class follows the multi-surface pattern from US-034
 * (`QuiverPage` / `QuiverViewPage`): scenarios cross from picker → diff
 * → reconcile without juggling three independent page objects.
 *
 * Selector conventions (mirroring US-021/022/026/029/030/032):
 *   - Wrapper testids for each surface (`branch-picker-*` / `branch-diff-*` /
 *     `branch-reconcile-*`).
 *   - Per-row testids derived from a stable id (branch id / conflict
 *     resolution key) so multi-row scenarios are not nth-child fragile.
 *   - aria-label is reused when the production component already exposes
 *     one (e.g. the picker trigger), per US-027 "don't add testid where
 *     aria-label is already stable".
 */
export class ExplorerBranchPage {
  readonly page: Page;

  // Explorer root (used to confirm the topbar BranchPicker is mounted on
  // an ontology-scoped route).
  readonly explorerRoot: Locator;

  // BranchPicker (topbar) — the only branch-switching surface today.
  readonly pickerTrigger: Locator;
  readonly pickerActive: Locator;
  readonly pickerMenu: Locator;
  readonly pickerLoading: Locator;

  // BranchDiffPage.
  readonly diffOpenReconcile: Locator;

  // BranchReconcilePage.
  readonly reconcileRoot: Locator;
  readonly reconcileStatus: Locator;
  readonly reconcileConflictCount: Locator;
  readonly reconcileConflictsSection: Locator;
  readonly reconcileChangesSection: Locator;
  readonly reconcileMergeButton: Locator;
  readonly reconcileError: Locator;
  readonly reconcileSuccess: Locator;

  constructor(page: Page) {
    this.page = page;
    this.explorerRoot = page.getByTestId('explorer-page');

    this.pickerTrigger = page.getByTestId('branch-picker-trigger');
    this.pickerActive = page.getByTestId('branch-picker-active');
    this.pickerMenu = page.getByTestId('branch-picker-menu');
    this.pickerLoading = page.getByTestId('branch-picker-loading');

    this.diffOpenReconcile = page.getByTestId('branch-diff-open-reconcile');

    this.reconcileRoot = page.getByTestId('branch-reconcile-page');
    this.reconcileStatus = page.getByTestId('reconcile-status');
    this.reconcileConflictCount = page.getByTestId('reconcile-conflict-count');
    this.reconcileConflictsSection = page.getByTestId(
      'reconcile-conflicts-section',
    );
    this.reconcileChangesSection = page.getByTestId(
      'reconcile-changes-section',
    );
    this.reconcileMergeButton = page.getByTestId('branch-reconcile-merge-button');
    this.reconcileError = page.getByTestId('reconcile-error');
    this.reconcileSuccess = page.getByTestId('reconcile-success');
  }

  async gotoExplorer(ontologyApiName: string): Promise<void> {
    await this.page.goto(`/explorer/${encodeURIComponent(ontologyApiName)}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  async gotoDiff(
    ontologyApiName: string,
    branchId: string,
  ): Promise<void> {
    await this.page.goto(
      `/explorer/${encodeURIComponent(ontologyApiName)}/branches/${encodeURIComponent(branchId)}/diff`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  async gotoReconcile(
    ontologyApiName: string,
    branchId: string,
  ): Promise<void> {
    await this.page.goto(
      `/explorer/${encodeURIComponent(ontologyApiName)}/branches/${encodeURIComponent(branchId)}/reconcile`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  pickerOption(branchId: string): Locator {
    return this.page.getByTestId(`branch-picker-option-${branchId}`);
  }

  conflictRow(resolutionKey: string): Locator {
    return this.page.getByTestId(`reconcile-conflict-${resolutionKey}`);
  }

  conflictUseMain(resolutionKey: string): Locator {
    return this.page.getByTestId(
      `reconcile-conflict-${resolutionKey}-use-main`,
    );
  }

  conflictUseBranch(resolutionKey: string): Locator {
    return this.page.getByTestId(
      `reconcile-conflict-${resolutionKey}-use-branch`,
    );
  }

  diffEntry(resolutionKey: string): Locator {
    return this.page.getByTestId(`reconcile-entry-${resolutionKey}`);
  }
}
