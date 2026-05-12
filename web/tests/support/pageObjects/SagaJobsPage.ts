import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/actions/:ontology/jobs` — the Saga Jobs monitoring
 * page rendered by `src/components/sagaJobs/SagaJobsPage.tsx` (US-044,
 * PC-A08).
 *
 * The page renders four mutually-exclusive list states (loading /
 * error / empty / loaded) plus two drawer overlays: the saga detail
 * drawer (per-row Inspect) and the DLQ drawer (Replay / Discard with
 * inline two-step confirmation).
 */
export class SagaJobsPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly emptyState: Locator;
  readonly list: Locator;
  readonly filters: Locator;
  readonly openDLQButton: Locator;
  readonly detailDrawer: Locator;
  readonly dlqDrawer: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('saga-jobs-page');
    this.loading = page.getByTestId('saga-jobs-loading');
    this.errorState = page.getByTestId('saga-jobs-error');
    this.emptyState = page.getByTestId('saga-jobs-empty');
    this.list = page.getByTestId('saga-jobs-list');
    this.filters = page.getByTestId('saga-jobs-filters');
    this.openDLQButton = page.getByTestId('saga-jobs-open-dlq-btn');
    this.detailDrawer = page.getByTestId('saga-detail-drawer');
    this.dlqDrawer = page.getByTestId('saga-dlq-drawer');
  }

  async goto(ontology: string): Promise<void> {
    await this.page.goto(
      `/actions/${encodeURIComponent(ontology)}/jobs`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  filterTab(value: string): Locator {
    return this.page.getByTestId(`saga-jobs-filter-${value.toLowerCase()}`);
  }

  rowBySagaId(sagaId: string): Locator {
    return this.page.locator(
      `[data-testid="saga-row"][data-saga-id="${sagaId}"]`,
    );
  }

  rowStatusBadge(sagaId: string): Locator {
    return this.rowBySagaId(sagaId).getByTestId('saga-row-status-badge');
  }

  inspectButton(sagaId: string): Locator {
    return this.page.locator(
      `[data-testid="saga-row-open-btn"][data-saga-id="${sagaId}"]`,
    );
  }

  // Saga detail drawer locators
  detailStatusBadge(): Locator {
    return this.page.getByTestId('saga-detail-status-badge');
  }
  detailCompensationCount(): Locator {
    return this.page.getByTestId('saga-detail-compensation-count');
  }
  detailDLQLink(): Locator {
    return this.page.getByTestId('saga-detail-dlq-link');
  }
  timeline(): Locator {
    return this.page.getByTestId('saga-detail-timeline');
  }
  stepRows(): Locator {
    return this.page.locator('[data-testid="saga-step-row"]');
  }
  stepRowByStepId(stepId: string): Locator {
    return this.page.locator(
      `[data-testid="saga-step-row"][data-step-id="${stepId}"]`,
    );
  }

  // DLQ drawer locators
  dlqRows(): Locator {
    return this.page.locator('[data-testid="saga-dlq-drawer-row"]');
  }
  dlqRowByDlqId(dlqId: string): Locator {
    return this.page.locator(
      `[data-testid="saga-dlq-drawer-row"][data-dlq-id="${dlqId}"]`,
    );
  }
  dlqReplayButton(dlqId: string): Locator {
    return this.page.locator(
      `[data-testid="saga-dlq-drawer-replay-btn"][data-dlq-id="${dlqId}"]`,
    );
  }
  dlqDiscardButton(dlqId: string): Locator {
    return this.page.locator(
      `[data-testid="saga-dlq-drawer-discard-btn"][data-dlq-id="${dlqId}"]`,
    );
  }
  dlqConfirm(): Locator {
    return this.page.getByTestId('saga-dlq-drawer-confirm');
  }
  dlqConfirmYes(): Locator {
    return this.page.getByTestId('saga-dlq-drawer-confirm-yes-btn');
  }
  dlqConfirmNo(): Locator {
    return this.page.getByTestId('saga-dlq-drawer-confirm-no-btn');
  }
}
