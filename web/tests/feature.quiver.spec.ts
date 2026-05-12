import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  QuiverPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/quiver/:ontology[/:rid]` — the Quiver Workbench
 * rendered by `src/components/quiver/QuiverPage.tsx`, plus the
 * read-only share surface rendered by `QuiverViewPage.tsx`.
 *
 * Scenarios map the PRD AC for US-034 (upgrade us-457-timeseries-tab
 * smoke into a full Quiver BDD suite):
 *
 *   AC: "至少 6 scenarios：多系列叠加、时间窗调节、聚合粒度切换、
 *        导出 dashboard、共享链接、空数据态"
 *
 * Honest mapping (mirroring US-025/028/030/033):
 *   - "多系列叠加" → happy path: pick form drives two SeriesSpec rows
 *     into the aggregate panel; chart wrapper mounts; both `quiver-row-*`
 *     testids resolve.
 *   - "时间窗调节" → split into two scenarios. (a) The `quiver-selection-*`
 *     testids reflect the "all → all" full range out of the box and the
 *     clear-selection button is absent until a chart drag publishes a
 *     selection. The streamPoints request URL is captured to lock the
 *     wire-level contract that the workbench does NOT pass a time-window
 *     query param today — Quiver fetches the full series and clips
 *     client-side via the aggregate panel selection. (b) Honest-mapping
 *     locks the gap: today there is no preset 1h/24h/7d/30d picker.
 *   - "聚合粒度切换" → no granularity selector exists today. The
 *     workbench renders fixed `Count / Sum / Avg / Min / Max` columns
 *     over the entire (or selected) range; bucket size / step is fully
 *     implicit. Lock with triple role-based absence assertions.
 *   - "导出 dashboard" → no CSV/PNG/JSON export today; absence assertions
 *     mirror the US-028/033 button+link double pattern.
 *   - "共享链接" → click the `quiver-share-{rid}` button on a saved row
 *     and verify the page navigates to the read-only view route. The
 *     read-only view does NOT render the save controls or per-row remove
 *     buttons — assert both gaps to lock the "share view is read-only"
 *     contract.
 *   - "空数据态" → before any series is added, the "No series yet"
 *     EmptyState renders and neither the chart nor the aggregate panel
 *     mounts. Locks the pre-Add state branch.
 *
 * Every scenario stubs the four endpoints QuiverPage actually hits:
 *   - GET /api/v2/quiver/dashboards (`listQuiverDashboards`)
 *   - POST /api/v2/quiver/save (`saveQuiverDashboard`)
 *   - DELETE /api/v2/quiver/dashboards/{rid}
 *   - GET /api/v2/quiver/dashboards/{rid}/view (read-only share view)
 *   - POST /api/v2/ontologies/.../timeseries/.../streamPoints
 *     (`streamTimeSeriesPoints`) — the per-series fetch fired by
 *     QuiverWorkbenchView's SeriesFetcher.
 */

const ONTOLOGY = 'northwind';

interface QuiverSeriesConfig {
  id: string;
  objectType: string;
  primaryKey: string;
  property: string;
  label: string;
  color: string;
  branch?: string;
}

interface QuiverDashboard {
  rid: string;
  name: string;
  owner: string;
  config: { ontologyApiName: string; series: QuiverSeriesConfig[] };
  createdAt: string;
  updatedAt: string;
}

interface SaveBody {
  rid?: string;
  name: string;
  config: { ontologyApiName: string; series: QuiverSeriesConfig[] };
}

interface CapturedTimeseries {
  url: string;
  path: string;
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
  branch: string | null;
  searchParamKeys: string[];
}

interface QuiverStubs {
  saved: QuiverDashboard[];
  savedCalls: SaveBody[];
  deletedRids: string[];
  timeseries: CapturedTimeseries[];
  /** Per-series points keyed by `${objectType}|${primaryKey}|${property}`. */
  pointsBySlot: Map<string, Array<{ time: string; value: number }>>;
}

function parseTimeseriesPath(path: string): {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
} | null {
  // /api/v2/ontologies/{ont}/objects/{ot}/{pk}/timeseries/{prop}/streamPoints
  const m = path.match(
    /^\/api\/v2\/ontologies\/([^/]+)\/objects\/([^/]+)\/([^/]+)\/timeseries\/([^/]+)\/streamPoints$/,
  );
  if (!m) return null;
  return {
    ontologyApiName: decodeURIComponent(m[1]!),
    objectType: decodeURIComponent(m[2]!),
    primaryKey: decodeURIComponent(m[3]!),
    property: decodeURIComponent(m[4]!),
  };
}

async function stubQuiverEndpoints(
  page: Page,
  stubs: QuiverStubs,
): Promise<void> {
  // Dashboard list — GET /api/v2/quiver/dashboards
  await page.route('**/api/v2/quiver/dashboards', async (route: Route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ dashboards: stubs.saved }),
      });
      return;
    }
    await route.continue();
  });

  // Save — POST /api/v2/quiver/save
  await page.route('**/api/v2/quiver/save', async (route: Route) => {
    if (route.request().method() !== 'POST') {
      await route.continue();
      return;
    }
    const body = route.request().postDataJSON() as SaveBody;
    stubs.savedCalls.push(body);
    const rid =
      body.rid ?? `ri.quiver.main.dashboard.${stubs.savedCalls.length}`;
    const saved: QuiverDashboard = {
      rid,
      name: body.name,
      owner: 'user:test',
      config: body.config,
      createdAt: '2026-05-13T00:00:00Z',
      updatedAt: '2026-05-13T00:00:00Z',
    };
    // Reflect into the saved list so subsequent GET sees it.
    const existingIdx = stubs.saved.findIndex((d) => d.rid === rid);
    if (existingIdx >= 0) {
      stubs.saved[existingIdx] = saved;
    } else {
      stubs.saved.push(saved);
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(saved),
    });
  });

  // Delete + GET-by-rid + view by rid — keyed on path shape
  await page.route(
    '**/api/v2/quiver/dashboards/**',
    async (route: Route) => {
      const url = new URL(route.request().url());
      const trimmed = url.pathname.replace(/\/+$/, '');
      const parts = trimmed.split('/');
      const method = route.request().method();
      // Path is one of:
      //   /api/v2/quiver/dashboards/{rid}
      //   /api/v2/quiver/dashboards/{rid}/view
      const last = parts.at(-1) ?? '';
      const isView = last === 'view';
      const rid = decodeURIComponent(isView ? (parts.at(-2) ?? '') : last);

      if (method === 'DELETE') {
        stubs.deletedRids.push(rid);
        stubs.saved = stubs.saved.filter((d) => d.rid !== rid);
        await route.fulfill({ status: 204, body: '' });
        return;
      }

      if (method === 'GET') {
        const match = stubs.saved.find((d) => d.rid === rid);
        if (!match) {
          await route.fulfill({
            status: 404,
            contentType: 'application/json',
            body: JSON.stringify({
              errorCode: 'NOT_FOUND',
              errorName: 'DashboardNotFound',
              errorInstanceId: 'spec',
            }),
          });
          return;
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(match),
        });
        return;
      }

      await route.continue();
    },
  );

  // Timeseries fetch — POST .../timeseries/{prop}/streamPoints
  await page.route(
    '**/api/v2/ontologies/**/timeseries/**/streamPoints**',
    async (route: Route) => {
      if (route.request().method() !== 'POST') {
        await route.continue();
        return;
      }
      const url = new URL(route.request().url());
      const parsed = parseTimeseriesPath(url.pathname);
      if (!parsed) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: '[]',
        });
        return;
      }
      const branchParam = url.searchParams.get('branch');
      const searchParamKeys = [...url.searchParams.keys()];
      stubs.timeseries.push({
        url: route.request().url(),
        path: url.pathname,
        ontologyApiName: parsed.ontologyApiName,
        objectType: parsed.objectType,
        primaryKey: parsed.primaryKey,
        property: parsed.property,
        branch: branchParam,
        searchParamKeys,
      });
      const slotKey = `${parsed.objectType}|${parsed.primaryKey}|${parsed.property}`;
      const points = stubs.pointsBySlot.get(slotKey) ?? [];
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(points),
      });
    },
  );
}

function newStubs(): QuiverStubs {
  return {
    saved: [],
    savedCalls: [],
    deletedRids: [],
    timeseries: [],
    pointsBySlot: new Map(),
  };
}

describeFeature('Quiver Workbench', () => {
  test('Scenario: stacking two series onto the chart populates the aggregate panel for both @smoke', async ({
    page,
    request,
  }) => {
    // Locks AC "多系列叠加": pick form drives two SeriesSpec rows; both
    // resolve into the aggregate panel; chart wrapper mounts; per-row
    // count/sum/avg cells reflect the per-slot points. Two distinct
    // (objectType, primaryKey, property) slots are used so QuiverPage's
    // `colorForSlot` assigns two distinct colours (single-slot multi-
    // branch overlay reuse is covered by the vitest US-404 case).
    const quiver = new QuiverPage(page);
    const stubs = newStubs();
    stubs.pointsBySlot.set('Server|s1|cpu', [
      { time: '2026-05-13T10:00:00Z', value: 10 },
      { time: '2026-05-13T11:00:00Z', value: 30 },
    ]);
    stubs.pointsBySlot.set('Server|s1|mem', [
      { time: '2026-05-13T10:00:00Z', value: 200 },
      { time: '2026-05-13T11:00:00Z', value: 250 },
    ]);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the quiver endpoints return empty list + canned points', async () => {
      await stubQuiverEndpoints(page, stubs);
    });

    await When('the user opens /quiver/northwind', async () => {
      await quiver.goto(ONTOLOGY);
      await expect(quiver.root).toBeVisible();
    });

    await Then('the page starts in the no-series empty state', async () => {
      await expect(quiver.aggregatePanel).toHaveCount(0);
      await expect(quiver.chartPanel).toHaveCount(0);
      await expect(page.getByText(/no series yet/i)).toBeVisible();
    });

    await When('the user adds Server/s1.cpu as the first series', async () => {
      await quiver.addSeries({
        objectType: 'Server',
        primaryKey: 's1',
        property: 'cpu',
      });
    });

    await When('the user adds Server/s1.mem as the second series', async () => {
      await quiver.addSeries({
        objectType: 'Server',
        primaryKey: 's1',
        property: 'mem',
      });
    });

    await Then('the chart wrapper and aggregate panel both mount', async () => {
      await expect(quiver.chartPanel).toBeVisible();
      await expect(quiver.aggregatePanel).toBeVisible();
    });

    await Then('both aggregate rows render with the expected slot prefixes', async () => {
      await expect(
        quiver.rowForSlot('Server', 's1', 'cpu'),
      ).toHaveCount(1);
      await expect(
        quiver.rowForSlot('Server', 's1', 'mem'),
      ).toHaveCount(1);
    });

    await Then('the cpu row count + sum reflect the canned points', async () => {
      const row = quiver.rowForSlot('Server', 's1', 'cpu');
      await expect(row).toContainText('Server/s1.cpu');
      // count=2, sum=40.00, avg=20.00 per quiverAggregation formatting
      await expect(row).toContainText('2');
      await expect(row).toContainText('40.00');
      await expect(row).toContainText('20.00');
    });

    await Then('two streamPoints fetches were issued, one per slot', async () => {
      await expect
        .poll(() => stubs.timeseries.length, { timeout: 5000 })
        .toBeGreaterThanOrEqual(2);
      const slots = stubs.timeseries.map(
        (t) => `${t.objectType}|${t.primaryKey}|${t.property}`,
      );
      expect(slots).toContain('Server|s1|cpu');
      expect(slots).toContain('Server|s1|mem');
    });
  });

  test('Scenario: selection window defaults to "all" and the streamPoints fetch carries no time-range query param', async ({
    page,
  }) => {
    // Locks the first half of AC "时间窗调节": the aggregate panel
    // surfaces a "selection start → selection end" header that is
    // "all → all" when no range has been published from the chart, and
    // the clear-selection button is absent. We also lock the wire
    // contract that the workbench's streamPoints request does NOT carry
    // a `from`/`to`/`range` query param — Quiver fetches the full series
    // and clips client-side via the aggregate panel's selection.
    const quiver = new QuiverPage(page);
    const stubs = newStubs();
    stubs.pointsBySlot.set('Server|s1|cpu', [
      { time: '2026-05-13T10:00:00Z', value: 10 },
      { time: '2026-05-13T12:00:00Z', value: 50 },
    ]);

    await Given('the quiver endpoints return empty list + canned points', async () => {
      await stubQuiverEndpoints(page, stubs);
    });

    await When('the user adds a Server/s1.cpu series', async () => {
      await quiver.goto(ONTOLOGY);
      await quiver.addSeries({
        objectType: 'Server',
        primaryKey: 's1',
        property: 'cpu',
      });
      await expect(quiver.aggregatePanel).toBeVisible();
    });

    await Then('the selection header reads "all → all"', async () => {
      await expect(quiver.selectionStart).toHaveText('all');
      await expect(quiver.selectionEnd).toHaveText('all');
    });

    await Then('the clear-selection button is absent (no range has been chosen)', async () => {
      await expect(quiver.clearSelectionBtn).toHaveCount(0);
    });

    await Then('the captured streamPoints call carries no time-range param', async () => {
      await expect.poll(() => stubs.timeseries.length).toBeGreaterThanOrEqual(1);
      const call = stubs.timeseries[0]!;
      // Only `branch` is allowed when the user passes an override; no
      // overlay or workbench-level setting injects from / to / range.
      // Quiver's wire contract today is "full series, client-clipped".
      expect(call.searchParamKeys).not.toContain('from');
      expect(call.searchParamKeys).not.toContain('to');
      expect(call.searchParamKeys).not.toContain('range');
      expect(call.searchParamKeys).not.toContain('window');
    });

    await Then('no preset 1h/24h/7d/30d range picker is rendered', async () => {
      // Honest-mapping for AC "时间窗调节": only the chart-drag
      // selection surface exists today; there is no preset window
      // picker. Triple role-based absence locks the gap so a future
      // PR adding 1h/24h/7d buttons must replace this assertion with
      // click-driven coverage.
      const presetRegex = /^(1\s*h|24\s*h|7\s*d|30\s*d|1\s*hour|24\s*hours|7\s*days|30\s*days)$/i;
      await expect(page.getByRole('button', { name: presetRegex })).toHaveCount(0);
      await expect(page.getByRole('tab', { name: presetRegex })).toHaveCount(0);
      await expect(page.getByRole('radio', { name: presetRegex })).toHaveCount(0);
    });
  });

  test('Scenario: aggregation granularity / bucket-size switching has no affordance today', async ({
    page,
  }) => {
    // Honest mapping for AC "聚合粒度切换": the workbench renders fixed
    // count/sum/avg/min/max columns over the (entire or selected) range
    // — bucket size and aggregation step are fully implicit. Lock the
    // gap with triple role-based absence + a positive assertion that
    // the streamPoints fetch carries no `step` / `bucket` / `interval`
    // query param.
    const quiver = new QuiverPage(page);
    const stubs = newStubs();
    stubs.pointsBySlot.set('Server|s1|cpu', [
      { time: '2026-05-13T10:00:00Z', value: 10 },
    ]);

    await Given('the quiver endpoints return empty list + canned points', async () => {
      await stubQuiverEndpoints(page, stubs);
    });

    await When('the user adds a series and reaches the aggregate panel', async () => {
      await quiver.goto(ONTOLOGY);
      await quiver.addSeries({
        objectType: 'Server',
        primaryKey: 's1',
        property: 'cpu',
      });
      await expect(quiver.aggregatePanel).toBeVisible();
    });

    await Then('no granularity / step / bucket-size picker is rendered', async () => {
      const granRegex = /granularity|bucket\s*size|step|resolution|interval/i;
      await expect(page.getByRole('button', { name: granRegex })).toHaveCount(0);
      await expect(page.getByRole('combobox', { name: granRegex })).toHaveCount(0);
      await expect(page.getByRole('textbox', { name: granRegex })).toHaveCount(0);
      await expect(page.getByRole('tab', { name: granRegex })).toHaveCount(0);
    });

    await Then('the streamPoints call carries no granularity / step query param', async () => {
      await expect.poll(() => stubs.timeseries.length).toBeGreaterThanOrEqual(1);
      const call = stubs.timeseries[0]!;
      expect(call.searchParamKeys).not.toContain('step');
      expect(call.searchParamKeys).not.toContain('bucket');
      expect(call.searchParamKeys).not.toContain('interval');
      expect(call.searchParamKeys).not.toContain('granularity');
    });
  });

  test('Scenario: dashboard export (CSV / PNG / JSON) has no affordance today', async ({
    page,
  }) => {
    // Honest mapping for AC "导出 dashboard": no export
    // button/link/menu surfaces a CSV / PNG / JSON dump today. Mirror
    // the US-028/033 button + link double absence pattern with a
    // regex covering Export Dashboard / Download CSV / Export PNG /
    // bare "CSV" / "JSON" labels.
    const quiver = new QuiverPage(page);
    const stubs = newStubs();
    stubs.pointsBySlot.set('Server|s1|cpu', [
      { time: '2026-05-13T10:00:00Z', value: 10 },
    ]);

    await Given('the quiver endpoints return empty list + canned points', async () => {
      await stubQuiverEndpoints(page, stubs);
    });

    await When('the user adds a series to reveal the chart + aggregate surfaces', async () => {
      await quiver.goto(ONTOLOGY);
      await quiver.addSeries({
        objectType: 'Server',
        primaryKey: 's1',
        property: 'cpu',
      });
      await expect(quiver.chartPanel).toBeVisible();
    });

    await Then('no export button or link is rendered anywhere on the page', async () => {
      const exportRegex =
        /export\s*(dashboard|chart|series|data)?\s*(csv|png|json|svg)?|download\s*(csv|png|json|svg|dashboard|chart)|\bcsv\b|\bpng\b/i;
      await expect(page.getByRole('button', { name: exportRegex })).toHaveCount(0);
      await expect(page.getByRole('link', { name: exportRegex })).toHaveCount(0);
      await expect(page.getByRole('menuitem', { name: exportRegex })).toHaveCount(0);
    });
  });

  test('Scenario: clicking the share button on a saved dashboard navigates to the read-only view @smoke', async ({
    page,
  }) => {
    // Locks AC "共享链接": each saved-list entry exposes a `share`
    // button that navigates to `/quiver/{ontology}/{rid}/view`. The
    // QuiverViewPage mounts the same QuiverWorkbenchView but hides the
    // save controls + picker form + per-row remove buttons. Lock both
    // halves (URL navigation + read-only contract).
    const quiver = new QuiverPage(page);
    const stubs = newStubs();
    const rid = 'ri.quiver.main.dashboard.shared';
    stubs.saved.push({
      rid,
      name: 'Shared CPU Snapshot',
      owner: 'user:test',
      config: {
        ontologyApiName: ONTOLOGY,
        series: [
          {
            id: 'Server|s1|cpu|main|1',
            objectType: 'Server',
            primaryKey: 's1',
            property: 'cpu',
            label: 'CPU s1',
            color: '#22d3ee',
          },
        ],
      },
      createdAt: '2026-05-13T00:00:00Z',
      updatedAt: '2026-05-13T00:00:00Z',
    });
    stubs.pointsBySlot.set('Server|s1|cpu', [
      { time: '2026-05-13T10:00:00Z', value: 42 },
    ]);

    await Given('the quiver endpoints return one saved dashboard', async () => {
      await stubQuiverEndpoints(page, stubs);
    });

    await When('the user opens the workbench page', async () => {
      await quiver.goto(ONTOLOGY);
      await expect(quiver.root).toBeVisible();
    });

    await Then('the saved-list panel shows the dashboard with editor + share affordances', async () => {
      await expect(quiver.savedList).toBeVisible();
      await expect(quiver.savedDashboard(rid)).toBeVisible();
      await expect(quiver.loadDashboardBtn(rid)).toBeVisible();
      await expect(quiver.shareDashboardBtn(rid)).toBeVisible();
      await expect(quiver.deleteDashboardBtn(rid)).toBeVisible();
    });

    await When('the user clicks the share button', async () => {
      await quiver.shareDashboardBtn(rid).click();
    });

    await Then('the URL navigates to the read-only share view', async () => {
      await expect.poll(() => new URL(page.url()).pathname).toBe(
        `/quiver/${ONTOLOGY}/${encodeURIComponent(rid)}/view`,
      );
    });

    await Then('the read-only view renders with the dashboard title', async () => {
      await expect(quiver.viewRoot).toBeVisible();
      await expect(quiver.viewTitle).toHaveText('Shared CPU Snapshot');
    });

    await Then('the read-only view hides the editor surfaces (save controls + picker form + remove buttons)', async () => {
      await expect(quiver.saveControls).toHaveCount(0);
      await expect(quiver.addForm).toHaveCount(0);
      // The aggregate panel mounts but has no per-row × button in
      // read-only mode (editable flag = `onRemove !== undefined`, and
      // QuiverViewPage does not pass onRemove).
      await expect(quiver.aggregatePanel).toBeVisible();
      await expect(page.getByTestId(/^quiver-remove-/)).toHaveCount(0);
    });
  });

  test('Scenario: the no-series empty state renders before any picker submission @smoke', async ({
    page,
  }) => {
    // Locks AC "空数据态": the pre-Add state branch is the "No series
    // yet" EmptyState; neither the chart panel nor the aggregate panel
    // mounts; the Add button is disabled until all three required
    // picker fields are filled. The Save button is similarly disabled
    // until a series + dashboard name combination exists (locked by
    // the existing vitest unit case; here we lock the chart/aggregate
    // absence which the unit tests do not).
    const quiver = new QuiverPage(page);
    const stubs = newStubs();

    await Given('the quiver endpoints return empty list', async () => {
      await stubQuiverEndpoints(page, stubs);
    });

    await When('the user opens the workbench page', async () => {
      await quiver.goto(ONTOLOGY);
      await expect(quiver.root).toBeVisible();
    });

    await Then('the no-series EmptyState placeholder is visible', async () => {
      await expect(page.getByText(/no series yet/i)).toBeVisible();
    });

    await Then('neither the chart panel nor the aggregate panel mounts', async () => {
      await expect(quiver.chartPanel).toHaveCount(0);
      await expect(quiver.aggregatePanel).toHaveCount(0);
    });

    await Then('the Add button is disabled until the three required fields are populated', async () => {
      await expect(quiver.addBtn).toBeDisabled();
      await quiver.inputObjectType.fill('Server');
      await expect(quiver.addBtn).toBeDisabled();
      await quiver.inputPrimaryKey.fill('s1');
      await expect(quiver.addBtn).toBeDisabled();
      await quiver.inputProperty.fill('cpu');
      await expect(quiver.addBtn).toBeEnabled();
    });

    await Then('no streamPoints fetch was issued (no series ever crossed the picker submit)', async () => {
      expect(stubs.timeseries.length).toBe(0);
    });
  });

  test('Scenario: saving a dashboard surfaces it in the saved list with share/delete affordances', async ({
    page,
  }) => {
    // Bonus scenario locking the save → list refetch lifecycle that
    // the read-only share view depends on. The save POST body carries
    // the series array with `ontologyApiName` + `series` keys, the
    // returned RID drives the saved-list refetch, and after refetch
    // the new row exposes load / share / delete buttons.
    const quiver = new QuiverPage(page);
    const stubs = newStubs();
    stubs.pointsBySlot.set('Server|s1|cpu', []);

    await Given('the quiver endpoints return empty list initially', async () => {
      await stubQuiverEndpoints(page, stubs);
    });

    await When('the user adds a Server/s1.cpu series', async () => {
      await quiver.goto(ONTOLOGY);
      await quiver.addSeries({
        objectType: 'Server',
        primaryKey: 's1',
        property: 'cpu',
      });
    });

    await When('the user names the dashboard "Demo" and clicks Save', async () => {
      await quiver.dashboardNameInput.fill('Demo');
      await expect(quiver.saveBtn).toBeEnabled();
      await quiver.saveBtn.click();
    });

    await Then('the save POST body carries the workbench config shape', async () => {
      await expect.poll(() => stubs.savedCalls.length).toBeGreaterThanOrEqual(1);
      const body = stubs.savedCalls[0]!;
      expect(body.name).toBe('Demo');
      expect(body.config.ontologyApiName).toBe(ONTOLOGY);
      expect(body.config.series).toHaveLength(1);
      expect(body.config.series[0]!.objectType).toBe('Server');
      expect(body.config.series[0]!.primaryKey).toBe('s1');
      expect(body.config.series[0]!.property).toBe('cpu');
      // First save has no existing rid → wire body must omit it.
      expect(body.rid).toBeUndefined();
    });

    await Then('the new dashboard appears in the saved list with full affordances', async () => {
      const newRid = stubs.saved[0]!.rid;
      await expect(quiver.savedDashboard(newRid)).toBeVisible();
      await expect(quiver.loadDashboardBtn(newRid)).toBeVisible();
      await expect(quiver.shareDashboardBtn(newRid)).toBeVisible();
      await expect(quiver.deleteDashboardBtn(newRid)).toBeVisible();
    });
  });
});
