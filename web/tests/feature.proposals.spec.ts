import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  ProposalsPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/proposals/:ontology` — the Ontology Proposals & Merge
 * UI rendered by `src/components/proposals/ProposalsPage.tsx` (US-040,
 * PC-A02). Scenarios cover the US-040 acceptance criteria:
 *
 *   - List page with open/approved/rejected/merged filters.
 *   - Detail panel diff view colored by ADDED/MODIFIED/DELETED.
 *   - Breaking-changes warning banner (driven by /breaking-changes API).
 *   - Approve / Reject / Merge button enablement matrix.
 *   - Merge typed-confirm dialog (Confirm button gated on title match).
 *
 * Every scenario stubs the backend through `page.route` so the page
 * renders deterministic fixtures without touching real PG / NATS.
 */

const ONTOLOGY = 'northwind';

type ProposalStatus = 'pending' | 'approved' | 'rejected' | 'merged';

interface MockProposal {
  id: string;
  branchId: string;
  ontologyRid: string;
  title: string;
  description?: string;
  status: ProposalStatus;
  author: string;
  createdAt: string;
  updatedAt: string;
}

interface MockReview {
  id: string;
  proposalId: string;
  reviewer: string;
  decision: 'approve' | 'reject';
  reason?: string;
  createdAt: string;
}

interface MockDiffEntry {
  entityType: string;
  entityRid: string;
  changeType: 'ADDED' | 'MODIFIED' | 'DELETED';
  before: unknown;
  after: unknown;
}

interface MockBreakingChange {
  kind: string;
  objectTypeApiName?: string;
  propertyApiName?: string;
  detail?: string;
}

interface CapturedRequest {
  url: string;
  method: string;
  body: unknown;
}

interface Stubs {
  proposals: MockProposal[];
  reviews: Record<string, MockReview[]>;
  diffsByBranch: Record<string, MockDiffEntry[]>;
  breakingByBranch: Record<string, MockBreakingChange[]>;
  approves: CapturedRequest[];
  rejects: CapturedRequest[];
  merges: CapturedRequest[];
  /**
   * When non-null, the next Merge POST returns 500 with this errorName so
   * a failure scenario can lock the error-toast contract. Cleared after
   * one mutation.
   */
  failNextMergeWith: string | null;
}

function newStubs(initial: Partial<Stubs> = {}): Stubs {
  return {
    proposals: initial.proposals ? initial.proposals.map((p) => ({ ...p })) : [],
    reviews: initial.reviews ?? {},
    diffsByBranch: initial.diffsByBranch ?? {},
    breakingByBranch: initial.breakingByBranch ?? {},
    approves: [],
    rejects: [],
    merges: [],
    failNextMergeWith: initial.failNextMergeWith ?? null,
  };
}

function proposalFixture(overrides: Partial<MockProposal> = {}): MockProposal {
  return {
    id: 'prop-1',
    branchId: 'br-1',
    ontologyRid: 'ri.ontology.main.ontology.northwind',
    title: 'Add tagline to product',
    description: 'Surface tagline as a top-level searchable property.',
    status: 'pending',
    author: 'alice@test',
    createdAt: '2026-05-13T00:00:00Z',
    updatedAt: '2026-05-13T00:00:00Z',
    ...overrides,
  };
}

function failWith(errorName: string, reason: string) {
  return {
    status: 500,
    contentType: 'application/json',
    body: JSON.stringify({
      errorCode: 'INTERNAL',
      errorName,
      errorInstanceId: 'spec',
      parameters: { reason },
    }),
  };
}

async function stubEndpoints(page: Page, stubs: Stubs): Promise<void> {
  const PROPS_PREFIX = `**/api/v2/ontologies/${ONTOLOGY}/proposals`;
  const BRANCHES_PREFIX = `**/api/v2/ontologies/${ONTOLOGY}/branches`;

  // /branches/{id}/diff and /branches/{id}/breaking-changes go first
  // (most-specific routes register first so they win Playwright's LIFO
  // dispatch).
  await page.route(`${BRANCHES_PREFIX}/*/diff`, async (route: Route) => {
    const url = route.request().url();
    const m = url.match(/\/branches\/([^/?#]+)\/diff/);
    const branchId = m?.[1] ?? '';
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: stubs.diffsByBranch[branchId] ?? [] }),
    });
  });

  await page.route(
    `${BRANCHES_PREFIX}/*/breaking-changes`,
    async (route: Route) => {
      const url = route.request().url();
      const m = url.match(/\/branches\/([^/?#]+)\/breaking-changes/);
      const branchId = m?.[1] ?? '';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          branchId,
          changes: stubs.breakingByBranch[branchId] ?? [],
        }),
      });
    },
  );

  // /proposals/{id}/approve | /reject | /merge — register before the
  // catch-all so the dispatch matches the leaf URL first.
  await page.route(`${PROPS_PREFIX}/*/approve`, async (route: Route) => {
    const url = route.request().url();
    const m = url.match(/\/proposals\/([^/?#]+)\/approve/);
    const proposalId = m?.[1] ?? '';
    stubs.approves.push({
      url,
      method: route.request().method(),
      body: route.request().postDataJSON() ?? {},
    });
    const idx = stubs.proposals.findIndex((p) => p.id === proposalId);
    if (idx === -1) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'NOT_FOUND',
          errorName: 'ProposalNotFound',
          errorInstanceId: 'spec',
          parameters: { proposalId },
        }),
      });
      return;
    }
    const reviewBody = (route.request().postDataJSON() ?? {}) as {
      reviewer: string;
      reason?: string;
    };
    const review: MockReview = {
      id: `rev-${(stubs.reviews[proposalId]?.length ?? 0) + 1}`,
      proposalId,
      reviewer: reviewBody.reviewer,
      decision: 'approve',
      reason: reviewBody.reason,
      createdAt: '2026-05-13T01:00:00Z',
    };
    stubs.reviews[proposalId] = [
      ...(stubs.reviews[proposalId] ?? []),
      review,
    ];
    stubs.proposals[idx] = {
      ...stubs.proposals[idx],
      status: 'approved',
      updatedAt: '2026-05-13T01:00:00Z',
    };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(stubs.proposals[idx]),
    });
  });

  await page.route(`${PROPS_PREFIX}/*/reject`, async (route: Route) => {
    const url = route.request().url();
    const m = url.match(/\/proposals\/([^/?#]+)\/reject/);
    const proposalId = m?.[1] ?? '';
    stubs.rejects.push({
      url,
      method: route.request().method(),
      body: route.request().postDataJSON() ?? {},
    });
    const idx = stubs.proposals.findIndex((p) => p.id === proposalId);
    if (idx === -1) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'NOT_FOUND',
          errorName: 'ProposalNotFound',
          errorInstanceId: 'spec',
          parameters: { proposalId },
        }),
      });
      return;
    }
    const reviewBody = (route.request().postDataJSON() ?? {}) as {
      reviewer: string;
      reason?: string;
    };
    const review: MockReview = {
      id: `rev-${(stubs.reviews[proposalId]?.length ?? 0) + 1}`,
      proposalId,
      reviewer: reviewBody.reviewer,
      decision: 'reject',
      reason: reviewBody.reason,
      createdAt: '2026-05-13T01:00:00Z',
    };
    stubs.reviews[proposalId] = [
      ...(stubs.reviews[proposalId] ?? []),
      review,
    ];
    stubs.proposals[idx] = {
      ...stubs.proposals[idx],
      status: 'rejected',
      updatedAt: '2026-05-13T01:00:00Z',
    };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(stubs.proposals[idx]),
    });
  });

  await page.route(`${PROPS_PREFIX}/*/merge`, async (route: Route) => {
    const url = route.request().url();
    const m = url.match(/\/proposals\/([^/?#]+)\/merge/);
    const proposalId = m?.[1] ?? '';
    stubs.merges.push({
      url,
      method: route.request().method(),
      body: route.request().postDataJSON() ?? {},
    });
    if (stubs.failNextMergeWith) {
      const name = stubs.failNextMergeWith;
      stubs.failNextMergeWith = null;
      await route.fulfill(failWith(name, 'Synthetic merge failure for BDD'));
      return;
    }
    const idx = stubs.proposals.findIndex((p) => p.id === proposalId);
    if (idx === -1) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'NOT_FOUND',
          errorName: 'ProposalNotFound',
          errorInstanceId: 'spec',
          parameters: { proposalId },
        }),
      });
      return;
    }
    stubs.proposals[idx] = {
      ...stubs.proposals[idx],
      status: 'merged',
      updatedAt: '2026-05-13T02:00:00Z',
    };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(stubs.proposals[idx]),
    });
  });

  // GET /proposals/{id} (detail). Playwright glob `*` does not cross `/`,
  // so the list-level catch-all `${PROPS_PREFIX}*` does NOT match this
  // path — register a dedicated single-resource handler.
  await page.route(`${PROPS_PREFIX}/*`, async (route: Route) => {
    const req = route.request();
    const method = req.method();
    const url = req.url();
    const pathOnly = url.split('?')[0];
    const m = pathOnly.match(/\/proposals\/([^/?#]+)$/);
    const proposalId = m?.[1] ?? '';

    if (method !== 'GET') {
      await route.continue();
      return;
    }

    const proposal = stubs.proposals.find((p) => p.id === proposalId);
    if (!proposal) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'NOT_FOUND',
          errorName: 'ProposalNotFound',
          errorInstanceId: 'spec',
          parameters: { proposalId },
        }),
      });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ...proposal,
        reviews: stubs.reviews[proposalId] ?? [],
      }),
    });
  });

  // GET /proposals (list). The Playwright glob `*` only matches single
  // path segments (no `/`), so this matches `/proposals` and
  // `/proposals?status=pending` but NOT `/proposals/{id}`.
  await page.route(`${PROPS_PREFIX}*`, async (route: Route) => {
    const req = route.request();
    const url = req.url();
    const method = req.method();

    if (method !== 'GET') {
      await route.continue();
      return;
    }

    const u = new URL(url);
    const status = u.searchParams.get('status');
    const data = status
      ? stubs.proposals.filter((p) => p.status === status)
      : stubs.proposals;
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data }),
    });
  });
}

describeFeature('Ontology Proposals & Merge', () => {
  test('Scenario: list renders status badges and the filter chips narrow the rows @smoke', async ({
    page,
    request,
  }) => {
    // AC: "新建路由 /proposals/:ontology 列表页，按 open/approved/rejected/merged 过滤".
    // Seed three proposals in three different statuses so the chips can
    // narrow the list. The "Open" chip filters server-side to status=pending.
    const proposals: MockProposal[] = [
      proposalFixture({
        id: 'prop-pending',
        title: 'Pending proposal',
        status: 'pending',
        branchId: 'br-pending',
      }),
      proposalFixture({
        id: 'prop-approved',
        title: 'Approved proposal',
        status: 'approved',
        branchId: 'br-approved',
      }),
      proposalFixture({
        id: 'prop-merged',
        title: 'Merged proposal',
        status: 'merged',
        branchId: 'br-merged',
      }),
    ];
    const stubs = newStubs({ proposals });
    const proposalsPage = new ProposalsPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('three proposals exist in three statuses', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens /proposals/northwind', async () => {
      await proposalsPage.goto(ONTOLOGY);
      await expect(proposalsPage.root).toBeVisible();
    });

    await Then('all three proposals render with their status badges', async () => {
      await expect(proposalsPage.list).toBeVisible();
      await expect(proposalsPage.rowByProposalId('prop-pending')).toBeVisible();
      await expect(
        proposalsPage.rowByProposalId('prop-approved'),
      ).toBeVisible();
      await expect(proposalsPage.rowByProposalId('prop-merged')).toBeVisible();
      await expect(
        proposalsPage.rowStatusBadge('prop-pending'),
      ).toContainText('pending');
      await expect(
        proposalsPage.rowStatusBadge('prop-approved'),
      ).toContainText('approved');
      await expect(
        proposalsPage.rowStatusBadge('prop-merged'),
      ).toContainText('merged');
    });

    await When('the user clicks the "Open" filter chip', async () => {
      await proposalsPage.filterBtn('pending').click();
    });

    await Then('only the pending proposal remains in the list', async () => {
      await expect(proposalsPage.rowByProposalId('prop-pending')).toBeVisible();
      await expect(proposalsPage.rowByProposalId('prop-approved')).toHaveCount(
        0,
      );
      await expect(proposalsPage.rowByProposalId('prop-merged')).toHaveCount(0);
    });

    await When('the user clicks the "Merged" filter chip', async () => {
      await proposalsPage.filterBtn('merged').click();
    });

    await Then('only the merged proposal remains in the list', async () => {
      await expect(proposalsPage.rowByProposalId('prop-merged')).toBeVisible();
      await expect(proposalsPage.rowByProposalId('prop-pending')).toHaveCount(
        0,
      );
      await expect(proposalsPage.rowByProposalId('prop-approved')).toHaveCount(
        0,
      );
    });
  });

  test('Scenario: selecting a proposal opens the detail panel with diff sections colored by change type', async ({
    page,
  }) => {
    // AC: "详情页：Diff 视图 (add/modify/delete 分类着色) + Approve / Reject / Merge 按钮".
    const proposals: MockProposal[] = [
      proposalFixture({
        id: 'prop-1',
        title: 'Refactor shipping schema',
        branchId: 'br-1',
      }),
    ];
    const stubs = newStubs({
      proposals,
      diffsByBranch: {
        'br-1': [
          {
            entityType: 'objectType',
            entityRid: 'ri.ontology.main.objectType.shipment',
            changeType: 'ADDED',
            before: null,
            after: { apiName: 'shipment' },
          },
          {
            entityType: 'property',
            entityRid: 'ri.ontology.main.property.product.tagline',
            changeType: 'MODIFIED',
            before: { baseType: 'string', nullable: true },
            after: { baseType: 'string', nullable: false },
          },
          {
            entityType: 'linkType',
            entityRid: 'ri.ontology.main.linkType.legacy',
            changeType: 'DELETED',
            before: { apiName: 'legacy' },
            after: null,
          },
        ],
      },
    });
    const proposalsPage = new ProposalsPage(page);

    await Given('a proposal with a 3-entry diff exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the page', async () => {
      await proposalsPage.goto(ONTOLOGY);
      await expect(proposalsPage.root).toBeVisible();
    });

    await Then('the detail panel is initially empty', async () => {
      await expect(proposalsPage.detailEmpty).toBeVisible();
    });

    await When('the user clicks the proposal row', async () => {
      await proposalsPage.rowByProposalId('prop-1').click();
    });

    await Then('the detail panel renders the proposal title', async () => {
      await expect(proposalsPage.detail).toBeVisible();
      await expect(proposalsPage.detailTitle()).toContainText(
        'Refactor shipping schema',
      );
    });

    await Then('all three diff rows render with their change-type colors', async () => {
      await expect(proposalsPage.diffSection()).toBeVisible();
      await expect(proposalsPage.diffRows()).toHaveCount(3);
      // The summary pills count entries by change type.
      await expect(proposalsPage.diffSummary('ADDED')).toContainText('1');
      await expect(proposalsPage.diffSummary('MODIFIED')).toContainText('1');
      await expect(proposalsPage.diffSummary('DELETED')).toContainText('1');
      // Each row exposes its change type via data attribute so we can
      // assert the per-row coloring contract without coupling to the
      // exact Tailwind class names.
      await expect(
        page.locator(
          '[data-testid="proposals-diff-row"][data-change-type="ADDED"]',
        ),
      ).toHaveCount(1);
      await expect(
        page.locator(
          '[data-testid="proposals-diff-row"][data-change-type="MODIFIED"]',
        ),
      ).toHaveCount(1);
      await expect(
        page.locator(
          '[data-testid="proposals-diff-row"][data-change-type="DELETED"]',
        ),
      ).toHaveCount(1);
    });
  });

  test('Scenario: a branch carrying breaking changes surfaces the warning banner with the change list', async ({
    page,
  }) => {
    // AC: "冲突警告横幅 (接 breakingChanges API)". Seed two breaking
    // changes on the branch and assert the banner renders with the
    // count + each item's kind / property name.
    const proposals: MockProposal[] = [
      proposalFixture({
        id: 'prop-breaking',
        title: 'Tighten product schema',
        branchId: 'br-breaking',
      }),
    ];
    const stubs = newStubs({
      proposals,
      diffsByBranch: { 'br-breaking': [] },
      breakingByBranch: {
        'br-breaking': [
          {
            kind: 'PROPERTY_DELETED',
            objectTypeApiName: 'product',
            propertyApiName: 'legacy_sku',
            detail: 'still referenced by 2 saved object sets',
          },
          {
            kind: 'PROPERTY_REQUIRED_ADDED',
            objectTypeApiName: 'product',
            propertyApiName: 'tagline',
            detail: 'new NOT NULL column',
          },
        ],
      },
    });
    const proposalsPage = new ProposalsPage(page);

    await Given('the branch has 2 breaking changes', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the page and selects the proposal', async () => {
      await proposalsPage.goto(ONTOLOGY);
      await proposalsPage.rowByProposalId('prop-breaking').click();
      await expect(proposalsPage.detail).toBeVisible();
    });

    await Then('the breaking-changes banner renders with both items', async () => {
      await expect(proposalsPage.breakingBanner()).toBeVisible();
      await expect(proposalsPage.breakingBanner()).toHaveAttribute(
        'data-changes-count',
        '2',
      );
      await expect(proposalsPage.breakingItems()).toHaveCount(2);
      await expect(
        page.locator(
          '[data-testid="proposals-breaking-item"][data-kind="PROPERTY_DELETED"]',
        ),
      ).toContainText('legacy_sku');
      await expect(
        page.locator(
          '[data-testid="proposals-breaking-item"][data-kind="PROPERTY_REQUIRED_ADDED"]',
        ),
      ).toContainText('tagline');
    });

    await Then(
      'the clean-state banner is NOT rendered while breaking changes exist',
      async () => {
        await expect(proposalsPage.breakingClean()).toHaveCount(0);
      },
    );
  });

  test('Scenario: the Merge button is gated on approved status and the typed-confirm dialog must match the title', async ({
    page,
  }) => {
    // AC: "Merge 弹二次确认 (要求输入 proposal 名)". The Merge button is
    // disabled until status === approved. The dialog gates Confirm on
    // typing the title verbatim.
    const proposals: MockProposal[] = [
      proposalFixture({
        id: 'prop-pending',
        title: 'Pending must approve first',
        status: 'pending',
        branchId: 'br-pending',
      }),
      proposalFixture({
        id: 'prop-approved',
        title: 'Ready to merge',
        status: 'approved',
        branchId: 'br-approved',
      }),
    ];
    const stubs = newStubs({
      proposals,
      diffsByBranch: { 'br-approved': [], 'br-pending': [] },
    });
    const proposalsPage = new ProposalsPage(page);

    await Given('one pending and one approved proposal exist', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the pending proposal first', async () => {
      await proposalsPage.goto(ONTOLOGY);
      await proposalsPage.rowByProposalId('prop-pending').click();
      await expect(proposalsPage.detail).toBeVisible();
    });

    await Then('the Merge button is disabled', async () => {
      await expect(proposalsPage.mergeBtn()).toBeDisabled();
    });

    await When('the user switches to the approved proposal', async () => {
      await proposalsPage.rowByProposalId('prop-approved').click();
      await expect(proposalsPage.detailTitle()).toContainText('Ready to merge');
    });

    await Then('the Merge button is now enabled', async () => {
      await expect(proposalsPage.mergeBtn()).toBeEnabled();
    });

    await When('the user clicks Merge', async () => {
      await proposalsPage.mergeBtn().click();
      await expect(proposalsPage.mergeDialog).toBeVisible();
    });

    await Then(
      'the Confirm button is disabled until the title matches exactly',
      async () => {
        await expect(proposalsPage.mergeDialogConfirm()).toBeDisabled();
        await proposalsPage.mergeDialogInput().fill('ready to merge'); // wrong case
        await expect(proposalsPage.mergeDialogConfirm()).toBeDisabled();
        await proposalsPage.mergeDialogInput().fill('Ready to merge');
        await expect(proposalsPage.mergeDialogConfirm()).toBeEnabled();
      },
    );

    await When('the user confirms the merge', async () => {
      await proposalsPage.mergeDialogConfirm().click();
    });

    await Then('a POST to the merge endpoint is captured', async () => {
      await expect.poll(() => stubs.merges.length).toBeGreaterThanOrEqual(1);
      const last = stubs.merges.at(-1)!;
      expect(last.method).toBe('POST');
      expect(last.url).toMatch(/\/proposals\/prop-approved\/merge$/);
    });

    await Then('the dialog closes and the proposal flips to merged', async () => {
      await expect(proposalsPage.mergeDialog).toHaveCount(0);
      // After merge, the page navigates the detail panel back to empty,
      // and the list row's badge re-renders as `merged` via invalidation.
      await expect(
        proposalsPage.rowStatusBadge('prop-approved'),
      ).toContainText('merged');
    });
  });

  test('Scenario: approving a pending proposal POSTs the reviewer payload and records a review row', async ({
    page,
  }) => {
    // AC: "Approve 按钮调对应 endpoint" + records ProposalReview row.
    const proposals: MockProposal[] = [
      proposalFixture({
        id: 'prop-approve',
        title: 'Approve me',
        status: 'pending',
        branchId: 'br-approve',
      }),
    ];
    const stubs = newStubs({
      proposals,
      diffsByBranch: { 'br-approve': [] },
    });
    const proposalsPage = new ProposalsPage(page);

    await Given('one pending proposal exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens and selects the proposal', async () => {
      await proposalsPage.goto(ONTOLOGY);
      await proposalsPage.rowByProposalId('prop-approve').click();
      await expect(proposalsPage.detail).toBeVisible();
    });

    await When(
      'the user fills the reviewer + reason and clicks Approve',
      async () => {
        await proposalsPage.reviewerInput().fill('bob@test');
        await proposalsPage.reasonInput().fill('LGTM');
        await proposalsPage.approveBtn().click();
      },
    );

    await Then('a POST to the approve endpoint with the payload is captured', async () => {
      await expect
        .poll(() => stubs.approves.length)
        .toBeGreaterThanOrEqual(1);
      const last = stubs.approves.at(-1)!;
      expect(last.method).toBe('POST');
      expect(last.url).toMatch(/\/proposals\/prop-approve\/approve$/);
      expect(last.body).toMatchObject({ reviewer: 'bob@test', reason: 'LGTM' });
    });

    await Then(
      'the proposal status flips to approved and the review row appears',
      async () => {
        await expect(proposalsPage.detailStatusBadge()).toContainText(
          'approved',
        );
        await expect(proposalsPage.reviewRows()).toHaveCount(1);
        await expect(proposalsPage.reviewRows().first()).toContainText(
          'bob@test',
        );
      },
    );

    await Then(
      'the Approve / Reject buttons are now disabled (status is no longer pending)',
      async () => {
        await expect(proposalsPage.approveBtn()).toBeDisabled();
        await expect(proposalsPage.rejectBtn()).toBeDisabled();
      },
    );

    await Then('the Merge button is now enabled', async () => {
      await expect(proposalsPage.mergeBtn()).toBeEnabled();
    });
  });
});
