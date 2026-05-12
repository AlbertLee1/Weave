import { expect, test, type Page, type Route } from '@playwright/test';
import {
  ApprovalsPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/approvals/:ontology` — the Approval Queue page rendered
 * by `src/components/approvals/ApprovalsPage.tsx`.
 *
 * Scenarios map the PRD AC for US-026 (frontend-backend-gap-coverage):
 *   AC mapping → scenario:
 *     发起 + 待审   → list-renders-pending-queue (a PENDING row surfaces
 *                    in the default PENDING tab — i.e. an action was
 *                    initiated and is awaiting review)
 *     批准         → approve-success-refreshes-list (open modal, submit
 *                    with reason → POST approve → PENDING tab refetches
 *                    to empty since the row terminated)
 *     驳回         → reject-success-refreshes-list (open Reject modal
 *                    with no reason → POST reject body is `{}`)
 *     超时         → stale-pending-row-surfaces (honest absence
 *                    assertion: the backend has no TIMED_OUT terminal
 *                    state today; stale rows continue to surface as
 *                    PENDING. Scenario locks the gap so a future PR
 *                    adding a timeout indicator must update the spec.)
 *     权限不足态   → forbidden-keeps-modal-open (POST approve → 403 →
 *                    modal stays open + error alert visible + refetch
 *                    did not run)
 * Plus two state-branch boundaries:
 *     列表过滤     → filter-status-APPROVED-pushes-query
 *     empty        → empty-state-when-no-rows
 *     error        → error-state-when-list-fails-500
 *
 * All scenarios stub the approvals endpoint through `page.route` (same
 * convention as US-022 action history) so the page renders deterministic
 * fixtures without touching real backend data.
 */

const ONTOLOGY = 'northwind';

type Status = 'PENDING' | 'APPROVED' | 'REJECTED';

interface MockApproval {
  id: string;
  ontologyApiName: string;
  actionType: string;
  actionTypeRid?: string;
  parameters?: unknown;
  approvers?: string[];
  status: Status;
  requestedBy?: string;
  reviewedBy?: string;
  reason?: string;
  createdAt: string;
  updatedAt: string;
}

const pending: MockApproval = {
  id: 'apr-001',
  ontologyApiName: ONTOLOGY,
  actionType: 'deleteEmployee',
  parameters: { employeeID: 'EMP-77', force: true },
  approvers: ['carol@example.com'],
  status: 'PENDING',
  requestedBy: 'alice@example.com',
  createdAt: '2026-05-12T08:00:00Z',
  updatedAt: '2026-05-12T08:00:00Z',
};

const stalePending: MockApproval = {
  // Same shape as `pending` but the request is 35 days old — used to
  // document the "no TIMED_OUT terminal state" gap (see scenario notes).
  id: 'apr-002',
  ontologyApiName: ONTOLOGY,
  actionType: 'archiveCustomer',
  parameters: { customerID: 'ANCIENT' },
  approvers: ['carol@example.com'],
  status: 'PENDING',
  requestedBy: 'alice@example.com',
  createdAt: '2026-04-08T10:00:00Z',
  updatedAt: '2026-04-08T10:00:00Z',
};

const approvedHistorical: MockApproval = {
  id: 'apr-100',
  ontologyApiName: ONTOLOGY,
  actionType: 'deleteEmployee',
  parameters: { employeeID: 'EMP-09' },
  approvers: ['carol@example.com'],
  status: 'APPROVED',
  requestedBy: 'alice@example.com',
  reviewedBy: 'carol@example.com',
  reason: 'LGTM',
  createdAt: '2026-05-01T12:00:00Z',
  updatedAt: '2026-05-01T12:30:00Z',
};

/**
 * Stubs GET /api/v2/ontologies/{ontology}/actions/approvals and lets the
 * caller decide which rows to return as a function of the query params.
 * Captured queries flow into `captured` so scenarios can assert that
 * React Query refetches carry the right `status` / `mine` params.
 *
 * GET-only so paired POST routes (approve/reject) on the same prefix
 * fall through cleanly when registered separately.
 */
function stubApprovals(
  page: Page,
  rowsFor: (query: URLSearchParams) => MockApproval[],
  captured: URLSearchParams[],
): Promise<void> {
  return page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actions/approvals*`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() !== 'GET') {
        await route.continue();
        return;
      }
      const url = new URL(req.url());
      // withActiveBranch may inject branch= when caller is on a non-default
      // branch; strip it so scenario assertions stay focused on filters.
      url.searchParams.delete('branch');
      // The endpoint suffix must end in /approvals (no trailing path), so
      // approve/reject sub-routes that match this glob via `*` fall back
      // to other handlers.
      if (!url.pathname.endsWith('/approvals')) {
        await route.continue();
        return;
      }
      captured.push(url.searchParams);
      const rows = rowsFor(url.searchParams);
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: rows }),
      });
    },
  );
}

describeFeature('Approvals (dual-sign) queue page', () => {
  test('Scenario: the PENDING tab surfaces the row awaiting review @smoke', async ({
    page,
    request,
  }) => {
    const approvals = new ApprovalsPage(page);
    const captured: URLSearchParams[] = [];

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('one action is queued for review on northwind', async () => {
      await stubApprovals(page, () => [pending], captured);
    });

    await When('the user opens /approvals/northwind', async () => {
      await approvals.goto(ONTOLOGY);
    });

    await Then('the page root is visible and loading has cleared', async () => {
      await expect(approvals.root).toBeVisible();
      await expect(approvals.loading).toBeHidden();
    });

    await Then('the queued row shows status, action type, and approve/reject affordances', async () => {
      await expect(approvals.list).toBeVisible();
      await expect(approvals.cards).toHaveCount(1);
      const card = approvals.cardByApprovalId(pending.id);
      await expect(card).toContainText('PENDING');
      await expect(card).toContainText('deleteEmployee');
      await expect(card).toContainText('alice@example.com');
      await expect(approvals.approveButton(pending.id)).toBeVisible();
      await expect(approvals.rejectButton(pending.id)).toBeVisible();
      await expect(approvals.parametersBlock(pending.id)).toContainText('EMP-77');
    });

    await Then('the initial GET carries status=PENDING and mine=true', async () => {
      await expect
        .poll(() => captured.length > 0 && captured[0].get('status'))
        .toBe('PENDING');
      expect(captured[0].get('mine')).toBe('true');
    });
  });

  test('Scenario: approving a row POSTs /approve with the reason and refreshes the queue @smoke', async ({
    page,
  }) => {
    const approvals = new ApprovalsPage(page);
    const captured: URLSearchParams[] = [];
    let approveCalls = 0;
    let approveBody: unknown = null;
    let approvedOnServer = false;

    await Given('the queue has one pending row and /approve is stubbed', async () => {
      await stubApprovals(
        page,
        () => (approvedOnServer ? [] : [pending]),
        captured,
      );
      await page.route(
        `**/api/v2/ontologies/${ONTOLOGY}/actions/approvals/${pending.id}/approve`,
        async (route) => {
          if (route.request().method() !== 'POST') {
            await route.continue();
            return;
          }
          approveCalls += 1;
          approveBody = route.request().postDataJSON();
          approvedOnServer = true;
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ approvalId: pending.id, status: 'APPROVED' }),
          });
        },
      );
    });

    await Given('the user is on the Approvals page', async () => {
      await approvals.goto(ONTOLOGY);
      await expect(approvals.list).toBeVisible();
    });

    await When('the user clicks Approve, fills "LGTM", and submits', async () => {
      await approvals.approveButton(pending.id).click();
      await expect(approvals.modalOverlay).toBeVisible();
      await approvals.reasonInput.fill('LGTM');
      await approvals.reviewSubmit.click();
    });

    await Then('/approve is invoked exactly once with the typed reason', async () => {
      await expect.poll(() => approveCalls).toBe(1);
      expect(approveBody).toEqual({ reason: 'LGTM' });
    });

    await Then('the modal closes and the PENDING tab is now empty', async () => {
      await expect(approvals.modalOverlay).toBeHidden();
      await expect(approvals.empty).toBeVisible();
      await expect(approvals.cards).toHaveCount(0);
    });
  });

  test('Scenario: rejecting a row with no reason POSTs an empty body and refreshes @smoke', async ({
    page,
  }) => {
    const approvals = new ApprovalsPage(page);
    const captured: URLSearchParams[] = [];
    let rejectCalls = 0;
    let rejectBody: unknown = null;
    let rejectedOnServer = false;

    await Given('the queue has one pending row and /reject is stubbed', async () => {
      await stubApprovals(
        page,
        () => (rejectedOnServer ? [] : [pending]),
        captured,
      );
      await page.route(
        `**/api/v2/ontologies/${ONTOLOGY}/actions/approvals/${pending.id}/reject`,
        async (route) => {
          if (route.request().method() !== 'POST') {
            await route.continue();
            return;
          }
          rejectCalls += 1;
          rejectBody = route.request().postDataJSON();
          rejectedOnServer = true;
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ approvalId: pending.id, status: 'REJECTED' }),
          });
        },
      );
    });

    await Given('the user is on the Approvals page', async () => {
      await approvals.goto(ONTOLOGY);
      await expect(approvals.list).toBeVisible();
    });

    await When('the user clicks Reject and submits without a reason', async () => {
      await approvals.rejectButton(pending.id).click();
      await expect(approvals.modalOverlay).toBeVisible();
      await approvals.reviewSubmit.click();
    });

    await Then('/reject is invoked exactly once with an empty body', async () => {
      await expect.poll(() => rejectCalls).toBe(1);
      // The api client sends `{}` (not `{reason: undefined}`) when reason
      // is blank — see web/src/api/approvals.ts:58/68.
      expect(rejectBody).toEqual({});
    });

    await Then('the modal closes and the PENDING tab is now empty', async () => {
      await expect(approvals.modalOverlay).toBeHidden();
      await expect(approvals.empty).toBeVisible();
    });
  });

  test('Scenario: switching to the Approved tab pushes status=APPROVED to the API', async ({
    page,
  }) => {
    const approvals = new ApprovalsPage(page);
    const captured: URLSearchParams[] = [];

    await Given('the approvals endpoint mirrors the status filter', async () => {
      await stubApprovals(
        page,
        (q) => {
          const s = q.get('status');
          if (s === 'APPROVED') return [approvedHistorical];
          if (s === 'REJECTED') return [];
          return [pending];
        },
        captured,
      );
    });

    await Given('the user is on the Approvals page', async () => {
      await approvals.goto(ONTOLOGY);
      await expect(approvals.list).toBeVisible();
    });

    await When('the user clicks the Approved status tab', async () => {
      await approvals.filterStatusApproved.click();
    });

    await Then('the most recent /approvals GET carried status=APPROVED', async () => {
      await expect
        .poll(() => captured.length > 0 && captured[captured.length - 1].get('status'))
        .toBe('APPROVED');
    });

    await Then('the historical approved row is shown with reviewer + reason', async () => {
      await expect(approvals.cards).toHaveCount(1);
      const card = approvals.cardByApprovalId(approvedHistorical.id);
      await expect(card).toContainText('APPROVED');
      await expect(card).toContainText('carol@example.com');
      await expect(card).toContainText('LGTM');
      // The terminal row does not offer Approve / Reject affordances.
      await expect(approvals.approveButton(approvedHistorical.id)).toHaveCount(0);
      await expect(approvals.rejectButton(approvedHistorical.id)).toHaveCount(0);
    });
  });

  test('Scenario: a stale PENDING row still surfaces in the queue (no TIMED_OUT state)', async ({
    page,
  }) => {
    // Honest mapping for AC "超时": the backend ActionApproval status
    // enum is PENDING|APPROVED|REJECTED today — there is no TIMED_OUT
    // terminal state nor a "stale" / "expired" badge on the card. So
    // rows older than any human-meaningful TTL keep surfacing as
    // PENDING until a reviewer acts on them. This scenario locks that
    // contract; a future PR adding a timeout indicator must update
    // either the absence assertion below or replace it with a positive
    // one. Same pattern as US-025 ("回滚" absence) + US-024
    // ("拖拽节点" → toolbar +LLM honest mapping).
    const approvals = new ApprovalsPage(page);
    const captured: URLSearchParams[] = [];

    await Given('a 35-day-old action is still PENDING approval', async () => {
      await stubApprovals(page, () => [stalePending], captured);
    });

    await When('the reviewer opens the Approvals page', async () => {
      await approvals.goto(ONTOLOGY);
    });

    await Then('the stale row is still listed under PENDING', async () => {
      await expect(approvals.list).toBeVisible();
      const card = approvals.cardByApprovalId(stalePending.id);
      await expect(card).toContainText('PENDING');
      await expect(card).toContainText('archiveCustomer');
      // Approve / Reject affordances remain available — the row is not
      // locked out by a timeout.
      await expect(approvals.approveButton(stalePending.id)).toBeVisible();
      await expect(approvals.rejectButton(stalePending.id)).toBeVisible();
    });

    await Then('no "stale" / "expired" / "timed out" badge is rendered', async () => {
      // Absence assertion — if a future PR introduces a TIMED_OUT badge
      // or stale-row warning, this expect will start matching and the
      // scenario will fail, forcing an update to either the page or
      // this spec.
      await expect(
        approvals.cardByApprovalId(stalePending.id).getByText(
          /timed?\s*out|expired|stale/i,
        ),
      ).toHaveCount(0);
    });
  });

  test('Scenario: a 403 from /approve keeps the modal open and surfaces the error', async ({
    page,
  }) => {
    const approvals = new ApprovalsPage(page);
    const captured: URLSearchParams[] = [];
    let approveCalls = 0;

    await Given('the queue has one pending row and /approve is stubbed to 403', async () => {
      await stubApprovals(page, () => [pending], captured);
      await page.route(
        `**/api/v2/ontologies/${ONTOLOGY}/actions/approvals/${pending.id}/approve`,
        async (route) => {
          if (route.request().method() !== 'POST') {
            await route.continue();
            return;
          }
          approveCalls += 1;
          await route.fulfill({
            status: 403,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'FORBIDDEN',
              errorName: 'Forbidden',
              errorInstanceId: 'spec',
              parameters: { error: 'caller is not an approver' },
              statusCode: 403,
            }),
          });
        },
      );
    });

    await Given('the user is on the Approvals page', async () => {
      await approvals.goto(ONTOLOGY);
      await expect(approvals.list).toBeVisible();
    });

    await When('the user opens the Approve modal and submits', async () => {
      await approvals.approveButton(pending.id).click();
      await expect(approvals.modalOverlay).toBeVisible();
      await approvals.reviewSubmit.click();
    });

    await Then('/approve was called exactly once', async () => {
      await expect.poll(() => approveCalls).toBe(1);
    });

    await Then('the modal stays open with a Forbidden error alert', async () => {
      await expect(approvals.modalOverlay).toBeVisible();
      await expect(approvals.reviewAlert).toBeVisible();
      await expect(approvals.reviewAlert).toContainText('Forbidden');
      await expect(approvals.reviewAlert).toContainText('caller is not an approver');
      // The row is still PENDING — it was not optimistically removed.
      await expect(approvals.cardByApprovalId(pending.id)).toBeVisible();
    });
  });

  test('Scenario: the Approvals page renders the empty state when no rows match', async ({
    page,
  }) => {
    const approvals = new ApprovalsPage(page);
    const captured: URLSearchParams[] = [];

    await Given('the approvals endpoint returns no rows', async () => {
      await stubApprovals(page, () => [], captured);
    });

    await When('the user opens the Approvals page', async () => {
      await approvals.goto(ONTOLOGY);
    });

    await Then('the empty-state panel is visible and the list is absent', async () => {
      await expect(approvals.empty).toBeVisible();
      await expect(approvals.list).toHaveCount(0);
      await expect(approvals.cards).toHaveCount(0);
    });
  });

  test('Scenario: the Approvals page renders the error state when /approvals fails with 500', async ({
    page,
  }) => {
    const approvals = new ApprovalsPage(page);

    await Given('the approvals endpoint returns 500', async () => {
      await page.route(
        `**/api/v2/ontologies/${ONTOLOGY}/actions/approvals*`,
        async (route) => {
          if (route.request().method() !== 'GET') {
            await route.continue();
            return;
          }
          const url = new URL(route.request().url());
          if (!url.pathname.endsWith('/approvals')) {
            await route.continue();
            return;
          }
          await route.fulfill({
            status: 500,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'INTERNAL',
              errorName: 'InternalError',
              errorInstanceId: 'spec',
              statusCode: 500,
            }),
          });
        },
      );
    });

    await When('the user opens the Approvals page', async () => {
      await approvals.goto(ONTOLOGY);
    });

    await Then('the error panel is visible and the list/empty are not rendered', async () => {
      await expect(approvals.error).toBeVisible();
      await expect(approvals.list).toHaveCount(0);
      await expect(approvals.empty).toHaveCount(0);
    });
  });
});
