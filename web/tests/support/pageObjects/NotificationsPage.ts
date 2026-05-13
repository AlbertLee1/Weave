import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/notifications` — the full Notifications & Mentions
 * inbox rendered by `src/components/notifications/NotificationsPage.tsx`
 * (US-050, PC-A15).
 *
 * Complements the Topbar bell + `NotificationCenter` slide panel by
 * exposing a full page with type tabs, read-status filter, and
 * client-side pagination over `/api/v2/notifications`.
 */
export class NotificationsPage {
  readonly page: Page;
  readonly root: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly emptyState: Locator;
  readonly list: Locator;
  readonly filters: Locator;
  readonly tabs: Locator;
  readonly readFilter: Locator;
  readonly markAllButton: Locator;
  readonly pagination: Locator;
  readonly pageIndicator: Locator;
  readonly prevPageButton: Locator;
  readonly nextPageButton: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('notifications-page');
    this.loading = page.getByTestId('notifications-loading');
    this.errorState = page.getByTestId('notifications-error');
    this.emptyState = page.getByTestId('notifications-empty');
    this.list = page.getByTestId('notifications-list');
    this.filters = page.getByTestId('notifications-filters');
    this.tabs = page.getByTestId('notifications-tabs');
    this.readFilter = page.getByTestId('notifications-read-filter');
    this.markAllButton = page.getByTestId('notifications-mark-all-btn');
    this.pagination = page.getByTestId('notifications-pagination');
    this.pageIndicator = page.getByTestId('notifications-page-indicator');
    this.prevPageButton = page.getByTestId('notifications-prev-page-btn');
    this.nextPageButton = page.getByTestId('notifications-next-page-btn');
  }

  async goto(): Promise<void> {
    await this.page.goto('/notifications');
    await this.page.waitForLoadState('domcontentloaded');
  }

  tab(
    key: 'all' | 'mention' | 'watch' | 'approval' | 'system',
  ): Locator {
    return this.page.getByTestId(`notifications-tab-${key}`);
  }

  tabCountBadge(
    key: 'all' | 'mention' | 'watch' | 'approval' | 'system',
  ): Locator {
    return this.page.getByTestId(`notifications-tab-count-${key}`);
  }

  readOption(key: 'all' | 'unread'): Locator {
    return this.page.getByTestId(`notifications-read-${key}`);
  }

  rows(): Locator {
    return this.page.locator('[data-testid^="notifications-row-"]');
  }

  rowById(id: string): Locator {
    return this.page.locator(
      `[data-testid="notifications-row-${id}"]`,
    );
  }
}
