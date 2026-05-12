import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  LogicFlowsPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/logic-flows` — the AIP Logic Flows visual editor rendered
 * by `src/components/aiplogic/LogicFlowsPage.tsx`.
 *
 * PRD AC for US-024 names 6 scenarios: 拖拽节点 / 连线 / 变量绑定 / 运行 /
 * 查看 trace / 异常分支. The page is a ReactFlow-backed canvas editor, so each
 * AC maps onto the page's actual capabilities:
 *   - 拖拽节点 → clicking the toolbar `+LLM` button to append a node, then
 *     saving (Scenario 2). ReactFlow drag-and-drop isn't exercised — the
 *     production "add" affordance is the toolbar button; we lock the PUT
 *     body shape via stub capture.
 *   - 连线  → preseeding a flow with 2 nodes + 1 edge and asserting ReactFlow
 *     renders the edge on the canvas (Scenario 3). User-driven handle drag
 *     is non-deterministic in headless Playwright; ReactFlow render
 *     correctness is what matters for users.
 *   - 变量绑定 → validation-banner highlights an unbound `{{...}}` ref
 *     (Scenario 4). Bound refs (input / iterate / sibling node id) stay
 *     silent — Scenario 1 implicitly covers the green path.
 *   - 运行 + 查看 trace → executing the flow with a fixture input, asserting
 *     run-panel renders the success status + run-output + per-step
 *     `run-trace-item`s (Scenario 5 — both AC bullets fold into one scenario
 *     because the trace is only visible after a run).
 *   - 异常分支 → executing returns a 200 with `status='failed'` + an error
 *     message + partial trace; run-panel surfaces the failed badge + error
 *     block (Scenario 6).
 *
 * Two boundary scenarios (Scenario 7 empty + Scenario 8 dry-run) lock the
 * remaining state branches of the page — same template as US-021/022/023.
 */

interface MockFlow {
  id: string;
  name: string;
  description?: string;
  nodes: Array<{ id: string; type: string; config?: Record<string, unknown> }>;
  edges: Array<{ from: string; to: string; branch?: string }>;
  fallbackModel?: string;
  maxRetries?: number;
  createdAt: string;
  updatedAt: string;
}

interface MockRun {
  id: number;
  flowId: string;
  status: 'success' | 'failed';
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  trace?: Array<{
    nodeId: string;
    type: string;
    status: 'success' | 'skipped' | 'failed';
    output?: Record<string, unknown>;
    error?: string;
  }>;
  error?: string;
  createdAt: string;
}

function singleOutputFlow(): MockFlow {
  return {
    id: 'flow_single_output',
    name: 'Single output flow',
    description: 'Just one terminal node.',
    nodes: [
      {
        id: 'output',
        type: 'output',
        config: { keys: [], __editorPosition: { x: 200, y: 120 } },
      },
    ],
    edges: [],
    createdAt: '2026-05-01T00:00:00Z',
    updatedAt: '2026-05-01T00:00:00Z',
  };
}

function twoNodeFlowWithEdge(): MockFlow {
  return {
    id: 'flow_with_edge',
    name: 'LLM → Output',
    nodes: [
      {
        id: 'greet',
        type: 'llm',
        config: {
          provider: 'mock',
          model: '',
          promptTemplate: 'Hello {{input.name}}.',
          __editorPosition: { x: 80, y: 80 },
        },
      },
      {
        id: 'output',
        type: 'output',
        config: { keys: ['greet'], __editorPosition: { x: 320, y: 80 } },
      },
    ],
    edges: [{ from: 'greet', to: 'output' }],
    createdAt: '2026-05-01T00:00:00Z',
    updatedAt: '2026-05-01T00:00:00Z',
  };
}

function unboundParamFlow(): MockFlow {
  return {
    id: 'flow_unbound_param',
    name: 'Unbound reference',
    nodes: [
      {
        id: 'greet',
        type: 'llm',
        config: {
          provider: 'mock',
          model: '',
          promptTemplate: 'Hello {{ghost.field}} and {{input.name}}.',
          __editorPosition: { x: 80, y: 80 },
        },
      },
      {
        id: 'output',
        type: 'output',
        config: { keys: ['greet'], __editorPosition: { x: 320, y: 80 } },
      },
    ],
    edges: [{ from: 'greet', to: 'output' }],
    createdAt: '2026-05-01T00:00:00Z',
    updatedAt: '2026-05-01T00:00:00Z',
  };
}

interface StubRefs {
  flows: () => MockFlow[];
  flow: (flowId: string) => MockFlow | undefined;
  runs?: (flowId: string) => MockRun[];
  flowsFail?: () => boolean;
  onUpdate?: (
    flowId: string,
    body: {
      nodes?: MockFlow['nodes'];
      edges?: MockFlow['edges'];
      fallbackModel?: string;
      maxRetries?: number;
    },
  ) => MockFlow;
  onCreate?: (body: {
    id?: string;
    name?: string;
    nodes: MockFlow['nodes'];
    edges: MockFlow['edges'];
  }) => MockFlow;
  onExecute?: (flowId: string, body: { input?: Record<string, unknown> }) => {
    status: number;
    body: MockRun;
  };
  onDryRun?: (
    flowId: string,
    body: {
      node: { id: string; type: string; config?: Record<string, unknown> };
      state?: Record<string, unknown>;
    },
  ) => { status: number; trace: NonNullable<MockRun['trace']>[number] };
}

/**
 * Wire up the four endpoints the page hits on load (list, detail, runs, +
 * mutation endpoints invoked on user actions). The refs callback pattern
 * lets mutation scenarios update the underlying fixtures so subsequent
 * React Query refetches see the new state, matching the US-023 threads
 * stub convention.
 *
 * Pattern note: register the narrower routes (`/runs`, `/execute`,
 * `/dry-run-node`) AFTER the parent `*` route so Playwright's LIFO
 * pattern resolution dispatches them correctly (see US-023 learnings).
 */
async function stubLogicFlowsApi(page: Page, refs: StubRefs): Promise<void> {
  await page.route('**/api/v2/aip/logic-flows', async (route: Route) => {
    const req = route.request();
    if (req.method() === 'GET') {
      if (refs.flowsFail?.() ?? false) {
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
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ flows: refs.flows() }),
      });
      return;
    }
    if (req.method() === 'POST') {
      const body = JSON.parse(req.postData() ?? '{}') as Parameters<
        NonNullable<StubRefs['onCreate']>
      >[0];
      if (refs.onCreate) {
        const created = refs.onCreate(body);
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(created),
        });
        return;
      }
    }
    await route.continue();
  });

  await page.route('**/api/v2/aip/logic-flows/*', async (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    const segments = url.pathname.split('/');
    const flowId = decodeURIComponent(segments[segments.length - 1]);

    if (req.method() === 'GET') {
      const f = refs.flow(flowId);
      if (!f) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'NotFound',
            statusCode: 404,
          }),
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(f),
      });
      return;
    }
    if (req.method() === 'PUT') {
      const body = JSON.parse(req.postData() ?? '{}') as Parameters<
        NonNullable<StubRefs['onUpdate']>
      >[1];
      if (refs.onUpdate) {
        const updated = refs.onUpdate(flowId, body);
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(updated),
        });
        return;
      }
    }
    if (req.method() === 'DELETE') {
      await route.fulfill({ status: 204, body: '' });
      return;
    }
    await route.continue();
  });

  await page.route('**/api/v2/aip/logic-flows/*/runs', async (route: Route) => {
    const req = route.request();
    if (req.method() !== 'GET') {
      await route.continue();
      return;
    }
    const url = new URL(req.url());
    const segments = url.pathname.split('/');
    const flowId = decodeURIComponent(segments[segments.length - 2]);
    const runs = refs.runs ? refs.runs(flowId) : [];
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ runs }),
    });
  });

  await page.route(
    '**/api/v2/aip/logic-flows/*/execute',
    async (route: Route) => {
      const req = route.request();
      if (req.method() !== 'POST') {
        await route.continue();
        return;
      }
      const url = new URL(req.url());
      const segments = url.pathname.split('/');
      const flowId = decodeURIComponent(segments[segments.length - 2]);
      const body = JSON.parse(req.postData() ?? '{}') as {
        input?: Record<string, unknown>;
      };
      if (refs.onExecute) {
        const { status, body: runBody } = refs.onExecute(flowId, body);
        await route.fulfill({
          status,
          contentType: 'application/json',
          body: JSON.stringify(runBody),
        });
        return;
      }
      await route.continue();
    },
  );

  await page.route(
    '**/api/v2/aip/logic-flows/*/dry-run-node',
    async (route: Route) => {
      const req = route.request();
      if (req.method() !== 'POST') {
        await route.continue();
        return;
      }
      const url = new URL(req.url());
      const segments = url.pathname.split('/');
      const flowId = decodeURIComponent(segments[segments.length - 2]);
      const body = JSON.parse(req.postData() ?? '{}') as Parameters<
        NonNullable<StubRefs['onDryRun']>
      >[1];
      if (refs.onDryRun) {
        const { status, trace } = refs.onDryRun(flowId, body);
        await route.fulfill({
          status,
          contentType: 'application/json',
          body: JSON.stringify({ trace }),
        });
        return;
      }
      await route.continue();
    },
  );
}

describeFeature('AIP Logic Flows page', () => {
  test('Scenario: opening /logic-flows renders the list with two flows and auto-selects the first @smoke', async ({
    page,
    request,
  }) => {
    const lf = new LogicFlowsPage(page);
    const flowA = singleOutputFlow();
    const flowB: MockFlow = {
      ...singleOutputFlow(),
      id: 'flow_b',
      name: 'Second flow',
    };
    const flowList = [flowA, flowB];

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the AIP logic-flows endpoint advertises two flows', async () => {
      await stubLogicFlowsApi(page, {
        flows: () => flowList,
        flow: (id) => flowList.find((f) => f.id === id),
      });
    });

    await When('the user opens /logic-flows', async () => {
      await lf.goto();
    });

    await Then(
      'the page root is visible and the list loading skeleton has cleared',
      async () => {
        await expect(lf.root).toBeVisible();
        await expect(lf.listLoading).toBeHidden();
      },
    );

    await Then('both flows appear in the list', async () => {
      await expect(lf.list).toBeVisible();
      await expect(lf.listItems).toHaveCount(2);
      await expect(lf.flowListItem(flowA.id)).toContainText('Single output flow');
      await expect(lf.flowListItem(flowB.id)).toContainText('Second flow');
    });

    await Then(
      'the editor renders for the first flow with the validation banner clean',
      async () => {
        await expect(lf.editor).toBeVisible();
        await expect(lf.editor).toContainText('Single output flow');
        await expect(lf.validationBanner).toHaveAttribute('data-issue-count', '0');
      },
    );
  });

  test('Scenario: clicking +LLM appends a node and saving sends the new shape to the server (拖拽节点) @smoke', async ({
    page,
  }) => {
    const lf = new LogicFlowsPage(page);
    const initial = singleOutputFlow();
    let flowList: MockFlow[] = [initial];
    const captured: Array<{ flowId: string; body: unknown }> = [];

    await Given('the page exposes a single-output flow with mutation hooks', async () => {
      await stubLogicFlowsApi(page, {
        flows: () => flowList,
        flow: (id) => flowList.find((f) => f.id === id),
        onUpdate: (flowId, body) => {
          captured.push({ flowId, body });
          const current = flowList.find((f) => f.id === flowId)!;
          const next: MockFlow = {
            ...current,
            nodes: body.nodes ?? current.nodes,
            edges: body.edges ?? current.edges,
            fallbackModel: body.fallbackModel ?? current.fallbackModel,
            maxRetries: body.maxRetries ?? current.maxRetries,
            updatedAt: '2026-05-01T00:01:00Z',
          };
          flowList = flowList.map((f) => (f.id === flowId ? next : f));
          return next;
        },
      });
    });

    await Given('the user has loaded the editor for that flow', async () => {
      await lf.goto();
      await expect(lf.editor).toBeVisible();
      await expect(lf.canvasNode('output')).toBeVisible();
    });

    await When('the user clicks the +LLM toolbar button', async () => {
      await lf.addNodeLlm.click();
    });

    await Then('a second node appears on the canvas', async () => {
      await expect(lf.canvasNodes).toHaveCount(2);
    });

    await When('the user clicks Save changes', async () => {
      await expect(lf.saveFlowBtn).toBeEnabled();
      await lf.saveFlowBtn.click();
    });

    await Then(
      'the PUT request carries the new node alongside the original output',
      async () => {
        await expect.poll(() => captured.length).toBe(1);
        const body = captured[0]!.body as { nodes: MockFlow['nodes'] };
        expect(body.nodes.length).toBe(2);
        const types = body.nodes.map((n) => n.type).sort();
        expect(types).toEqual(['llm', 'output']);
        // The new node's id is auto-generated (llm_<n>) — we just lock the
        // type + that the original 'output' node survived.
        expect(body.nodes.find((n) => n.id === 'output')).toBeTruthy();
      },
    );

    await Then('the Save button settles back to "Saved"', async () => {
      await expect(lf.saveFlowBtn).toContainText(/Saved/i);
    });
  });

  test('Scenario: a flow with two nodes and one edge renders the edge on the canvas (连线)', async ({
    page,
  }) => {
    const lf = new LogicFlowsPage(page);
    const flow = twoNodeFlowWithEdge();

    await Given('the page advertises a flow with 2 nodes and 1 edge', async () => {
      await stubLogicFlowsApi(page, {
        flows: () => [flow],
        flow: (id) => (id === flow.id ? flow : undefined),
      });
    });

    await When('the user opens /logic-flows', async () => {
      await lf.goto();
    });

    await Then('both nodes are present on the canvas', async () => {
      await expect(lf.canvasNode('greet')).toBeVisible();
      await expect(lf.canvasNode('output')).toBeVisible();
      await expect(lf.canvasNodes).toHaveCount(2);
    });

    await Then('exactly one edge is rendered between them', async () => {
      // ReactFlow renders edges as <g class="react-flow__edge ..."> SVG groups
      // inside the canvas viewport. Count locks "1 edge" without depending on
      // visual positioning.
      await expect(lf.canvasEdges).toHaveCount(1);
    });

    await Then('the validation banner reports no issues for the green path', async () => {
      await expect(lf.validationBanner).toHaveAttribute('data-issue-count', '0');
    });
  });

  test('Scenario: an llm node referencing an unknown id raises an unboundParam issue (变量绑定)', async ({
    page,
  }) => {
    const lf = new LogicFlowsPage(page);
    const flow = unboundParamFlow();

    await Given('the page exposes a flow whose prompt references `{{ghost.field}}`', async () => {
      await stubLogicFlowsApi(page, {
        flows: () => [flow],
        flow: (id) => (id === flow.id ? flow : undefined),
      });
    });

    await When('the user opens /logic-flows', async () => {
      await lf.goto();
      await expect(lf.editor).toBeVisible();
    });

    await Then('the validation banner counts one issue', async () => {
      await expect(lf.validationBanner).toHaveAttribute('data-issue-count', '1');
    });

    await Then('the issue is classified as unboundParam and points at the offending node', async () => {
      await expect(lf.validationIssues).toHaveCount(1);
      const issue = lf.validationIssues.first();
      await expect(issue).toHaveAttribute('data-issue-kind', 'unboundParam');
      await expect(issue).toContainText('ghost');
      await expect(issue).toContainText('greet');
    });

    await Then('the bound `{{input.name}}` placeholder does not produce a second issue', async () => {
      // Sanity: `input` is in VALID_PARAM_ROOTS so it should never raise.
      // Combined with the count=1 assertion above this locks "only the ghost
      // ref is flagged" without enumerating every issue.
      await expect(lf.validationBanner).toHaveAttribute('data-issue-count', '1');
    });
  });

  test('Scenario: running the flow renders the run panel with success status, output, and per-step trace (运行 + 查看 trace) @smoke', async ({
    page,
  }) => {
    const lf = new LogicFlowsPage(page);
    const flow = twoNodeFlowWithEdge();
    let executeCalls = 0;
    const capturedInputs: Array<Record<string, unknown> | undefined> = [];

    await Given('the page exposes a flow that responds to /execute with a success run', async () => {
      await stubLogicFlowsApi(page, {
        flows: () => [flow],
        flow: (id) => (id === flow.id ? flow : undefined),
        runs: () => [],
        onExecute: (_flowId, body) => {
          executeCalls += 1;
          capturedInputs.push(body.input);
          const run: MockRun = {
            id: 42,
            flowId: flow.id,
            status: 'success',
            input: body.input,
            output: { greet: { text: 'Hello Alice.' } },
            trace: [
              {
                nodeId: 'greet',
                type: 'llm',
                status: 'success',
                output: { text: 'Hello Alice.' },
              },
              {
                nodeId: 'output',
                type: 'output',
                status: 'success',
                output: { greet: { text: 'Hello Alice.' } },
              },
            ],
            createdAt: '2026-05-01T00:02:00Z',
          };
          return { status: 200, body: run };
        },
      });
    });

    await Given('the user has loaded the editor for that flow', async () => {
      await lf.goto();
      await expect(lf.editor).toBeVisible();
      await expect(lf.canvasNode('output')).toBeVisible();
    });

    await When('the user opens the Run panel and provides an input payload', async () => {
      await lf.toggleExecutePanel.click();
      await expect(lf.executePanel).toBeVisible();
      await lf.executeInput.fill('{ "name": "Alice" }');
    });

    await When('the user clicks Run', async () => {
      await expect(lf.executeFlowBtn).toBeEnabled();
      await lf.executeFlowBtn.click();
    });

    await Then('the POST /execute endpoint is invoked exactly once', async () => {
      await expect.poll(() => executeCalls).toBe(1);
      expect(capturedInputs.at(-1)).toEqual({ name: 'Alice' });
    });

    await Then('the run panel surfaces a success status and the run output', async () => {
      await expect(lf.runPanel).toBeVisible();
      await expect(lf.runPanel).toContainText('success');
      await expect(lf.runOutput).toBeVisible();
      await expect(lf.runOutput).toContainText('Hello Alice.');
    });

    await Then('every traced node appears as its own row with a status badge', async () => {
      await expect(lf.runTraceItems).toHaveCount(2);
      await expect(lf.traceItem('greet')).toHaveAttribute('data-status', 'success');
      await expect(lf.traceItem('output')).toHaveAttribute('data-status', 'success');
      await expect(lf.traceItem('greet')).toContainText('llm');
      await expect(lf.traceItem('output')).toContainText('output');
    });

    await Then('the execute error banner stays hidden on the happy path', async () => {
      await expect(lf.executeError).toBeHidden();
    });
  });

  test('Scenario: running the flow returns a failed run and the run panel shows the failed status + error (异常分支)', async ({
    page,
  }) => {
    const lf = new LogicFlowsPage(page);
    const flow = twoNodeFlowWithEdge();

    await Given('the page exposes a flow whose /execute reports a failed run', async () => {
      await stubLogicFlowsApi(page, {
        flows: () => [flow],
        flow: (id) => (id === flow.id ? flow : undefined),
        runs: () => [],
        onExecute: (_flowId, _body) => {
          // 200 with status=failed mirrors how the executor surfaces
          // recoverable failures back to the client (the SPA branches on
          // run.status rather than HTTP status).
          const run: MockRun = {
            id: 43,
            flowId: flow.id,
            status: 'failed',
            input: {},
            trace: [
              {
                nodeId: 'greet',
                type: 'llm',
                status: 'failed',
                error: 'mock provider rejected request',
              },
              {
                nodeId: 'output',
                type: 'output',
                status: 'skipped',
              },
            ],
            error: 'mock provider rejected request',
            createdAt: '2026-05-01T00:03:00Z',
          };
          return { status: 200, body: run };
        },
      });
    });

    await Given('the user has loaded the editor for that flow', async () => {
      await lf.goto();
      await expect(lf.editor).toBeVisible();
      await expect(lf.canvasNode('greet')).toBeVisible();
    });

    await When('the user opens the Run panel and clicks Run', async () => {
      await lf.toggleExecutePanel.click();
      await expect(lf.executePanel).toBeVisible();
      await lf.executeFlowBtn.click();
    });

    await Then('the run panel reports the failed status and surfaces the error message', async () => {
      await expect(lf.runPanel).toBeVisible();
      await expect(lf.runPanel).toContainText('failed');
      await expect(lf.runPanel).toContainText('mock provider rejected request');
    });

    await Then('the trace shows the failing step and the downstream skipped step', async () => {
      await expect(lf.runTraceItems).toHaveCount(2);
      await expect(lf.traceItem('greet')).toHaveAttribute('data-status', 'failed');
      await expect(lf.traceItem('output')).toHaveAttribute('data-status', 'skipped');
    });
  });

  test('Scenario: with no flows seeded, the empty state shows and the New flow modal opens on demand', async ({
    page,
  }) => {
    const lf = new LogicFlowsPage(page);

    await Given('the AIP logic-flows endpoint returns an empty list', async () => {
      await stubLogicFlowsApi(page, {
        flows: () => [],
        flow: () => undefined,
      });
    });

    await When('the user opens /logic-flows', async () => {
      await lf.goto();
    });

    await Then('the list empty wrapper and the editor empty wrapper are both visible', async () => {
      await expect(lf.root).toBeVisible();
      await expect(lf.listEmpty).toBeVisible();
      await expect(lf.editorEmpty).toBeVisible();
      await expect(lf.listItems).toHaveCount(0);
    });

    await When('the user clicks New', async () => {
      await lf.newFlowBtn.click();
    });

    await Then('the New Logic Flow modal opens with the name + id + description inputs', async () => {
      await expect(lf.modalOverlay).toBeVisible();
      await expect(lf.newFlowName).toBeVisible();
      await expect(lf.newFlowId).toBeVisible();
      await expect(lf.newFlowDescription).toBeVisible();
      await expect(lf.newFlowSubmit).toBeVisible();
    });
  });

  test('Scenario: selecting a node and running a dry-run surfaces the trace result inline', async ({
    page,
  }) => {
    const lf = new LogicFlowsPage(page);
    const flow = twoNodeFlowWithEdge();
    let dryRunCalls = 0;

    await Given('the page exposes a flow whose dry-run-node responds with a success trace', async () => {
      await stubLogicFlowsApi(page, {
        flows: () => [flow],
        flow: (id) => (id === flow.id ? flow : undefined),
        onDryRun: (_flowId, body) => {
          dryRunCalls += 1;
          return {
            status: 200,
            trace: {
              nodeId: body.node.id,
              type: body.node.type,
              status: 'success',
              output: { text: 'dry-run echo' },
            },
          };
        },
      });
    });

    await Given('the user has loaded the editor for that flow', async () => {
      await lf.goto();
      await expect(lf.editor).toBeVisible();
      await expect(lf.canvasNode('greet')).toBeVisible();
    });

    await When('the user selects the greet node on the canvas', async () => {
      await lf.canvasNode('greet').click();
      await expect(lf.nodeConfigPanel).toBeVisible();
      await expect(lf.dryRunPanel).toBeVisible();
    });

    await When('the user clicks Run node in the dry-run subpanel', async () => {
      await lf.dryRunBtn.click();
    });

    await Then('the POST /dry-run-node endpoint is invoked once', async () => {
      await expect.poll(() => dryRunCalls).toBe(1);
    });

    await Then('the dry-run result panel reports a success status and the output JSON', async () => {
      await expect(lf.dryRunResult).toBeVisible();
      await expect(lf.dryRunResult).toHaveAttribute('data-status', 'success');
      await expect(lf.dryRunOutput).toBeVisible();
      await expect(lf.dryRunOutput).toContainText('dry-run echo');
    });
  });
});
