import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/proposals/:ontology` — the Ontology Proposals & Merge
 * UI rendered by `src/components/proposals/ProposalsPage.tsx` (US-040,
 * PC-A02). Selectors mirror the data-testid attributes baked into the
 * production component.
 */
export class ProposalsPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly emptyState: Locator;
  readonly list: Locator;
  readonly filters: Locator;
  readonly detailEmpty: Locator;
  readonly detail: Locator;
  readonly mergeDialog: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('proposals-page');
    this.loading = page.getByTestId('proposals-loading');
    this.errorState = page.getByTestId('proposals-error');
    this.emptyState = page.getByTestId('proposals-empty');
    this.list = page.getByTestId('proposals-list');
    this.filters = page.getByTestId('proposals-filters');
    this.detailEmpty = page.getByTestId('proposals-detail-empty');
    this.detail = page.getByTestId('proposals-detail');
    this.mergeDialog = page.getByTestId('proposals-merge-dialog');
  }

  async goto(ontology: string): Promise<void> {
    await this.page.goto(`/proposals/${encodeURIComponent(ontology)}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  filterBtn(value: string): Locator {
    return this.page.locator(
      `[data-testid="proposals-filter-btn"][data-filter="${value}"]`,
    );
  }

  rowByProposalId(proposalId: string): Locator {
    return this.page.locator(
      `[data-testid="proposals-row"][data-proposal-id="${proposalId}"]`,
    );
  }

  rowStatusBadge(proposalId: string): Locator {
    return this.rowByProposalId(proposalId).getByTestId(
      'proposals-row-status-badge',
    );
  }

  rowTitle(proposalId: string): Locator {
    return this.rowByProposalId(proposalId).getByTestId('proposals-row-title');
  }

  // Detail panel locators
  approveBtn(): Locator {
    return this.page.getByTestId('proposals-approve-btn');
  }
  rejectBtn(): Locator {
    return this.page.getByTestId('proposals-reject-btn');
  }
  mergeBtn(): Locator {
    return this.page.getByTestId('proposals-merge-btn');
  }
  detailStatusBadge(): Locator {
    return this.page.getByTestId('proposals-detail-status-badge');
  }
  detailTitle(): Locator {
    return this.page.getByTestId('proposals-detail-title');
  }
  reviewerInput(): Locator {
    return this.page.getByTestId('proposals-reviewer-input');
  }
  reasonInput(): Locator {
    return this.page.getByTestId('proposals-reason-input');
  }
  breakingBanner(): Locator {
    return this.page.getByTestId('proposals-breaking-banner');
  }
  breakingClean(): Locator {
    return this.page.getByTestId('proposals-breaking-clean');
  }
  breakingItems(): Locator {
    return this.page.locator('[data-testid="proposals-breaking-item"]');
  }
  diffSection(): Locator {
    return this.page.getByTestId('proposals-diff');
  }
  diffRows(): Locator {
    return this.page.locator('[data-testid="proposals-diff-row"]');
  }
  diffSummary(variant: 'ADDED' | 'MODIFIED' | 'DELETED'): Locator {
    return this.page.locator(
      `[data-testid="proposals-diff-summary"][data-variant="${variant}"]`,
    );
  }
  reviewRows(): Locator {
    return this.page.locator('[data-testid="proposals-review-row"]');
  }

  // Merge confirm dialog
  mergeDialogInput(): Locator {
    return this.page.getByTestId('proposals-merge-dialog-input');
  }
  mergeDialogConfirm(): Locator {
    return this.page.getByTestId('proposals-merge-dialog-confirm-btn');
  }
  mergeDialogCancel(): Locator {
    return this.page.getByTestId('proposals-merge-dialog-cancel-btn');
  }
}
