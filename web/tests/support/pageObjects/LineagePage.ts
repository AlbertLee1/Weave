import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/lineage/:rid` — the ReactFlow-based DAG view rendered
 * by `src/components/lineage/LineagePage.tsx` (US-049, PC-A13).
 *
 * Surfaces:
 *   - Header (root RID, node + edge counts, optional truncated badge).
 *   - Controls (direction select + depth input + clear-selection button).
 *   - Graph canvas: each custom node carries `data-testid="lineage-node"`
 *     + `data-rid`, plus an inline expand/collapse button on non-root
 *     nodes (`data-testid="lineage-node-expand-btn"`).
 *   - Detail panel (right side) appears when a node is selected; surfaces
 *     RID, resource type, incoming/outgoing counts, and the per-edge
 *     transform / dataset / property breakdown.
 */
export class LineagePage {
  readonly page: Page;
  readonly root: Locator;
  readonly rootRidLabel: Locator;
  readonly counts: Locator;
  readonly truncated: Locator;
  readonly directionSelect: Locator;
  readonly depthInput: Locator;
  readonly clearSelectionBtn: Locator;
  readonly graph: Locator;
  readonly loading: Locator;
  readonly errorState: Locator;
  readonly empty: Locator;
  readonly detailPanel: Locator;
  readonly detailClose: Locator;
  readonly detailRid: Locator;
  readonly detailType: Locator;
  readonly detailInCount: Locator;
  readonly detailOutCount: Locator;
  readonly detailInEdges: Locator;
  readonly detailOutEdges: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('lineage-page');
    this.rootRidLabel = page.getByTestId('lineage-root-rid');
    this.counts = page.getByTestId('lineage-counts');
    this.truncated = page.getByTestId('lineage-truncated');
    this.directionSelect = page.getByTestId('lineage-direction-select');
    this.depthInput = page.getByTestId('lineage-depth-input');
    this.clearSelectionBtn = page.getByTestId('lineage-clear-selection-btn');
    this.graph = page.getByTestId('lineage-graph');
    this.loading = page.getByTestId('lineage-loading');
    this.errorState = page.getByTestId('lineage-error');
    this.empty = page.getByTestId('lineage-empty');
    this.detailPanel = page.getByTestId('lineage-detail-panel');
    this.detailClose = page.getByTestId('lineage-detail-close-btn');
    this.detailRid = page.getByTestId('lineage-detail-rid');
    this.detailType = page.getByTestId('lineage-detail-type');
    this.detailInCount = page.getByTestId('lineage-detail-in-count');
    this.detailOutCount = page.getByTestId('lineage-detail-out-count');
    this.detailInEdges = page.getByTestId('lineage-detail-in-edges');
    this.detailOutEdges = page.getByTestId('lineage-detail-out-edges');
  }

  async goto(rid: string): Promise<void> {
    await this.page.goto(`/lineage/${encodeURIComponent(rid)}`);
    await this.page.waitForLoadState('domcontentloaded');
  }

  nodes(): Locator {
    return this.page.locator('[data-testid="lineage-node"]');
  }
  nodeByRid(rid: string): Locator {
    return this.page.locator(
      `[data-testid="lineage-node"][data-rid="${rid}"]`,
    );
  }
  expandButtonForRid(rid: string): Locator {
    return this.page.locator(
      `[data-testid="lineage-node-expand-btn"][data-rid="${rid}"]`,
    );
  }
  detailEdgeRows(): Locator {
    return this.page.locator('[data-testid="lineage-detail-edge"]');
  }
}
