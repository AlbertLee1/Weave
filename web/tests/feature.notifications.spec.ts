import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  NotificationsPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/notifications` — the full Notifications & Mentions
 * Centre rendered by `src/components/notifications/NotificationsPage.tsx`
 * (US-050, PC-A15).
 *
 * Scenarios map to the US-050 acceptance criteria:
 *
 *   - "顶栏铃铛 + 未读计数" (TopBar bell + badge) and "下拉面板：
 *     mentions / approvals / system 分类 + Mark all read"
 *     (NotificationCenter slide panel) → already shipped in US-340
 *     (mention/bell) + US-343 (typed tabs + bulk read). This spec
 *     covers the *new* surface: the full `/notifications` inbox.
 *   - "完整页 /notifications 含过滤与分页" → the list, type tabs,
 *     read-status filter, and pager scenarios. Each scenario asserts
 *     a different slice: render+tab counts, type filter narrowing,
 *     read-status filter narrowing, pagination splitting the slice.
 *   - "接 /api/v2/notifications 系列 endpoint" → bulk-mark-read
 *     scenario asserts the page hits `/api/v2/notifications/read-all`
 *     with the `type=<tab>` query when the active tab is scoped.
 *
 * Every scenario stubs `**\/api/v2/notifications**` via `page.route`
 * so the page renders deterministic fixtures without touching real
 * PG. Dev-mode auth grants the calling user — no extra wiring.
 */

interface MockNotification {
  id: string;
  userId: string;
  title: string;
  body: string;
  type: string;
  link?: string;
  read: boolean;
  createdAt: string;
}

interface CapturedRequest {
  url: string;
  method: string;
}

interface Stubs {
  notifications: MockNotification[];
  gets: CapturedRequest[];
  readAlls: CapturedRequest[];
  singleReads: CapturedRequest[];
}

function newStubs(): Stubs {
  return {
    notifications: [],
    gets: [],
    readAlls: [],
    singleReads: [],
  };
}

function notif(overrides: Partial<MockNotification>): MockNotification {
  return {
    id: overrides.id ?? 'notif-1',
    userId: 'alice@test',
    title: 'Sample notification',
    body: 'Body text',
    type: 'mention',
    link: undefined,
    read: false,
    createdAt: '2026-05-13T12:00:00Z',
    ...overrides,
  };
}

async function stubNotifications(page: Page, stubs: Stubs): Promise<void> {
  // Wire stubs across the entire `/api/v2/notifications` family.
  // Sub-pattern routes register first; the catch-all GET goes last so
  // narrower routes win Playwright's LIFO resolution (see US-023 /
  // US-040 / US-042 codebase notes).
  await page.route(
    '**/api/v2/notifications/*/read',
    async (route: Route) => {
      stubs.singleReads.push({
        url: route.request().url(),
        method: route.request().method(),
      });
      const match = route.request().url().match(/notifications\/([^/]+)\/read/);
      if (match) {
        const id = decodeURIComponent(match[1]);
        const idx = stubs.notifications.findIndex((n) => n.id === id);
        if (idx >= 0) stubs.notifications[idx]!.read = true;
      }
      await route.fulfill({ status: 204, body: '' });
    },
  );

  await page.route(
    '**/api/v2/notifications/read-all*',
    async (route: Route) => {
      const url = route.request().url();
      stubs.readAlls.push({ url, method: route.request().method() });
      const u = new URL(url);
      const types = u.searchParams.getAll('type');
      let updated = 0;
      for (const n of stubs.notifications) {
        if (n.read) continue;
        if (types.length === 0 || types.includes(n.type)) {
          n.read = true;
          updated += 1;
        }
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ updated }),
      });
    },
  );

  await page.route('**/api/v2/notifications*', async (route: Route) => {
    const url = route.request().url();
    const pathOnly = url.split('?')[0];
    // Guard against the sub-patterns leaking into this handler — the
    // single-read and read-all paths register first but a request
    // racing in before those routes attach must still 404 cleanly.
    if (pathOnly.endsWith('/read') || pathOnly.endsWith('/read-all')) {
      await route.continue();
      return;
    }
    stubs.gets.push({ url, method: route.request().method() });
    const u = new URL(url);
    let rows = stubs.notifications;
    if (u.searchParams.get('unread') === 'true') {
      rows = rows.filter((n) => !n.read);
    }
    const types = u.searchParams.getAll('type');
    if (types.length > 0) {
      rows = rows.filter((n) => types.includes(n.type));
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: rows }),
    });
  });
}

describeFeature('Global Notifications inbox page', () => {
  test('Scenario: renders mixed-type notifications with per-tab unread counts @smoke', async ({
    page,
    request,
  }) => {
    // AC: "完整页 /notifications 含过滤与分页" + "下拉面板：mentions /
    // approvals / system 分类". Seed 4 notifications across 4 types
    // (3 unread + 1 read). Assert: page renders, tabs strip shows
    // accurate per-tab unread counts, list renders 4 rows with
    // type+unread data attrs reflecting wire shape.
    const stubs = newStubs();
    stubs.notifications = [
      notif({
        id: 'n-mention',
        type: 'mention',
        title: 'Alice mentioned you',
        read: false,
      }),
      notif({
        id: 'n-watch',
        type: 'watch',
        title: 'Watched ObjectType changed',
        read: false,
      }),
      notif({
        id: 'n-approval',
        type: 'approval',
        title: 'Approval pending',
        read: false,
      }),
      notif({
        id: 'n-system-read',
        type: 'system',
        title: 'System note (already read)',
        read: true,
      }),
    ];

    const inbox = new NotificationsPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('four notifications exist across four types', async () => {
      await stubNotifications(page, stubs);
    });

    await When('the user opens /notifications', async () => {
      await inbox.goto();
      await expect(inbox.root).toBeVisible();
    });

    await Then('the list renders every notification row', async () => {
      await expect(inbox.list).toBeVisible();
      await expect(inbox.rows()).toHaveCount(4);
      await expect(inbox.rowById('n-mention')).toBeVisible();
      await expect(inbox.rowById('n-watch')).toBeVisible();
      await expect(inbox.rowById('n-approval')).toBeVisible();
      await expect(inbox.rowById('n-system-read')).toBeVisible();
    });

    await Then(
      'each row carries the type and read-state wire metadata',
      async () => {
        await expect(inbox.rowById('n-mention')).toHaveAttribute(
          'data-notification-type',
          'mention',
        );
        await expect(inbox.rowById('n-mention')).toHaveAttribute(
          'data-unread',
          'true',
        );
        await expect(inbox.rowById('n-system-read')).toHaveAttribute(
          'data-unread',
          'false',
        );
      },
    );

    await Then('the type tabs strip shows accurate unread counts', async () => {
      await expect(inbox.tabCountBadge('all')).toHaveText('3');
      await expect(inbox.tabCountBadge('mention')).toHaveText('1');
      await expect(inbox.tabCountBadge('watch')).toHaveText('1');
      await expect(inbox.tabCountBadge('approval')).toHaveText('1');
      // System has no unread row → no count badge rendered.
      await expect(inbox.tabCountBadge('system')).toHaveCount(0);
    });
  });

  test('Scenario: clicking a type tab narrows the list to that type @smoke', async ({
    page,
  }) => {
    // AC: "下拉面板：mentions / approvals / system 分类". Same data
    // shape as above but the assertion focus is the *filter*: switch
    // to the Mentions tab → only the mention row remains visible, the
    // other rows toHaveCount(0).
    const stubs = newStubs();
    stubs.notifications = [
      notif({
        id: 'n-mention-1',
        type: 'mention',
        title: 'Alice mentioned you in #foo',
      }),
      notif({
        id: 'n-mention-2',
        type: 'mention',
        title: 'Bob mentioned you in #bar',
      }),
      notif({ id: 'n-watch-1', type: 'watch', title: 'Watch event' }),
      notif({
        id: 'n-approval-1',
        type: 'approval',
        title: 'Approval pending',
      }),
    ];

    const inbox = new NotificationsPage(page);

    await Given('mixed notifications exist', async () => {
      await stubNotifications(page, stubs);
    });

    await When('the user opens the page', async () => {
      await inbox.goto();
      await expect(inbox.root).toBeVisible();
      await expect(inbox.rows()).toHaveCount(4);
    });

    await When('the user clicks the Mentions tab', async () => {
      await inbox.tab('mention').click();
    });

    await Then(
      'only mention rows remain visible and the active tab data attribute flips',
      async () => {
        await expect(inbox.root).toHaveAttribute('data-active-tab', 'mention');
        await expect(inbox.tab('mention')).toHaveAttribute(
          'data-active',
          'true',
        );
        await expect(inbox.rowById('n-mention-1')).toBeVisible();
        await expect(inbox.rowById('n-mention-2')).toBeVisible();
        await expect(inbox.rowById('n-watch-1')).toHaveCount(0);
        await expect(inbox.rowById('n-approval-1')).toHaveCount(0);
        await expect(inbox.rows()).toHaveCount(2);
      },
    );
  });

  test('Scenario: clicking Unread-only narrows the list to unread rows @smoke', async ({
    page,
  }) => {
    // AC: "完整页 /notifications 含过滤". The read-status filter is a
    // page-local affordance (the bell panel does not have it) so the
    // BDD locks the wire-shape independent path: clicking 'Unread'
    // toggles the data-read-filter attribute + drops the read rows
    // from the visible list. No extra GETs are dispatched (filter is
    // client-side over the cached list, matching the bell panel
    // approach).
    const stubs = newStubs();
    stubs.notifications = [
      notif({
        id: 'n-unread-a',
        type: 'mention',
        title: 'Unread mention A',
        read: false,
      }),
      notif({
        id: 'n-read-a',
        type: 'mention',
        title: 'Read mention',
        read: true,
      }),
      notif({
        id: 'n-unread-b',
        type: 'system',
        title: 'Unread system',
        read: false,
      }),
    ];

    const inbox = new NotificationsPage(page);

    await Given('mixed read/unread notifications exist', async () => {
      await stubNotifications(page, stubs);
    });

    await When('the user opens the page', async () => {
      await inbox.goto();
      await expect(inbox.root).toBeVisible();
      await expect(inbox.rows()).toHaveCount(3);
    });

    await When('the user clicks the Unread-only filter', async () => {
      await inbox.readOption('unread').click();
    });

    await Then('only unread rows remain', async () => {
      await expect(inbox.root).toHaveAttribute('data-read-filter', 'unread');
      await expect(inbox.readOption('unread')).toHaveAttribute(
        'data-active',
        'true',
      );
      await expect(inbox.rowById('n-unread-a')).toBeVisible();
      await expect(inbox.rowById('n-unread-b')).toBeVisible();
      await expect(inbox.rowById('n-read-a')).toHaveCount(0);
      await expect(inbox.rows()).toHaveCount(2);
    });

    await When('the user clicks the All filter', async () => {
      await inbox.readOption('all').click();
    });

    await Then('the read row reappears', async () => {
      await expect(inbox.root).toHaveAttribute('data-read-filter', 'all');
      await expect(inbox.rowById('n-read-a')).toBeVisible();
      await expect(inbox.rows()).toHaveCount(3);
    });
  });

  test('Scenario: pagination splits the list when the slice exceeds the page size', async ({
    page,
  }) => {
    // AC: "完整页 /notifications 含过滤与分页". Seed 23 rows so the
    // 20-row PAGE_SIZE splits into page 1 (20 rows) + page 2 (3
    // rows). Assert prev disabled on page 1, clicking Next surfaces
    // the remaining 3 rows + flips data-current-page + disables Next
    // again.
    const stubs = newStubs();
    stubs.notifications = Array.from({ length: 23 }, (_, i) =>
      notif({
        id: `n-page-${String(i).padStart(2, '0')}`,
        type: i % 2 === 0 ? 'mention' : 'system',
        title: `Notification ${i}`,
        read: false,
      }),
    );

    const inbox = new NotificationsPage(page);

    await Given('23 notifications exist', async () => {
      await stubNotifications(page, stubs);
    });

    await When('the user opens the page', async () => {
      await inbox.goto();
      await expect(inbox.root).toBeVisible();
    });

    await Then(
      'page 1 renders 20 rows with Prev disabled and Next enabled',
      async () => {
        await expect(inbox.pagination).toHaveAttribute(
          'data-current-page',
          '0',
        );
        await expect(inbox.pagination).toHaveAttribute(
          'data-total-pages',
          '2',
        );
        await expect(inbox.rows()).toHaveCount(20);
        await expect(inbox.list).toHaveAttribute('data-row-count', '23');
        await expect(inbox.list).toHaveAttribute('data-page-row-count', '20');
        await expect(inbox.prevPageButton).toBeDisabled();
        await expect(inbox.nextPageButton).toBeEnabled();
        await expect(inbox.pageIndicator).toHaveText('Page 1 of 2');
      },
    );

    await When('the user clicks Next', async () => {
      await inbox.nextPageButton.click();
    });

    await Then(
      'page 2 renders the remaining 3 rows with Next disabled and Prev enabled',
      async () => {
        await expect(inbox.pagination).toHaveAttribute(
          'data-current-page',
          '1',
        );
        await expect(inbox.rows()).toHaveCount(3);
        await expect(inbox.list).toHaveAttribute('data-page-row-count', '3');
        await expect(inbox.prevPageButton).toBeEnabled();
        await expect(inbox.nextPageButton).toBeDisabled();
        await expect(inbox.pageIndicator).toHaveText('Page 2 of 2');
        await expect(inbox.rowById('n-page-20')).toBeVisible();
        await expect(inbox.rowById('n-page-22')).toBeVisible();
        // The first-page rows are out of the current slice.
        await expect(inbox.rowById('n-page-00')).toHaveCount(0);
      },
    );

    await When('the user clicks Prev', async () => {
      await inbox.prevPageButton.click();
    });

    await Then('page 1 returns', async () => {
      await expect(inbox.pagination).toHaveAttribute(
        'data-current-page',
        '0',
      );
      await expect(inbox.rows()).toHaveCount(20);
      await expect(inbox.rowById('n-page-00')).toBeVisible();
    });
  });

  test('Scenario: Mark-mention-read posts to read-all with the type=mention scope', async ({
    page,
  }) => {
    // AC: "接 /api/v2/notifications 系列 endpoint" + "下拉面板：Mark
    // all read". The page-level bulk-read button scopes to the
    // active tab. Switch to Mentions, click Mark mention read, assert
    // the POST hits /read-all with `type=mention` query, the rows
    // flip to read in the next refetch (React Query invalidate), and
    // the Mark button disables.
    const stubs = newStubs();
    stubs.notifications = [
      notif({ id: 'n-m-1', type: 'mention', read: false }),
      notif({ id: 'n-m-2', type: 'mention', read: false }),
      notif({ id: 'n-w-1', type: 'watch', read: false }),
    ];

    const inbox = new NotificationsPage(page);

    await Given('two unread mentions and an unread watch exist', async () => {
      await stubNotifications(page, stubs);
    });

    await When('the user opens the page', async () => {
      await inbox.goto();
      await expect(inbox.root).toBeVisible();
      await expect(inbox.rows()).toHaveCount(3);
    });

    await When('the user switches to the Mentions tab', async () => {
      await inbox.tab('mention').click();
      await expect(inbox.rows()).toHaveCount(2);
    });

    await When('the user clicks Mark mention read', async () => {
      await inbox.markAllButton.click();
    });

    await Then(
      'a POST to /read-all captures the type=mention scope',
      async () => {
        await expect
          .poll(() => stubs.readAlls.length)
          .toBeGreaterThanOrEqual(1);
        const last = stubs.readAlls.at(-1)!;
        expect(last.method).toBe('POST');
        const u = new URL(last.url);
        expect(u.searchParams.getAll('type')).toEqual(['mention']);
      },
    );

    await Then(
      'after refetch the mention rows are no longer unread and the watch row stays unread',
      async () => {
        await expect
          .poll(async () => {
            const m1 = await inbox
              .rowById('n-m-1')
              .getAttribute('data-unread');
            const m2 = await inbox
              .rowById('n-m-2')
              .getAttribute('data-unread');
            return { m1, m2 };
          })
          .toEqual({ m1: 'false', m2: 'false' });
        // The watch tab was untouched (different type scope).
        await inbox.tab('watch').click();
        await expect(inbox.rowById('n-w-1')).toHaveAttribute(
          'data-unread',
          'true',
        );
      },
    );

    await Then(
      'switching back to mentions disables the Mark-mention-read button (no unread left)',
      async () => {
        await inbox.tab('mention').click();
        await expect(inbox.markAllButton).toBeDisabled();
      },
    );
  });

  test('Scenario: list-fetch error surfaces an inline error state', async ({
    page,
  }) => {
    // Edge case: backend 500. Spec asserts the error wrapper appears
    // and the loading / empty / list states are absent — locks the
    // four-state matrix contract from US-022 codebase notes.
    await page.route('**/api/v2/notifications*', async (route: Route) => {
      const url = route.request().url();
      if (url.includes('/read')) {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'INTERNAL',
          errorName: 'InternalError',
          parameters: { error: 'kaboom' },
          errorInstanceId: 'err-1',
        }),
      });
    });

    const inbox = new NotificationsPage(page);

    await When('the user opens the page', async () => {
      await inbox.goto();
      await expect(inbox.root).toBeVisible();
    });

    await Then(
      'the error wrapper renders and no list / empty state appears',
      async () => {
        await expect(inbox.errorState).toBeVisible();
        await expect(inbox.list).toHaveCount(0);
        await expect(inbox.emptyState).toHaveCount(0);
        await expect(inbox.loading).toHaveCount(0);
      },
    );
  });

  test('Scenario: empty state renders when the backend returns zero notifications', async ({
    page,
  }) => {
    // Empty: backend returns `{data:[]}`. Spec asserts the empty
    // wrapper renders and the bulk-mark button is disabled (no
    // unread to flush).
    const stubs = newStubs();
    await stubNotifications(page, stubs);

    const inbox = new NotificationsPage(page);

    await When('the user opens the page', async () => {
      await inbox.goto();
      await expect(inbox.root).toBeVisible();
    });

    await Then('the empty state renders', async () => {
      await expect(inbox.emptyState).toBeVisible();
      await expect(inbox.list).toHaveCount(0);
      await expect(inbox.markAllButton).toBeDisabled();
    });
  });
});
