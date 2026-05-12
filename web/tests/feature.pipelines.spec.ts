import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  PipelinesPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/pipelines` — the Pipelines browser rendered by
 * `src/components/pipelines/PipelinesPage.tsx`.
 *
 * PRD AC for US-025 names 5 scenarios: 列表 / 创建 / 调度 / 运行历史 / 回滚.
 * The page is **read-only** — there is no in-page CRUD or rollback affordance
 * (pipelines are created via `POST /api/v2/pipelines`; rollback is not
 * implemented; live run history is gated on US-298 per the in-page log-panel
 * caption). Each AC therefore maps onto the page's actual capability:
 *   - 列表       → Scenario 1 (list-renders + auto-select-first) and
 *                   Scenario 2 (list-item click switches detail).
 *   - 创建       → Scenario 7 (empty-state copy points at the documented
 *                   `POST /api/v2/pipelines` create path — the page itself
 *                   doesn't bake a Create button; same honest-mapping
 *                   convention as US-022's "批量取消" call-out and US-024's
 *                   "拖拽节点" toolbar mapping).
 *   - 调度       → Scenario 3 (header + log-panel surface the cron schedule
 *                   and enabled/disabled state in lockstep).
 *   - 运行历史   → Scenario 5 (log-panel placeholder explicitly cites US-298
 *                   as the landing point for live run history; this scenario
 *                   locks the placeholder copy so future work doesn't ship
 *                   silent regressions).
 *   - 回滚       → Scenario 6 (page does not implement an in-page rollback;
 *                   ditto pattern — explicit absence assertion documents the
 *                   gap until the affordance ships).
 *
 * Two boundary scenarios (Scenario 8 list-error + Scenario 9 detail-error)
 * lock the remaining state branches of the page — same template as
 * US-021/022/023/024.
 */

interface MockPipeline {
  id: string;
  name: string;
  description?: string;
  inputs: Array<{
    name: string;
    type: string;
    config?: Record<string, unknown>;
  }>;
  transforms: Array<{
    name: string;
    type: string;
    inputs?: string[];
    config?: Record<string, unknown>;
  }>;
  outputs: Array<{
    name: string;
    type: string;
    input?: string;
    config?: Record<string, unknown>;
  }>;
  schedule?: string;
  enabled: boolean;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}

function pipelineAlpha(): MockPipeline {
  return {
    id: 'pipe_alpha',
    name: 'Alpha pipeline',
    description: 'Reads users, tags them, writes to warehouse.',
    inputs: [
      { name: 'src_users', type: 'objectset', config: { ontology: 'main' } },
    ],
    transforms: [
      {
        name: 'tag_premium',
        type: 'derive',
        inputs: ['src_users'],
        config: { formula: 'isPremium ? "yes" : "no"' },
      },
      {
        name: 'enrich',
        type: 'lookup',
        inputs: ['tag_premium'],
        config: { table: 'premium_overrides' },
      },
    ],
    outputs: [
      {
        name: 'sink_warehouse',
        type: 'jdbc',
        input: 'enrich',
        config: { table: 'users' },
      },
    ],
    schedule: '0 */6 * * *',
    enabled: true,
    createdBy: 'user-1',
    createdAt: '2026-04-28T10:00:00Z',
    updatedAt: '2026-04-28T10:00:00Z',
  };
}

function pipelineBeta(): MockPipeline {
  return {
    id: 'pipe_beta',
    name: 'Beta pipeline',
    inputs: [{ name: 'csv_in', type: 'csv', config: {} }],
    transforms: [],
    outputs: [
      { name: 'sink_kafka', type: 'kafka', input: 'csv_in', config: {} },
    ],
    enabled: false,
    createdBy: 'user-1',
    createdAt: '2026-04-28T11:00:00Z',
    updatedAt: '2026-04-28T11:00:00Z',
  };
}

interface StubRefs {
  pipelines: () => MockPipeline[];
  pipeline: (id: string) => MockPipeline | undefined;
  listFail?: () => boolean;
  detailFail?: (id: string) => boolean;
}

/**
 * Wire up the two endpoints the page hits on load (list + per-id detail).
 * Register the narrower `/pipelines/*` route AFTER the parent `/pipelines`
 * route so Playwright's LIFO pattern resolution dispatches them correctly
 * (same convention as US-023 / US-024 stubs).
 */
async function stubPipelinesApi(page: Page, refs: StubRefs): Promise<void> {
  await page.route('**/api/v2/pipelines', async (route: Route) => {
    const req = route.request();
    if (req.method() !== 'GET') {
      await route.continue();
      return;
    }
    if (refs.listFail?.() ?? false) {
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
      body: JSON.stringify({ pipelines: refs.pipelines() }),
    });
  });

  await page.route('**/api/v2/pipelines/*', async (route: Route) => {
    const req = route.request();
    if (req.method() !== 'GET') {
      await route.continue();
      return;
    }
    const url = new URL(req.url());
    const segments = url.pathname.split('/');
    const id = decodeURIComponent(segments[segments.length - 1]);
    if (refs.detailFail?.(id) ?? false) {
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
    const p = refs.pipeline(id);
    if (!p) {
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
      body: JSON.stringify(p),
    });
  });
}

describeFeature('Pipelines page', () => {
  test('Scenario: opening /pipelines renders the list with two pipelines and auto-selects the first (列表) @smoke', async ({
    page,
    request,
  }) => {
    const pp = new PipelinesPage(page);
    const alpha = pipelineAlpha();
    const beta = pipelineBeta();
    const list = [alpha, beta];

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the pipelines endpoint advertises two pipelines', async () => {
      await stubPipelinesApi(page, {
        pipelines: () => list,
        pipeline: (id) => list.find((p) => p.id === id),
      });
    });

    await When('the user opens /pipelines', async () => {
      await pp.goto();
    });

    await Then(
      'the page root is visible and the list loading wrapper has cleared',
      async () => {
        await expect(pp.root).toBeVisible();
        await expect(pp.listLoading).toBeHidden();
      },
    );

    await Then('both pipelines appear in the sidebar', async () => {
      await expect(pp.list).toBeVisible();
      await expect(pp.listItems).toHaveCount(2);
      await expect(pp.pipelineListItem(alpha.id)).toContainText(
        'Alpha pipeline',
      );
      await expect(pp.pipelineListItem(beta.id)).toContainText('Beta pipeline');
    });

    await Then(
      'the first pipeline is auto-selected and its detail panel renders',
      async () => {
        await expect(pp.detail).toBeVisible();
        await expect(pp.detail).toContainText('Alpha pipeline');
        await expect(pp.detailEmpty).toBeHidden();
      },
    );
  });

  test('Scenario: clicking the second list item switches the detail to the Beta pipeline (列表) @smoke', async ({
    page,
  }) => {
    const pp = new PipelinesPage(page);
    const alpha = pipelineAlpha();
    const beta = pipelineBeta();
    const list = [alpha, beta];

    await Given(
      'the page is loaded with both Alpha and Beta pipelines',
      async () => {
        await stubPipelinesApi(page, {
          pipelines: () => list,
          pipeline: (id) => list.find((p) => p.id === id),
        });
        await pp.goto();
        // Wait for the auto-selected Alpha detail to settle so the click on
        // Beta is the only mutation under test.
        await expect(pp.detail).toContainText('Alpha pipeline');
      },
    );

    await When('the user clicks the Beta pipeline in the sidebar', async () => {
      await pp.pipelineListItem(beta.id).click();
    });

    await Then('the detail panel switches to Beta', async () => {
      await expect(pp.detail).toContainText('Beta pipeline');
    });

    await Then(
      'the graph re-renders with only the Beta nodes (csv_in, sink_kafka)',
      async () => {
        // Beta has 1 input + 0 transforms + 1 output → 2 nodes total.
        await expect(pp.graphNodes).toHaveCount(2);
        await expect(pp.graphNode('csv_in')).toBeVisible();
        await expect(pp.graphNode('sink_kafka')).toBeVisible();
      },
    );
  });

  test('Scenario: the detail header and log panel report the schedule and enabled state in lockstep (调度) @smoke', async ({
    page,
  }) => {
    const pp = new PipelinesPage(page);
    const alpha = pipelineAlpha();
    const beta = pipelineBeta();
    const list = [alpha, beta];

    await Given('the page is loaded with both pipelines', async () => {
      await stubPipelinesApi(page, {
        pipelines: () => list,
        pipeline: (id) => list.find((p) => p.id === id),
      });
      await pp.goto();
      await expect(pp.detail).toContainText('Alpha pipeline');
    });

    await Then(
      `the Alpha log-panel surfaces the cron schedule "${alpha.schedule}" and the enabled badge`,
      async () => {
        await expect(pp.logSchedule).toHaveText(alpha.schedule!);
        await expect(pp.logEnabled).toHaveText('enabled');
        // The header carries the same cron + badge — keeps the two surfaces
        // in lockstep so we'd notice if either drifts.
        await expect(pp.detail).toContainText(alpha.schedule!);
        await expect(pp.detail).toContainText('enabled');
      },
    );

    await When('the user switches to the Beta pipeline', async () => {
      await pp.pipelineListItem(beta.id).click();
      await expect(pp.detail).toContainText('Beta pipeline');
    });

    await Then(
      'Beta surfaces "on demand" + the disabled badge in both the header and the log panel',
      async () => {
        await expect(pp.logSchedule).toHaveText('on demand');
        await expect(pp.logEnabled).toHaveText('disabled');
        await expect(pp.detail).toContainText('on demand');
        await expect(pp.detail).toContainText('disabled');
      },
    );
  });

  test('Scenario: clicking a graph node activates the log panel selection with name, kind, type, and config JSON', async ({
    page,
  }) => {
    const pp = new PipelinesPage(page);
    const alpha = pipelineAlpha();

    await Given('the page is loaded with the Alpha pipeline auto-selected', async () => {
      await stubPipelinesApi(page, {
        pipelines: () => [alpha],
        pipeline: (id) => (id === alpha.id ? alpha : undefined),
      });
      await pp.goto();
      await expect(pp.detail).toContainText('Alpha pipeline');
      await expect(pp.graphNodes).toHaveCount(4);
    });

    await Then(
      'the log-panel starts in the no-selection state',
      async () => {
        await expect(pp.logNoSelection).toBeVisible();
        await expect(pp.logSelected).toBeHidden();
      },
    );

    await When('the user clicks the tag_premium transform node', async () => {
      await pp.graphNode('tag_premium').click();
    });

    await Then(
      'the log-panel switches to the selected state and surfaces the node metadata',
      async () => {
        await expect(pp.logSelected).toBeVisible();
        await expect(pp.logNoSelection).toBeHidden();
        await expect(pp.logSelected).toContainText('tag_premium');
        await expect(pp.logSelected).toContainText('Transform');
        await expect(pp.logSelected).toContainText('derive');
        // The upstream chip mirrors the DAG (src_users → tag_premium).
        await expect(pp.logSelected).toContainText('src_users');
      },
    );

    await Then(
      'the log-panel config block carries the node config JSON verbatim',
      async () => {
        await expect(pp.logConfig).toBeVisible();
        await expect(pp.logConfig).toContainText('isPremium');
        await expect(pp.logConfig).toContainText('formula');
      },
    );
  });

  test('Scenario: the log panel placeholder cites US-298 as the landing point for live run history (运行历史)', async ({
    page,
  }) => {
    const pp = new PipelinesPage(page);
    const alpha = pipelineAlpha();

    await Given(
      'the page is loaded with the Alpha pipeline auto-selected',
      async () => {
        await stubPipelinesApi(page, {
          pipelines: () => [alpha],
          pipeline: (id) => (id === alpha.id ? alpha : undefined),
        });
        await pp.goto();
        await expect(pp.detail).toContainText('Alpha pipeline');
      },
    );

    await Then(
      'the pipeline log panel is visible alongside the graph',
      async () => {
        await expect(pp.logPanel).toBeVisible();
        await expect(pp.logPanel).toContainText('Pipeline log');
      },
    );

    await Then(
      'the placeholder explicitly defers live run history to US-298',
      async () => {
        // Locking the copy here means future work that wires the panel up to
        // a live run feed has to either keep / update this assertion. If the
        // copy silently drifts, this scenario screams.
        await expect(pp.logPanel).toContainText(/Live run history/i);
        await expect(pp.logPanel).toContainText(/US-298/);
      },
    );
  });

  test('Scenario: there is no in-page rollback affordance — the page is read-only (回滚)', async ({
    page,
  }) => {
    const pp = new PipelinesPage(page);
    const alpha = pipelineAlpha();

    await Given(
      'the page is loaded with the Alpha pipeline auto-selected',
      async () => {
        await stubPipelinesApi(page, {
          pipelines: () => [alpha],
          pipeline: (id) => (id === alpha.id ? alpha : undefined),
        });
        await pp.goto();
        await expect(pp.detail).toContainText('Alpha pipeline');
      },
    );

    await Then(
      'no rollback / revert button is present anywhere on the page',
      async () => {
        // Explicit absence assertion: documents the AC gap until a rollback
        // affordance ships. Same honest-mapping pattern as US-022's "批量取
        // 消" / US-024's "拖拽节点 → toolbar +LLM" mappings. When rollback
        // arrives, replace this scenario with a click-driven happy path.
        await expect(
          page.getByRole('button', { name: /rollback|revert/i }),
        ).toHaveCount(0);
      },
    );

    await Then(
      'the detail panel header only carries the schedule + enabled badges, no mutation controls',
      async () => {
        // The detail header today is read-only metadata. Locking that means
        // any new button added to the header without a deliberate test
        // update will trip this scenario — caller's responsibility to either
        // update the assertion or add a dedicated mutation scenario.
        const headerButtons = pp.detail
          .locator('header')
          .getByRole('button');
        await expect(headerButtons).toHaveCount(0);
      },
    );
  });

  test('Scenario: with no pipelines seeded, the empty state copy points at the documented create endpoint (创建)', async ({
    page,
  }) => {
    const pp = new PipelinesPage(page);

    await Given(
      'the pipelines endpoint returns an empty list',
      async () => {
        await stubPipelinesApi(page, {
          pipelines: () => [],
          pipeline: () => undefined,
        });
      },
    );

    await When('the user opens /pipelines', async () => {
      await pp.goto();
    });

    await Then(
      'the list-empty wrapper is visible and contains the create-via-API hint',
      async () => {
        await expect(pp.listEmpty).toBeVisible();
        await expect(pp.listEmpty).toContainText(/POST \/api\/v2\/pipelines/);
      },
    );

    await Then(
      'the detail panel shows its no-selection empty state',
      async () => {
        await expect(pp.detailEmpty).toBeVisible();
        await expect(pp.listItems).toHaveCount(0);
      },
    );
  });

  test('Scenario: when /pipelines list returns 500 the sidebar shows the error wrapper', async ({
    page,
  }) => {
    const pp = new PipelinesPage(page);

    await Given(
      'the pipelines list endpoint is stubbed to return 500',
      async () => {
        await stubPipelinesApi(page, {
          pipelines: () => [],
          pipeline: () => undefined,
          listFail: () => true,
        });
      },
    );

    await When('the user opens /pipelines', async () => {
      await pp.goto();
    });

    await Then('the list-error wrapper renders with role=alert', async () => {
      await expect(pp.listError).toBeVisible();
      await expect(pp.listError).toHaveAttribute('role', 'alert');
    });

    await Then(
      'no pipeline list items are rendered and the detail empty wrapper stays put',
      async () => {
        await expect(pp.listItems).toHaveCount(0);
        await expect(pp.detailEmpty).toBeVisible();
      },
    );
  });

  test('Scenario: when /pipelines list works but the per-id detail fails, the detail-error wrapper renders', async ({
    page,
  }) => {
    const pp = new PipelinesPage(page);
    const alpha = pipelineAlpha();

    await Given(
      'the list works but the Alpha detail endpoint is stubbed to return 500',
      async () => {
        await stubPipelinesApi(page, {
          pipelines: () => [alpha],
          pipeline: (id) => (id === alpha.id ? alpha : undefined),
          detailFail: (id) => id === alpha.id,
        });
      },
    );

    await When('the user opens /pipelines', async () => {
      await pp.goto();
    });

    await Then(
      'the list still surfaces the Alpha row but the detail panel switches to its error wrapper',
      async () => {
        await expect(pp.pipelineListItem(alpha.id)).toBeVisible();
        await expect(pp.detailError).toBeVisible();
        await expect(pp.detailError).toContainText(/Failed to load/i);
      },
    );
  });
});
