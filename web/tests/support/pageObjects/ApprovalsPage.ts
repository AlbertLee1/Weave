import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/approvals/:ontology` — the Approval Queue page rendered
 * by `src/components/approvals/ApprovalsPage.tsx`.
 *
 * Mirrors the four-state testid template established by US-021/022 (root /
 * loading / error / empty wrappers) so spec scenarios can lock the state
 * branch without depending on the i18n copy in the shared EmptyState
 * subcomponent. New scenarios should add helper methods on demand rather
 * than fattening this surface preemptively.
 */
export class ApprovalsPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly error: Locator;
  readonly empty: Locator;
  readonly noOntology: Locator;
  readonly filters: Locator;
  readonly mineToggle: Locator;
  readonly filterStatusPending: Locator;
  readonly filterStatusApproved: Locator;
  readonly filterStatusRejected: Locator;
  readonly filterStatusAll: Locator;
  readonly list: Locator;
  readonly cards: Locator;
  readonly modalOverlay: Locator;
  readonly reasonInput: Locator;
  readonly reviewSubmit: Locator;
  readonly reviewAlert: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('approvals-page');
    this.loading = page.getByTestId('approvals-loading');
    this.error = page.getByTestId('approvals-error');
    this.empty = page.getByTestId('approvals-empty');
    this.noOntology = page.getByTestId('approvals-no-ontology');
    this.filters = page.getByTestId('approvals-filters');
    this.mineToggle = page.getByTestId('approvals-mine-toggle');
    this.filterStatusPending = page.getByTestId('filter-status-pending');
    this.filterStatusApproved = page.getByTestId('filter-status-approved');
    this.filterStatusRejected = page.getByTestId('filter-status-rejected');
    this.filterStatusAll = page.getByTestId('filter-status-all');
    this.list = page.getByTestId('approvals-list');
    this.cards = page.getByTestId('approval-card');
    this.modalOverlay = page.getByTestId('modal-overlay');
    this.reasonInput = page.getByTestId('review-reason-input');
    this.reviewSubmit = page.getByTestId('review-submit');
    this.reviewAlert = page.getByTestId('modal-overlay').getByRole('alert');
  }

  async goto(ontologyApiName: string): Promise<void> {
    await this.page.goto(
      `/approvals/${encodeURIComponent(ontologyApiName)}`,
    );
    await this.page.waitForLoadState('domcontentloaded');
  }

  cardByApprovalId(approvalId: string): Locator {
    return this.page.locator(
      `[data-testid="approval-card"][data-approval-id="${approvalId}"]`,
    );
  }

  approveButton(approvalId: string): Locator {
    return this.cardByApprovalId(approvalId).getByTestId('approval-approve-btn');
  }

  rejectButton(approvalId: string): Locator {
    return this.cardByApprovalId(approvalId).getByTestId('approval-reject-btn');
  }

  parametersBlock(approvalId: string): Locator {
    return this.cardByApprovalId(approvalId).getByTestId('approval-parameters');
  }
}
