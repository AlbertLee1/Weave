import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  LineagePage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/lineage/:rid` — the column-level lineage upgrade
 * rendered by `src/components/lineage/LineagePage.tsx` (US-049,
 * PC-A13).
 *
 * Scenarios map to the US-049 acceptance criteria:
 *
 *   - "现有 Lineage 页升级为 reactflow 力导向/DAG 图" → render scenario
 *     stubs an upstream chain (grandparent → parent → root) and asserts
 *     three custom nodes mount + ReactFlow canvas is reachable + the
 *     counts header surfaces the merged node/edge counts.
 *   - "节点点击 → 显示 property / dataset / transform" → selection scenario
 *     clicks a non-root node and asserts the right-hand detail panel
 *     surfaces RID, resource type, in/out edge counts, and the per-edge
 *     transform breakdown with stable data attributes.
 *   - "上下游展开/折叠" → expand/collapse scenario clicks the per-node
 *     expand button to fetch additional lineage one hop further, asserts
 *     the merged graph grew, then clicks the same button (now labelled
 *     "collapse") and asserts the contributed nodes are removed.
 *   - Direction switch → flips direction=downstream and asserts the
 *     refetch carries the new query parameter.
 *
 * Every scenario stubs the backend through `page.route` so the page
 * renders deterministic fixtures without touching real PG.
 */

const ROOT_RID = 'ri.ontology.main.object-type.root';
const PARENT_RID = 'ri.ontology.main.object-type.parent';
const GRANDPARENT_RID = 'ri.ontology.main.object-type.grandparent';
const DATASET_RID = 'ri.dataset.main.dataset.upstream-payroll';

interface LineageNodeFixture {
  rid: string;
  type?: string;
}

interface LineageEdgeFixture {
  from: string;
  to: string;
  operation?: string;
  timestamp: string;
}

interface LineageResponseFixture {
  root: string;
  direction: string;
  depth: number;
  truncated: boolean;
  nodes: LineageNodeFixture[];
  edges: LineageEdgeFixture[];
}

interface CapturedRequest {
  url: string;
  rid: string;
  direction: string;
  depth: number;
}

interface Stubs {
  // responses keyed by the rid the SPA is requesting; falls back to a
  // single-node body when no override is registered.
  byRid: Record<string, LineageResponseFixture>;
  calls: CapturedRequest[];
  failNext: boolean;
}

function newStubs(): Stubs {
  return { byRid: {}, calls: [], failNext: false };
}

async function stubLineage(page: Page, stubs: Stubs): Promise<void> {
  await page.route('**/api/v2/objects/*/lineage*', async (route: Route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    const url = route.request().url();
    const m = url.match(/\/api\/v2\/objects\/([^/?#]+)\/lineage/);
    const rid = m ? decodeURIComponent(m[1]) : '';
    const parsed = new URL(url);
    const direction = parsed.searchParams.get('direction') ?? 'upstream';
    const depth = Number(parsed.searchParams.get('depth') ?? '1');
    stubs.calls.push({ url, rid, direction, depth });
    if (stubs.failNext) {
      stubs.failNext = false;
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'INTERNAL',
          errorName: 'LineageQueryFailed',
          errorInstanceId: 'spec',
          parameters: { message: 'boom' },
        }),
      });
      return;
    }
    const body = stubs.byRid[rid] ?? {
      root: rid,
      direction,
      depth,
      truncated: false,
      nodes: [{ rid }],
      edges: [],
    };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ...body, direction, depth }),
    });
  });
}

describeFeature('Lineage column-level DAG (US-049)', () => {
  test('Scenario: renders the upstream chain as a ReactFlow DAG @smoke', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    stubs.byRid[ROOT_RID] = {
      root: ROOT_RID,
      direction: 'upstream',
      depth: 1,
      truncated: false,
      nodes: [
        { rid: ROOT_RID, type: 'object-type' },
        { rid: PARENT_RID, type: 'object-type' },
      ],
      edges: [
        {
          from: PARENT_RID,
          to: ROOT_RID,
          operation: 'pipeline-run',
          timestamp: '2026-04-01T12:00:00Z',
        },
      ],
    };

    const lineage = new LineagePage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the backend reports one upstream parent', async () => {
      await stubLineage(page, stubs);
    });

    await When('the user opens the lineage view for the root rid', async () => {
      await lineage.goto(ROOT_RID);
      await expect(lineage.root).toBeVisible();
    });

    await Then('the header surfaces the root rid and merged counts', async () => {
      await expect(lineage.rootRidLabel).toHaveText(ROOT_RID);
      await expect(lineage.counts).toHaveAttribute('data-node-count', '2');
      await expect(lineage.counts).toHaveAttribute('data-edge-count', '1');
    });

    await Then('the ReactFlow canvas mounts with both nodes', async () => {
      await expect(lineage.graph).toBeVisible();
      await expect(lineage.graph).toHaveAttribute('data-direction', 'upstream');
      await expect(lineage.graph).toHaveAttribute('data-depth', '1');
      await expect(lineage.nodes()).toHaveCount(2);
      const root = lineage.nodeByRid(ROOT_RID);
      await expect(root).toBeVisible();
      await expect(root).toHaveAttribute('data-root', 'true');
      const parent = lineage.nodeByRid(PARENT_RID);
      await expect(parent).toBeVisible();
      await expect(parent).toHaveAttribute('data-root', 'false');
    });

    await When('the user switches direction to downstream', async () => {
      const before = stubs.calls.length;
      await lineage.directionSelect.selectOption('downstream');
      await expect.poll(() => stubs.calls.length).toBeGreaterThan(before);
    });

    await Then('the most recent request carries direction=downstream', async () => {
      const last = stubs.calls[stubs.calls.length - 1]!;
      expect(last.direction).toBe('downstream');
      expect(last.rid).toBe(ROOT_RID);
    });
  });

  test('Scenario: selecting a node opens the detail panel with property / dataset / transform context', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    stubs.byRid[ROOT_RID] = {
      root: ROOT_RID,
      direction: 'upstream',
      depth: 1,
      truncated: false,
      nodes: [
        { rid: ROOT_RID, type: 'object-type' },
        { rid: PARENT_RID, type: 'object-type' },
        { rid: DATASET_RID, type: 'dataset' },
      ],
      edges: [
        {
          from: PARENT_RID,
          to: ROOT_RID,
          operation: 'pipeline-run',
          timestamp: '2026-04-01T12:00:00Z',
        },
        {
          from: DATASET_RID,
          to: ROOT_RID,
          operation: 'ingest-csv',
          timestamp: '2026-03-15T08:00:00Z',
        },
      ],
    };

    const lineage = new LineagePage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the root has two upstream nodes with distinct operations', async () => {
      await stubLineage(page, stubs);
    });

    await When('the user opens the lineage view and clicks the dataset node', async () => {
      await lineage.goto(ROOT_RID);
      await expect(lineage.graph).toBeVisible();
      await expect(lineage.nodes()).toHaveCount(3);
      // Before clicking, the detail panel must not be mounted.
      await expect(lineage.detailPanel).toHaveCount(0);
      await lineage.nodeByRid(DATASET_RID).click();
    });

    await Then('the detail panel surfaces the dataset RID, resource type, and edge counts', async () => {
      await expect(lineage.detailPanel).toBeVisible();
      await expect(lineage.detailPanel).toHaveAttribute('data-rid', DATASET_RID);
      await expect(lineage.detailPanel).toHaveAttribute(
        'data-node-type',
        'dataset',
      );
      await expect(lineage.detailRid).toHaveText(DATASET_RID);
      await expect(lineage.detailType).toHaveText('dataset');
      // Dataset feeds into root via the ingest-csv edge → out=1, in=0.
      await expect(lineage.detailInCount).toHaveText('0');
      await expect(lineage.detailOutCount).toHaveText('1');
    });

    await Then('the per-edge transform breakdown is rendered', async () => {
      const rows = lineage.detailEdgeRows();
      await expect(rows).toHaveCount(1);
      const ingest = page.locator(
        '[data-testid="lineage-detail-edge"][data-edge-operation="ingest-csv"]',
      );
      await expect(ingest).toHaveCount(1);
      await expect(ingest).toHaveAttribute('data-edge-direction', 'out');
    });

    await When('the user dismisses the panel with the close affordance', async () => {
      await lineage.detailClose.click();
    });

    await Then('the detail panel unmounts and the graph keeps its node count', async () => {
      await expect(lineage.detailPanel).toHaveCount(0);
      await expect(lineage.nodes()).toHaveCount(3);
    });
  });

  test('Scenario: expand fetches another hop and collapse returns the graph to its previous size @smoke', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    stubs.byRid[ROOT_RID] = {
      root: ROOT_RID,
      direction: 'upstream',
      depth: 1,
      truncated: false,
      nodes: [
        { rid: ROOT_RID, type: 'object-type' },
        { rid: PARENT_RID, type: 'object-type' },
      ],
      edges: [
        {
          from: PARENT_RID,
          to: ROOT_RID,
          operation: 'pipeline-run',
          timestamp: '2026-04-01T12:00:00Z',
        },
      ],
    };
    stubs.byRid[PARENT_RID] = {
      root: PARENT_RID,
      direction: 'upstream',
      depth: 1,
      truncated: false,
      nodes: [
        { rid: PARENT_RID, type: 'object-type' },
        { rid: GRANDPARENT_RID, type: 'object-type' },
      ],
      edges: [
        {
          from: GRANDPARENT_RID,
          to: PARENT_RID,
          operation: 'transform-aggregate',
          timestamp: '2026-03-01T12:00:00Z',
        },
      ],
    };

    const lineage = new LineagePage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given(
      'the root has one upstream parent that in turn has its own upstream',
      async () => {
        await stubLineage(page, stubs);
      },
    );

    await When('the user expands the parent node', async () => {
      await lineage.goto(ROOT_RID);
      await expect(lineage.graph).toBeVisible();
      await expect(lineage.nodes()).toHaveCount(2);
      const before = stubs.calls.length;
      await lineage.expandButtonForRid(PARENT_RID).click();
      await expect
        .poll(() => stubs.calls.filter((c) => c.rid === PARENT_RID).length)
        .toBeGreaterThan(0);
      await expect(lineage.nodes()).toHaveCount(3);
      // sanity: the new request also asked the backend for that rid
      const newCalls = stubs.calls.slice(before);
      expect(newCalls.some((c) => c.rid === PARENT_RID)).toBe(true);
    });

    await Then('the grandparent node and its edge join the merged graph', async () => {
      await expect(lineage.nodeByRid(GRANDPARENT_RID)).toBeVisible();
      await expect(lineage.counts).toHaveAttribute('data-node-count', '3');
      await expect(lineage.counts).toHaveAttribute('data-edge-count', '2');
    });

    await Then('the expand button on the parent now reports the expanded state', async () => {
      await expect(lineage.expandButtonForRid(PARENT_RID)).toHaveAttribute(
        'data-expanded',
        'true',
      );
    });

    await When('the user clicks the same affordance to collapse', async () => {
      await lineage.expandButtonForRid(PARENT_RID).click();
    });

    await Then('the contributed grandparent node and its edge are removed', async () => {
      await expect(lineage.nodes()).toHaveCount(2);
      await expect(lineage.nodeByRid(GRANDPARENT_RID)).toHaveCount(0);
      await expect(lineage.counts).toHaveAttribute('data-node-count', '2');
      await expect(lineage.counts).toHaveAttribute('data-edge-count', '1');
      await expect(lineage.expandButtonForRid(PARENT_RID)).toHaveAttribute(
        'data-expanded',
        'false',
      );
    });
  });

  test('Scenario: truncated flag surfaces the badge for downstream-heavy roots', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    stubs.byRid[ROOT_RID] = {
      root: ROOT_RID,
      direction: 'upstream',
      depth: 1,
      truncated: true,
      nodes: [
        { rid: ROOT_RID, type: 'object-type' },
        { rid: PARENT_RID, type: 'object-type' },
      ],
      edges: [
        {
          from: PARENT_RID,
          to: ROOT_RID,
          operation: 'pipeline-run',
          timestamp: '2026-04-01T12:00:00Z',
        },
      ],
    };

    const lineage = new LineagePage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the backend reports a truncated traversal', async () => {
      await stubLineage(page, stubs);
    });

    await When('the user opens the lineage view', async () => {
      await lineage.goto(ROOT_RID);
      await expect(lineage.graph).toBeVisible();
    });

    await Then('a truncated badge is rendered next to the counts', async () => {
      await expect(lineage.truncated).toBeVisible();
      await expect(lineage.truncated).toContainText(/truncated/i);
    });
  });
});
