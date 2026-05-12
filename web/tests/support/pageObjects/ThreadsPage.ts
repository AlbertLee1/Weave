import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/threads` — the AIP Threads collaboration page rendered
 * by `src/components/threads/ThreadsPage.tsx`.
 *
 * Locators target the testid matrix on the page (root / per-state list
 * branches / per-row affordances / composer / tree panel). New scenarios
 * should add locators on demand rather than fattening this surface
 * preemptively (same convention as ActionHistoryPage / DashboardPage).
 */
export class ThreadsPage {
  readonly page: Page;
  readonly root: Locator;
  readonly list: Locator;
  readonly listLoading: Locator;
  readonly listError: Locator;
  readonly listEmpty: Locator;
  readonly listItems: Locator;
  readonly newThreadBtn: Locator;
  readonly newThreadTitle: Locator;
  readonly newThreadProvider: Locator;
  readonly newThreadModel: Locator;
  readonly newThreadSystem: Locator;
  readonly newThreadSubmit: Locator;
  readonly modalOverlay: Locator;
  readonly conversation: Locator;
  readonly messages: Locator;
  readonly messageRows: Locator;
  readonly composerInput: Locator;
  readonly composerSend: Locator;
  readonly composerError: Locator;
  readonly treePanel: Locator;
  readonly treeNodes: Locator;
  readonly treeEmpty: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('threads-page');
    this.list = page.getByTestId('thread-list');
    this.listLoading = page.getByTestId('thread-list-loading');
    this.listError = page.getByTestId('thread-list-error');
    this.listEmpty = page.getByTestId('thread-list-empty');
    this.listItems = page.getByTestId('thread-list-item');
    this.newThreadBtn = page.getByTestId('new-thread-btn');
    this.newThreadTitle = page.getByTestId('new-thread-title');
    this.newThreadProvider = page.getByTestId('new-thread-provider');
    this.newThreadModel = page.getByTestId('new-thread-model');
    this.newThreadSystem = page.getByTestId('new-thread-system');
    this.newThreadSubmit = page.getByTestId('new-thread-submit');
    this.modalOverlay = page.getByTestId('modal-overlay');
    this.conversation = page.getByTestId('thread-conversation');
    this.messages = page.getByTestId('thread-messages');
    this.messageRows = page.getByTestId('thread-message');
    this.composerInput = page.getByTestId('composer-input');
    this.composerSend = page.getByTestId('composer-send');
    this.composerError = page.getByTestId('composer-error');
    this.treePanel = page.getByTestId('thread-tree-panel');
    this.treeNodes = page.getByTestId('thread-tree-node');
    this.treeEmpty = page.getByTestId('thread-tree-empty');
  }

  async goto(): Promise<void> {
    await this.page.goto('/threads');
    await this.page.waitForLoadState('domcontentloaded');
  }

  threadItem(threadId: string): Locator {
    return this.page.locator(
      `[data-testid="thread-list-item"][data-thread-id="${threadId}"]`,
    );
  }

  deleteButton(threadId: string): Locator {
    return this.threadItem(threadId).getByTestId('thread-delete-btn');
  }

  treeNode(messageId: number | string): Locator {
    return this.page.locator(
      `[data-testid="thread-tree-node"][data-message-id="${messageId}"]`,
    );
  }

  messageRow(messageId: number | string): Locator {
    return this.page.locator(
      `[data-testid="thread-message"][data-message-id="${messageId}"]`,
    );
  }
}
