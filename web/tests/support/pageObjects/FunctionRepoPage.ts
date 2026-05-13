import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/functions/:ontology/:functionRid/repo` — the
 * Function Code Repository surface rendered by
 * `src/components/functions/FunctionRepoPage.tsx` (US-046, PC-A03).
 *
 * Surfaces:
 *   - Header subject (function name + version + ontology metadata).
 *   - Left commit rail (clickable rows with `data-commit-hash`).
 *   - Centre commit detail (diff viewer + replay launcher).
 *   - Right version rail (read-only list with
 *     `data-version`/`data-version-current` rows; pinning is honest-
 *     mapped to a `data-pin-supported="false"` attribute on the rail).
 *   - Replay drawer (form + result panel with `data-replay-match` +
 *     `data-execution-id`).
 */
export class FunctionRepoPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly subject: Locator;
  readonly commitList: Locator;
  readonly commitListEmpty: Locator;
  readonly commitListLoading: Locator;
  readonly detailPane: Locator;
  readonly detailPlaceholder: Locator;
  readonly diffViewer: Locator;
  readonly commitMeta: Locator;
  readonly replayButton: Locator;
  readonly versionsRail: Locator;
  readonly versionsEmpty: Locator;
  readonly versionsNote: Locator;
  readonly replayDrawer: Locator;
  readonly replayOverlay: Locator;
  readonly replayForm: Locator;
  readonly replayVersionInput: Locator;
  readonly replayInput: Locator;
  readonly replaySubmit: Locator;
  readonly replayClose: Locator;
  readonly replayParseError: Locator;
  readonly replayServerError: Locator;
  readonly replayResult: Locator;
  readonly replayMatchBadge: Locator;
  readonly replayResultBody: Locator;
  readonly replayExecutionId: Locator;
  readonly replayWarning: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('function-repo-page');
    this.loading = page.getByTestId('function-repo-loading');
    this.errorState = page.getByTestId('function-repo-error');
    this.subject = page.getByTestId('function-repo-subject');
    this.commitList = page.getByTestId('function-repo-commit-list');
    this.commitListEmpty = page.getByTestId('function-repo-commit-list-empty');
    this.commitListLoading = page.getByTestId(
      'function-repo-commit-list-loading',
    );
    this.detailPane = page.getByTestId('function-repo-detail-pane');
    this.detailPlaceholder = page.getByTestId(
      'function-repo-detail-placeholder',
    );
    this.diffViewer = page.getByTestId('function-repo-diff-viewer');
    this.commitMeta = page.getByTestId('function-repo-commit-meta');
    this.replayButton = page.getByTestId('function-repo-replay-btn');
    this.versionsRail = page.getByTestId('function-repo-versions');
    this.versionsEmpty = page.getByTestId('function-repo-versions-empty');
    this.versionsNote = page.getByTestId('function-repo-versions-note');
    this.replayDrawer = page.getByTestId('function-repo-replay-drawer');
    this.replayOverlay = page.getByTestId('function-repo-replay-overlay');
    this.replayForm = page.getByTestId('function-repo-replay-form');
    this.replayVersionInput = page.getByTestId(
      'function-repo-replay-version-input',
    );
    this.replayInput = page.getByTestId('function-repo-replay-input');
    this.replaySubmit = page.getByTestId('function-repo-replay-submit-btn');
    this.replayClose = page.getByTestId('function-repo-replay-close-btn');
    this.replayParseError = page.getByTestId(
      'function-repo-replay-parse-error',
    );
    this.replayServerError = page.getByTestId('function-repo-replay-error');
    this.replayResult = page.getByTestId('function-repo-replay-result');
    this.replayMatchBadge = page.getByTestId(
      'function-repo-replay-match-badge',
    );
    this.replayResultBody = page.getByTestId(
      'function-repo-replay-result-body',
    );
    this.replayExecutionId = page.getByTestId(
      'function-repo-replay-execution-id',
    );
    this.replayWarning = page.getByTestId('function-repo-replay-warning');
  }

  async goto(ontology: string, functionRid: string): Promise<void> {
    await this.page.goto(
      `/functions/${encodeURIComponent(ontology)}/${encodeURIComponent(functionRid)}/repo`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  commitRows(): Locator {
    return this.page.locator('[data-testid="function-repo-commit-row"]');
  }
  commitRowByHash(hash: string): Locator {
    return this.page.locator(
      `[data-testid="function-repo-commit-row"][data-commit-hash="${hash}"]`,
    );
  }
  versionRows(): Locator {
    return this.page.locator('[data-testid="function-repo-version-row"]');
  }
  versionRowByVersion(version: string): Locator {
    return this.page.locator(
      `[data-testid="function-repo-version-row"][data-version="${version}"]`,
    );
  }
}
