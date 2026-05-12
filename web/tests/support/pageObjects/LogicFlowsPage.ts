import { type Locator, type Page } from '@playwright/test';

/**
 * Page object for `/logic-flows` — the AIP Logic Flows visual editor rendered
 * by `src/components/aiplogic/LogicFlowsPage.tsx`.
 *
 * Locator surface follows the page's three columns (FlowList sidebar, FlowEditor
 * canvas, NodeConfigPanel) plus the toolbar / execute / dry-run subsections.
 * New scenarios should add locators on demand rather than fattening this
 * surface preemptively (same convention as ActionHistoryPage / ThreadsPage).
 */
export class LogicFlowsPage {
  readonly page: Page;
  readonly root: Locator;

  readonly list: Locator;
  readonly listLoading: Locator;
  readonly listError: Locator;
  readonly listEmpty: Locator;
  readonly listItems: Locator;
  readonly newFlowBtn: Locator;

  readonly newFlowName: Locator;
  readonly newFlowId: Locator;
  readonly newFlowDescription: Locator;
  readonly newFlowSubmit: Locator;
  readonly modalOverlay: Locator;

  readonly editor: Locator;
  readonly editorLoading: Locator;
  readonly editorEmpty: Locator;
  readonly canvas: Locator;
  readonly canvasNodes: Locator;
  readonly canvasEdges: Locator;

  readonly addNodeLlm: Locator;
  readonly addNodeTool: Locator;
  readonly addNodeIf: Locator;
  readonly addNodeIterate: Locator;
  readonly addNodeOutput: Locator;
  readonly saveFlowBtn: Locator;
  readonly saveError: Locator;
  readonly toggleExecutePanel: Locator;

  readonly validationBanner: Locator;
  readonly validationIssues: Locator;

  readonly executePanel: Locator;
  readonly executeInput: Locator;
  readonly executeFlowBtn: Locator;
  readonly executeError: Locator;
  readonly runPanel: Locator;
  readonly runOutput: Locator;
  readonly runTraceItems: Locator;
  readonly runsHistory: Locator;

  readonly nodeConfigPanel: Locator;
  readonly nodeIdInput: Locator;
  readonly nodeTypeSelect: Locator;
  readonly nodeConfigClose: Locator;
  readonly nodeConfigDelete: Locator;
  readonly nodeIssues: Locator;
  readonly nodeIssueItems: Locator;

  readonly dryRunPanel: Locator;
  readonly dryRunState: Locator;
  readonly dryRunBtn: Locator;
  readonly dryRunError: Locator;
  readonly dryRunResult: Locator;
  readonly dryRunOutput: Locator;

  constructor(page: Page) {
    this.page = page;
    this.root = page.getByTestId('logic-flows-page');

    this.list = page.getByTestId('logic-flow-list');
    this.listLoading = page.getByTestId('logic-flow-list-loading');
    this.listError = page.getByTestId('logic-flow-list-error');
    this.listEmpty = page.getByTestId('logic-flow-list-empty');
    this.listItems = page.getByTestId('flow-list-item');
    this.newFlowBtn = page.getByTestId('new-flow-btn');

    this.newFlowName = page.getByTestId('new-flow-name');
    this.newFlowId = page.getByTestId('new-flow-id');
    this.newFlowDescription = page.getByTestId('new-flow-description');
    this.newFlowSubmit = page.getByTestId('new-flow-submit');
    this.modalOverlay = page.getByTestId('modal-overlay');

    this.editor = page.getByTestId('logic-flow-editor');
    this.editorLoading = page.getByTestId('logic-flow-editor-loading');
    this.editorEmpty = page.getByTestId('logic-flow-editor-empty');
    this.canvas = page.getByTestId('logic-flow-canvas');
    this.canvasNodes = page.locator('.react-flow__node');
    this.canvasEdges = page.locator('.react-flow__edge');

    this.addNodeLlm = page.getByTestId('add-node-llm');
    this.addNodeTool = page.getByTestId('add-node-tool');
    this.addNodeIf = page.getByTestId('add-node-if');
    this.addNodeIterate = page.getByTestId('add-node-iterate');
    this.addNodeOutput = page.getByTestId('add-node-output');
    this.saveFlowBtn = page.getByTestId('save-flow-btn');
    this.saveError = page.getByTestId('save-error');
    this.toggleExecutePanel = page.getByTestId('toggle-execute-panel');

    this.validationBanner = page.getByTestId('validation-banner');
    this.validationIssues = page.getByTestId('validation-issue');

    this.executePanel = page.getByTestId('execute-panel');
    this.executeInput = page.getByTestId('execute-input');
    this.executeFlowBtn = page.getByTestId('execute-flow-btn');
    this.executeError = page.getByTestId('execute-error');
    this.runPanel = page.getByTestId('run-panel');
    this.runOutput = page.getByTestId('run-output');
    this.runTraceItems = page.getByTestId('run-trace-item');
    this.runsHistory = page.getByTestId('runs-history');

    this.nodeConfigPanel = page.getByTestId('node-config-panel');
    this.nodeIdInput = page.getByTestId('node-id-input');
    this.nodeTypeSelect = page.getByTestId('node-type-select');
    this.nodeConfigClose = page.getByTestId('node-config-close');
    this.nodeConfigDelete = page.getByTestId('node-config-delete');
    this.nodeIssues = page.getByTestId('node-issue-list');
    this.nodeIssueItems = page.getByTestId('node-issue');

    this.dryRunPanel = page.getByTestId('dry-run-panel');
    this.dryRunState = page.getByTestId('dry-run-state');
    this.dryRunBtn = page.getByTestId('dry-run-btn');
    this.dryRunError = page.getByTestId('dry-run-error');
    this.dryRunResult = page.getByTestId('dry-run-result');
    this.dryRunOutput = page.getByTestId('dry-run-output');
  }

  async goto(): Promise<void> {
    await this.page.goto('/logic-flows');
    await this.page.waitForLoadState('domcontentloaded');
  }

  flowListItem(flowId: string): Locator {
    return this.page.locator(
      `[data-testid="flow-list-item"][data-flow-id="${flowId}"]`,
    );
  }

  canvasNode(nodeId: string): Locator {
    return this.page.locator(`.react-flow__node[data-id="${nodeId}"]`);
  }

  traceItem(nodeId: string): Locator {
    return this.page.locator(
      `[data-testid="run-trace-item"][data-node-id="${nodeId}"]`,
    );
  }
}
