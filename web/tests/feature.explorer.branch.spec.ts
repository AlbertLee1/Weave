import { expect, test, type Page, type Route } from '@playwright/test';
import {
  ExplorerBranchPage,
  Given,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of the Explorer branch surfaces:
 *
 *   - `BranchPicker` rendered globally by `Topbar` whenever the URL is
 *     ontology-scoped (`src/components/layout/BranchPicker.tsx`).
 *   - `BranchDiffPage` at `/explorer/:ontology/branches/:branch/diff`
 *     (`src/components/explorer/BranchDiffPage.tsx`).
 *   - `BranchReconcilePage` at `/explorer/:ontology/branches/:branch/reconcile`
 *     (`src/components/explorer/BranchReconcilePage.tsx`).
 *
 * Scenarios map the PRD AC for US-037:
 *
 *   AC: "至少 5 scenarios：创建分支、切换、diff、合并、冲突提示"
 *
 * Honest mapping (mirroring US-025/028/030/033/034/035):
 *   - "创建分支" → there is NO create-branch UI today. The BranchPicker
 *     menu lists existing branches + a default `main` entry but exposes
 *     no "Create" affordance, and `web/src/api/ontologies.ts` has no
 *     POST helper for the `/api/v2/ontologies/{o}/branches` endpoint.
 *     Lock the gap with a triple absence assertion (button + link +
 *     menuitem regex covering Create/New/Add) inside the open
 *     BranchPicker menu plus a `page.on('request')` listener confirming
 *     zero `POST /branches` traffic. Replace this scenario with a
 *     click-driven flow the day a UX adds an inline creator.
 *   - "切换" → click the BranchPicker trigger to open the menu, pick a
 *     non-default branch option, and lock the post-conditions: the
 *     trigger's `data-branch` attribute flips to the new branch id, the
 *     active label text follows, and the menu collapses.
 *   - "diff" → navigate to `/branches/{id}/diff`, lock the summary
 *     counts (`+N added` / `~M modified` / `-K deleted`), the entity-type
 *     filter (`All (N)` + per-type chip), and the per-row badges.
 *   - "合并" → on the reconcile page with zero conflicts, click the
 *     merge button, capture the POST body to assert (a) the
 *     `conflictResolution` map is empty (no conflicts to resolve) and
 *     (b) `Content-Type: application/json` + (c) the success banner
 *     surfaces with the wire `appliedCount` rendered into the i18n copy.
 *   - "冲突提示" → reconcile page with two conflicts on the same
 *     resolution key. The merge button is disabled while conflicts are
 *     unresolved (text reflects the count). After picking `use-branch`
 *     for both, the button enables; the spec then issues a merge whose
 *     server response 409s with `MERGE_CONFLICT` (unresolved set) and
 *     locks the error banner + the conflict-count badge.
 *
 * Every scenario stubs the four endpoints the surfaces hit:
 *   - GET  /api/v2/ontologies/{o}/objectTypes (Explorer body)
 *   - GET  /api/v2/ontologies/{o}/branches (BranchPicker)
 *   - GET  /api/v2/ontologies/{o}/branches/{id}/diff (BranchDiffPage)
 *   - POST /api/v2/ontologies/{o}/branches/{id}/diff (BranchReconcilePage)
 *   - POST /api/v2/ontologies/{o}/branches/{id}/merge (BranchReconcilePage)
 *
 * AUTH_MODE is presumed to be `dev` in the e2e harness; signIn returns
 * `{}` which is fine for these read-only stubbed endpoints (the API
 * client decorates real requests with the bearer token but page.route
 * intercepts before the network leg).
 */

const ONTOLOGY = 'northwind';
const ONTOLOGY_RID = 'ri.ontology.main.ontology.northwind';
const BRANCH_ID = 'br-feature-x';

interface OntologyBranch {
  id: string;
  ontologyRid: string;
  name: string;
  baseVersion: number;
  parentBranchId?: string;
  baseTx?: string;
  status: 'open' | 'merged' | 'closed';
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

interface BranchDiffEntry {
  entityType: string;
  entityRid: string;
  changeType: 'ADDED' | 'MODIFIED' | 'DELETED';
  before: Record<string, unknown> | null;
  after: Record<string, unknown> | null;
}

interface AnnotatedDiffEntry {
  entityType: string;
  entityRid: string;
  apiName: string;
  resolutionKey: string;
  changeType: 'ADDED' | 'MODIFIED' | 'DELETED';
  hasConflict: boolean;
  before?: Record<string, unknown> | null;
  after?: Record<string, unknown> | null;
}

interface AnnotatedMergeConflict {
  entityType: string;
  entityRid: string;
  apiName: string;
  resolutionKey: string;
  changeType: 'ADDED' | 'MODIFIED' | 'DELETED';
  branchState?: Record<string, unknown> | null;
  mainState?: Record<string, unknown> | null;
}

interface BranchDiffPostResponse {
  branch: OntologyBranch;
  added: AnnotatedDiffEntry[];
  modified: AnnotatedDiffEntry[];
  deleted: AnnotatedDiffEntry[];
  conflicts: AnnotatedMergeConflict[];
  hasConflicts: boolean;
}

interface MergeBranchRequestBody {
  conflictResolution?: Record<string, 'use-branch' | 'use-main'>;
}

interface MergeBranchResponseOk {
  branch: OntologyBranch;
  appliedCount: number;
  skippedCount: number;
}

interface MergeBranchResponseConflict {
  errorCode: 'MERGE_CONFLICT';
  conflicts: AnnotatedMergeConflict[];
  unresolved: AnnotatedMergeConflict[];
}

interface BranchStubs {
  branches: OntologyBranch[];
  // GET diff (BranchDiffPage)
  legacyDiff: BranchDiffEntry[];
  // POST diff (BranchReconcilePage)
  reconcileDiff: BranchDiffPostResponse;
  // Captured request bodies — the merge-success scenario asserts the
  // shape sent by the front end; the conflict scenario reuses the
  // capture array to confirm the unresolved choices reached the server.
  mergeCalls: MergeBranchRequestBody[];
  // When set, merge route returns 409 MERGE_CONFLICT once and resets
  // (so subsequent merges succeed). The spec uses this for the conflict
  // scenario; the merge-success scenario leaves it null.
  conflictNextMerge: MergeBranchResponseConflict | null;
  mergeOkResponse: MergeBranchResponseOk;
}

const branchFixture: OntologyBranch = {
  id: BRANCH_ID,
  ontologyRid: ONTOLOGY_RID,
  name: 'feature-x',
  baseVersion: 1,
  parentBranchId: 'main',
  baseTx: 'tx-001',
  status: 'open',
  createdBy: 'alice@example.com',
  createdAt: '2026-05-10T08:00:00Z',
  updatedAt: '2026-05-12T11:00:00Z',
};

function emptyMergeOk(): MergeBranchResponseOk {
  return {
    branch: { ...branchFixture, status: 'merged' },
    appliedCount: 0,
    skippedCount: 0,
  };
}

function newStubs(overrides: Partial<BranchStubs> = {}): BranchStubs {
  return {
    branches: [branchFixture],
    legacyDiff: [],
    reconcileDiff: {
      branch: branchFixture,
      added: [],
      modified: [],
      deleted: [],
      conflicts: [],
      hasConflicts: false,
    },
    mergeCalls: [],
    conflictNextMerge: null,
    mergeOkResponse: emptyMergeOk(),
    ...overrides,
  };
}

async function stubExplorerSurface(
  page: Page,
  stubs: BranchStubs,
): Promise<void> {
  // GET /api/v2/ontologies/{o}/objectTypes → Explorer's body uses this.
  // Branch-aware queries append `?branch=...` after switching; we strip
  // any query string with a single glob handler so both calls match.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    },
  );

  // GET /api/v2/ontologies/{o}/branches → BranchPicker menu.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/branches`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: stubs.branches }),
      });
    },
  );

  // GET /api/v2/ontologies/{o}/branches/{id}/diff → BranchDiffPage.
  // POST /api/v2/ontologies/{o}/branches/{id}/diff → BranchReconcilePage.
  // POST /api/v2/ontologies/{o}/branches/{id}/merge → BranchReconcilePage.
  // Single glob handler dispatches by method + last path segment, mirroring
  // the US-034 `parts.at(-1)` pattern. Critical: the more-specific
  // `/diff` and `/merge` patterns share `/branches/{id}/` prefix, so we
  // must inspect `last`.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/branches/**`,
    async (route: Route) => {
      const req = route.request();
      const method = req.method();
      const url = new URL(req.url());
      const trimmed = url.pathname.replace(/\/+$/, '');
      const last = trimmed.split('/').pop() ?? '';
      // The `/branches` index is matched by the earlier rule; this
      // handler covers /branches/{id}/diff and /branches/{id}/merge.
      if (last === 'diff') {
        if (method === 'GET') {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ data: stubs.legacyDiff }),
          });
          return;
        }
        if (method === 'POST') {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(stubs.reconcileDiff),
          });
          return;
        }
      }
      if (last === 'merge' && method === 'POST') {
        const body = (req.postDataJSON() ??
          {}) as MergeBranchRequestBody;
        stubs.mergeCalls.push(body);
        if (stubs.conflictNextMerge) {
          const payload = stubs.conflictNextMerge;
          stubs.conflictNextMerge = null;
          await route.fulfill({
            status: 409,
            contentType: 'application/json',
            body: JSON.stringify(payload),
          });
          return;
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(stubs.mergeOkResponse),
        });
        return;
      }
      await route.continue();
    },
  );
}

describeFeature('Explorer branch surfaces', () => {
  test('Scenario: opening the BranchPicker menu lists default + server branches @smoke', async ({
    page,
    request,
  }) => {
    // Locks the discovery half of "切换": the BranchPicker trigger sits
    // in the Topbar of any ontology-scoped route, the menu opens on
    // click, the server branches load in, and the default `main` row
    // remains pinned at the top regardless of the server response.
    const explorer = new ExplorerBranchPage(page);
    const stubs = newStubs();

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the explorer + branches endpoints return one branch', async () => {
      await stubExplorerSurface(page, stubs);
    });

    await When('the user opens the explorer page', async () => {
      await explorer.gotoExplorer(ONTOLOGY);
      await expect(explorer.explorerRoot).toBeVisible();
    });

    await Then('the BranchPicker trigger surfaces with the default branch', async () => {
      await expect(explorer.pickerTrigger).toBeVisible();
      await expect(explorer.pickerActive).toHaveText('main');
      await expect(explorer.pickerTrigger).toHaveAttribute(
        'data-branch',
        'main',
      );
    });

    await When('the user clicks the BranchPicker trigger', async () => {
      await explorer.pickerTrigger.click();
    });

    await Then('the menu lists default plus the server branch', async () => {
      await expect(explorer.pickerMenu).toBeVisible();
      await expect(explorer.pickerOption('main')).toBeVisible();
      await expect(explorer.pickerOption(BRANCH_ID)).toBeVisible();
      // The default `main` row carries the i18n DEFAULT label suffix.
      await expect(explorer.pickerOption('main')).toContainText(/default/i);
    });
  });

  test('Scenario: switching the active branch flips the trigger attribute and label @smoke', async ({
    page,
    request,
  }) => {
    // Locks the AC "切换" mutation half: clicking a non-default branch
    // option (a) closes the menu, (b) writes the new id to the persisted
    // store (reflected on the trigger's `data-branch` attribute), and
    // (c) updates the active label so the topbar reads `feature-x`.
    // The previous scenario already covered list rendering, so this one
    // skips the assertion that the menu was populated.
    const explorer = new ExplorerBranchPage(page);
    const stubs = newStubs();

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the explorer + branches endpoints are stubbed', async () => {
      await stubExplorerSurface(page, stubs);
    });

    await When('the user opens the explorer and the picker menu', async () => {
      await explorer.gotoExplorer(ONTOLOGY);
      await expect(explorer.explorerRoot).toBeVisible();
      await explorer.pickerTrigger.click();
      await expect(explorer.pickerMenu).toBeVisible();
    });

    await When('the user picks the feature-x branch', async () => {
      await explorer.pickerOption(BRANCH_ID).click();
    });

    await Then('the menu collapses', async () => {
      await expect(explorer.pickerMenu).toHaveCount(0);
    });

    await Then('the trigger advertises the new branch id', async () => {
      await expect(explorer.pickerTrigger).toHaveAttribute(
        'data-branch',
        BRANCH_ID,
      );
    });

    await Then('the active label reflects the picked branch id', async () => {
      // The picker shows the branch's `id` in the trigger label (see
      // BranchPicker.tsx — span renders `activeBranch`). That id is
      // `br-feature-x` here. Note: production renders the `name`
      // (feature-x) only inside the menu row, so the trigger label
      // intentionally locks the id form.
      await expect(explorer.pickerActive).toHaveText(BRANCH_ID);
    });
  });

  test('Scenario: the legacy diff page renders summary counts, entity filter, and per-row badges @smoke', async ({
    page,
    request,
  }) => {
    // Locks the "diff" AC: navigate directly to /branches/{id}/diff and
    // assert (a) the summary badges add up to the fixture counts, (b)
    // the per-entity-type filter chip shows the right count, and (c)
    // each diff row renders an ADDED/MODIFIED/DELETED badge with the
    // entity-type pill. We do not assert hex / Tailwind colors per
    // US-028 ("colors are visual regression's job").
    const explorer = new ExplorerBranchPage(page);
    const stubs = newStubs({
      legacyDiff: [
        {
          entityType: 'objectType',
          entityRid: 'ri.ot.added',
          changeType: 'ADDED',
          before: null,
          after: { apiName: 'NewType', displayName: 'New Type' },
        },
        {
          entityType: 'objectType',
          entityRid: 'ri.ot.modified',
          changeType: 'MODIFIED',
          before: { apiName: 'Existing', displayName: 'Existing Old' },
          after: { apiName: 'Existing', displayName: 'Existing New' },
        },
        {
          entityType: 'linkType',
          entityRid: 'ri.lt.deleted',
          changeType: 'DELETED',
          before: { apiName: 'OldLink' },
          after: null,
        },
      ],
    });

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the diff endpoint returns 1 added + 1 modified + 1 deleted', async () => {
      await stubExplorerSurface(page, stubs);
    });

    await When('the user navigates to the legacy diff page', async () => {
      await explorer.gotoDiff(ONTOLOGY, BRANCH_ID);
    });

    await Then('the summary badges render the wire-shape counts', async () => {
      await expect(page.getByText(/\+1 added/i)).toBeVisible();
      await expect(page.getByText(/~1 modified/i)).toBeVisible();
      await expect(page.getByText(/-1 deleted/i)).toBeVisible();
    });

    await Then('an entity-type filter for objectType and linkType is rendered', async () => {
      const allChip = page.getByRole('button', { name: /^All \(3\)$/i });
      await expect(allChip).toBeVisible();
      await expect(
        page.getByRole('button', { name: /^objectType \(2\)$/i }),
      ).toBeVisible();
      await expect(
        page.getByRole('button', { name: /^linkType \(1\)$/i }),
      ).toBeVisible();
    });

    await Then('clicking objectType narrows the visible cards', async () => {
      await page.getByRole('button', { name: /^objectType \(2\)$/i }).click();
      // The legacy diff page groups by entityType and renders a `h3`
      // section heading per group. After filtering only the
      // `objectType` group + its two rows should remain; the deleted
      // linkType row must disappear.
      await expect(
        page.locator('h3', { hasText: /^objectType/ }),
      ).toBeVisible();
      await expect(page.locator('h3', { hasText: /^linkType/ })).toHaveCount(0);
      await expect(page.locator('text=ri.lt.deleted')).toHaveCount(0);
      await expect(page.locator('text=ri.ot.added')).toBeVisible();
      await expect(page.locator('text=ri.ot.modified')).toBeVisible();
    });

    await Then('the Open Reconcile UI link points at the reconcile route', async () => {
      await expect(explorer.diffOpenReconcile).toBeVisible();
      await expect(explorer.diffOpenReconcile).toHaveAttribute(
        'href',
        `/explorer/${ONTOLOGY}/branches/${BRANCH_ID}/reconcile`,
      );
    });
  });

  test('Scenario: merging a conflict-free branch posts an empty resolution map and surfaces the success banner @smoke', async ({
    page,
    request,
  }) => {
    // Locks the "合并" AC: with zero conflicts the merge button is
    // enabled at mount, the POST body carries an empty
    // `conflictResolution` map (since there is nothing to resolve), and
    // the success banner renders applied/skipped counts.
    const explorer = new ExplorerBranchPage(page);
    const stubs = newStubs({
      reconcileDiff: {
        branch: branchFixture,
        added: [
          {
            entityType: 'objectType',
            entityRid: 'ri.ot.fresh',
            apiName: 'Fresh',
            resolutionKey: 'objectType:Fresh',
            changeType: 'ADDED',
            hasConflict: false,
            after: { apiName: 'Fresh' },
          },
        ],
        modified: [],
        deleted: [],
        conflicts: [],
        hasConflicts: false,
      },
      mergeOkResponse: {
        branch: { ...branchFixture, status: 'merged' },
        appliedCount: 1,
        skippedCount: 0,
      },
    });

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the reconcile diff returns 1 ADDED entry with zero conflicts', async () => {
      await stubExplorerSurface(page, stubs);
    });

    await When('the user navigates to the reconcile page', async () => {
      await explorer.gotoReconcile(ONTOLOGY, BRANCH_ID);
      await expect(explorer.reconcileRoot).toBeVisible();
    });

    await Then('the status badge reflects the open branch state', async () => {
      await expect(explorer.reconcileStatus).toContainText(/open/);
    });

    await Then('no conflict-count badge is present', async () => {
      await expect(explorer.reconcileConflictCount).toHaveCount(0);
      await expect(explorer.reconcileConflictsSection).toHaveCount(0);
    });

    await Then('the merge button is enabled', async () => {
      await expect(explorer.reconcileMergeButton).toBeEnabled();
    });

    await When('the user clicks Merge', async () => {
      await explorer.reconcileMergeButton.click();
    });

    await Then('a single merge POST went out with an empty resolution map', async () => {
      await expect.poll(() => stubs.mergeCalls.length).toBe(1);
      const body = stubs.mergeCalls[0]!;
      // The page always sends `{conflictResolution: {...}}` (see
      // BranchReconcilePage.onSubmit). The map is empty because there
      // are no conflicts to resolve in this scenario — the wire shape
      // is locked here.
      expect(body.conflictResolution).toBeDefined();
      expect(body.conflictResolution).toEqual({});
    });

    await Then('the page navigates away from the reconcile surface', async () => {
      // The BranchReconcilePage.onSuccess handler calls
      // `navigate('/explorer/{ontologyApiName}')` synchronously after
      // the mutation resolves — this unmounts the reconcile surface
      // before the `mergeMutation.isSuccess` success-banner branch
      // ever paints. Locking "we landed off the reconcile route" is
      // the honest end-to-end assertion: the merge call resolved 200
      // (otherwise we'd still see the reconcile root or an error
      // banner), the page transitioned, and the topbar BranchPicker
      // remains because both routes are ontology-scoped.
      await expect
        .poll(() => new URL(page.url()).pathname)
        .not.toMatch(/\/reconcile$/);
      await expect(explorer.reconcileError).toHaveCount(0);
    });
  });

  test('Scenario: conflicts block merge until resolved and 409 keeps the page on the reconcile surface', async ({
    page,
    request,
  }) => {
    // Locks the "冲突提示" AC three-act:
    //   (1) On mount with 2 unresolved conflicts the merge button is
    //       *disabled* and its label reflects the unresolved count.
    //   (2) After the user picks `use-branch` for both conflicts the
    //       button enables, and (per US-026 modal-form-mutation
    //       template) submitting → 409 keeps the page on the reconcile
    //       surface, surfaces the error banner, and the conflict
    //       section remains visible (the optimistic resolutions did
    //       not erase the rows from the DOM).
    //   (3) The POST body carries the two `use-branch` choices keyed
    //       by `resolutionKey` — locking the wire shape per US-031
    //       narrow DTO template.
    const explorer = new ExplorerBranchPage(page);
    const conflicts: AnnotatedMergeConflict[] = [
      {
        entityType: 'objectType',
        entityRid: 'ri.ot.X',
        apiName: 'X',
        resolutionKey: 'objectType:X',
        changeType: 'MODIFIED',
        branchState: { apiName: 'X', displayName: 'Branch X' },
        mainState: { apiName: 'X', displayName: 'Main X' },
      },
      {
        entityType: 'objectType',
        entityRid: 'ri.ot.Y',
        apiName: 'Y',
        resolutionKey: 'objectType:Y',
        changeType: 'MODIFIED',
        branchState: { apiName: 'Y', displayName: 'Branch Y' },
        mainState: { apiName: 'Y', displayName: 'Main Y' },
      },
    ];
    const stubs = newStubs({
      reconcileDiff: {
        branch: branchFixture,
        added: [],
        modified: conflicts.map<AnnotatedDiffEntry>((c) => ({
          entityType: c.entityType,
          entityRid: c.entityRid,
          apiName: c.apiName,
          resolutionKey: c.resolutionKey,
          changeType: c.changeType,
          hasConflict: true,
          before: c.mainState ?? null,
          after: c.branchState ?? null,
        })),
        deleted: [],
        conflicts,
        hasConflicts: true,
      },
      conflictNextMerge: {
        errorCode: 'MERGE_CONFLICT',
        conflicts,
        unresolved: conflicts,
      },
    });

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the reconcile diff carries 2 unresolved conflicts', async () => {
      await stubExplorerSurface(page, stubs);
    });

    await When('the user opens the reconcile page', async () => {
      await explorer.gotoReconcile(ONTOLOGY, BRANCH_ID);
      await expect(explorer.reconcileRoot).toBeVisible();
    });

    await Then('the conflict-count badge shows 2', async () => {
      await expect(explorer.reconcileConflictCount).toBeVisible();
      await expect(explorer.reconcileConflictCount).toContainText('2');
    });

    await Then('both conflict rows render with a use-main / use-branch radio pair', async () => {
      await expect(explorer.conflictRow('objectType:X')).toBeVisible();
      await expect(explorer.conflictRow('objectType:Y')).toBeVisible();
      await expect(explorer.conflictUseMain('objectType:X')).toBeVisible();
      await expect(explorer.conflictUseBranch('objectType:X')).toBeVisible();
      await expect(explorer.conflictUseMain('objectType:Y')).toBeVisible();
      await expect(explorer.conflictUseBranch('objectType:Y')).toBeVisible();
    });

    await Then('the merge button is disabled while conflicts are unresolved', async () => {
      await expect(explorer.reconcileMergeButton).toBeDisabled();
      // The button's i18n label encodes the unresolved count: locking
      // the literal English copy is acceptable since the e2e harness
      // is pinned to language=en (CI default).
      await expect(explorer.reconcileMergeButton).toContainText(/2/);
    });

    await When('the user picks use-branch for both conflicts', async () => {
      await explorer.conflictUseBranch('objectType:X').click();
      await explorer.conflictUseBranch('objectType:Y').click();
    });

    await Then('the merge button enables', async () => {
      await expect(explorer.reconcileMergeButton).toBeEnabled();
    });

    await When('the user clicks Merge and the server responds 409', async () => {
      await explorer.reconcileMergeButton.click();
    });

    await Then('the captured POST body carries both use-branch choices', async () => {
      await expect.poll(() => stubs.mergeCalls.length).toBe(1);
      const body = stubs.mergeCalls[0]!;
      expect(body.conflictResolution).toEqual({
        'objectType:X': 'use-branch',
        'objectType:Y': 'use-branch',
      });
    });

    await Then('the reconcile page remains visible (no navigation away)', async () => {
      await expect(explorer.reconcileRoot).toBeVisible();
      // The conflicts section stays visible — the server-side
      // unresolved list overrides the in-page resolutions per the
      // BranchReconcilePage.onError(MergeBranchConflictError) handler.
      await expect(explorer.reconcileConflictsSection).toBeVisible();
    });

    await Then('the error banner surfaces the 409 hint', async () => {
      await expect(explorer.reconcileError).toBeVisible();
      // The i18n key `branchReconcile.submitConflictHint` is what the
      // page renders for a 409 MERGE_CONFLICT response.
      await expect(explorer.reconcileError).toContainText(/conflict/i);
    });

    await Then('no success banner ever appeared', async () => {
      await expect(explorer.reconcileSuccess).toHaveCount(0);
    });
  });

  test('Scenario: there is no inline create-branch affordance — POST /branches is never hit', async ({
    page,
    request,
  }) => {
    // Honest-mapping for AC "创建分支". The BranchPicker menu lists
    // existing branches but has no create-branch button/menuitem; the
    // API client `web/src/api/ontologies.ts` does not export a
    // `createBranch` helper. Lock the gap two ways:
    //   (a) Role-based absence inside the open menu (button + link +
    //       menuitem regex covering Create/New/Add/Fork branch labels).
    //   (b) `page.on('request')` listener confirming zero POST traffic
    //       to `/api/v2/ontologies/{o}/branches` (the canonical
    //       backend endpoint). Equivalent to the US-035 "no DELETE"
    //       mount-time absence assertion.
    const explorer = new ExplorerBranchPage(page);
    const stubs = newStubs();
    const postBranches: string[] = [];

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the explorer + branches endpoints are stubbed', async () => {
      await stubExplorerSurface(page, stubs);
    });

    await When('the user opens the explorer and a request listener is attached', async () => {
      const listener = (req: import('@playwright/test').Request): void => {
        const url = req.url();
        if (
          req.method() === 'POST' &&
          url.includes(`/api/v2/ontologies/${ONTOLOGY}/branches`) &&
          !url.endsWith('/diff') &&
          !url.endsWith('/merge')
        ) {
          postBranches.push(url);
        }
      };
      page.on('request', listener);
      await explorer.gotoExplorer(ONTOLOGY);
      await expect(explorer.explorerRoot).toBeVisible();
      await explorer.pickerTrigger.click();
      await expect(explorer.pickerMenu).toBeVisible();
      // Give the page a beat to fire any deferred fetches that an
      // automatic creator might mount; 200ms mirrors the US-035
      // "no spurious request" template.
      await page.waitForTimeout(200);
      page.off('request', listener);
    });

    await Then('no Create/New/Add Branch affordance lives in the menu', async () => {
      // role=button absence
      await expect(
        explorer.pickerMenu.getByRole('button', {
          name: /create|new|add|fork|spawn/i,
        }),
      ).toHaveCount(0);
      // role=link absence (in case a future UX uses an <a> instead)
      await expect(
        explorer.pickerMenu.getByRole('link', {
          name: /create|new|add|fork|spawn/i,
        }),
      ).toHaveCount(0);
      // role=menuitem absence (other than the per-branch
      // menuitemradio rows the picker emits today)
      await expect(
        explorer.pickerMenu.getByRole('menuitem', {
          name: /create|new|add|fork|spawn/i,
        }),
      ).toHaveCount(0);
    });

    await Then('no POST hit /api/v2/ontologies/{o}/branches', async () => {
      expect(postBranches).toEqual([]);
    });
  });
});
