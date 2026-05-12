import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/pipelines` — the read-only pipeline browser rendered by
 * `src/components/pipelines/PipelinesPage.tsx`.
 *
 * The page is composed of three regions:
 *   - `PipelineList`        — sidebar with `pipeline-list-item` entries
 *   - `PipelineDetail`      — header with schedule / enabled badges + canvas
 *   - `PipelineLogPanel`    — right aside surfacing schedule / enabled / node
 *                             config (run history placeholder lives here)
 *
 * Adds-as-needed convention: new scenarios should extend the locator surface
 * inline rather than fattening this object preemptively (same pattern as
 * LogicFlowsPage / ThreadsPage / ActionHistoryPage).
 */
export class PipelinesPage {
  readonly page: Page;
  readonly root: Locator;

  readonly list: Locator;
  readonly listLoading: Locator;
  readonly listError: Locator;
  readonly listEmpty: Locator;
  readonly listItems: Locator;

  readonly detail: Locator;
  readonly detailLoading: Locator;
  readonly detailError: Locator;
  readonly detailEmpty: Locator;

  readonly graphCanvas: Locator;
  readonly graph: Locator;
  readonly graphNodes: Locator;
  readonly graphEdges: Locator;

  readonly logPanel: Locator;
  readonly logSchedule: Locator;
  readonly logEnabled: Locator;
  readonly logSelected: Locator;
  readonly logNoSelection: Locator;
  readonly logConfig: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('pipelines-page');

    this.list = page.getByTestId('pipeline-list');
    this.listLoading = page.getByTestId('pipeline-list-loading');
    this.listError = page.getByTestId('pipeline-list-error');
    this.listEmpty = page.getByTestId('pipeline-list-empty');
    this.listItems = page.getByTestId('pipeline-list-item');

    this.detail = page.getByTestId('pipeline-detail');
    this.detailLoading = page.getByTestId('pipeline-detail-loading');
    this.detailError = page.getByTestId('pipeline-detail-error');
    this.detailEmpty = page.getByTestId('pipeline-detail-empty');

    this.graphCanvas = page.getByTestId('pipeline-graph-canvas');
    this.graph = page.getByTestId('pipeline-graph');
    this.graphNodes = page.getByTestId('pipeline-graph-node');
    this.graphEdges = page.getByTestId('pipeline-graph-edge');

    this.logPanel = page.getByTestId('pipeline-log-panel');
    this.logSchedule = page.getByTestId('pipeline-log-schedule');
    this.logEnabled = page.getByTestId('pipeline-log-enabled');
    this.logSelected = page.getByTestId('pipeline-log-selected');
    this.logNoSelection = page.getByTestId('pipeline-log-no-selection');
    this.logConfig = page.getByTestId('pipeline-log-config');
  }

  async goto(): Promise<void> {
    await this.page.goto('/pipelines');
    await this.page.waitForLoadState('domcontentloaded');
  }

  pipelineListItem(pipelineId: string): Locator {
    return this.page.locator(
      `[data-testid="pipeline-list-item"][data-pipeline-id="${pipelineId}"]`,
    );
  }

  graphNode(nodeName: string): Locator {
    return this.page.locator(
      `[data-testid="pipeline-graph-node"][data-node-name="${nodeName}"]`,
    );
  }

  graphEdge(from: string, to: string): Locator {
    return this.page.locator(
      `[data-testid="pipeline-graph-edge"][data-from="${from}"][data-to="${to}"]`,
    );
  }
}
