import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  SagaJobsPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/actions/:ontology/jobs` — the Saga / Job
 * monitoring page rendered by
 * `src/components/sagaJobs/SagaJobsPage.tsx` (US-044, PC-A08).
 *
 * Scenarios map to the US-044 acceptance criteria:
 *
 *   - "新建路由 /actions/:ontology/jobs 列表（5 态机彩色徽标）"
 *     → list scenario seeds 3 sagas across SUCCESS / COMPENSATED /
 *       FAILED states, asserts rows render with status badges + the
 *       5-state filter tabs are present (RUNNING / SUCCESS /
 *       COMPENSATING / COMPENSATED / FAILED).
 *   - "详情页：step timeline、compensation 标记、retry 计数、DLQ 链接"
 *     → detail scenario opens the drawer on a COMPENSATED saga,
 *       asserts the step timeline renders, each compensated step
 *       carries the compensation marker, the compensation-count
 *       summary surfaces, and the "View DLQ entries" link appears
 *       when a step ended in COMPENSATION_FAILED.
 *   - "DLQ 抽屉：Replay / Discard（二次确认）"
 *     → DLQ replay scenario asserts the two-step confirm: click
 *       Replay → inline confirm banner appears → click Yes → POST to
 *       the retry endpoint → row transitions to RESOLVED on refetch.
 *     → DLQ discard scenario asserts the cancel path: click Discard →
 *       confirm banner appears → click Cancel → no DELETE/DROP POST
 *       is sent.
 *
 * Every scenario stubs the backend through `page.route` so the page
 * renders deterministic fixtures without touching real PG / NATS.
 */

const ONTOLOGY = 'northwind';

interface MockSaga {
  sagaId: string;
  ontology: string;
  status:
    | 'RUNNING'
    | 'SUCCESS'
    | 'COMPENSATING'
    | 'COMPENSATED'
    | 'FAILED';
  requestedBy?: string;
  failureMessage?: string;
  idempotencyKey?: string;
  createdAt: string;
  updatedAt: string;
}

interface MockStep {
  stepId: string;
  sagaId: string;
  stepIndex: number;
  actionType: string;
  status:
    | 'PENDING'
    | 'APPLIED'
    | 'FAILED'
    | 'COMPENSATED'
    | 'COMPENSATION_FAILED';
  editsJson?: unknown;
  inverseEditsJson?: unknown;
  parameters?: unknown;
  createdAt: string;
  updatedAt: string;
}

interface MockDLQ {
  dlqId: string;
  sagaId: string;
  stepId: string;
  ontology: string;
  failureMessage?: string;
  status: 'PENDING' | 'RESOLVED' | 'DROPPED';
  attempts: number;
  editsJson?: unknown;
  createdAt: string;
  updatedAt: string;
}

interface CapturedRequest {
  url: string;
  method: string;
  body: unknown;
}

interface Stubs {
  sagas: MockSaga[];
  stepsBySagaId: Record<string, MockStep[]>;
  dlq: MockDLQ[];
  listGets: CapturedRequest[];
  detailGets: CapturedRequest[];
  dlqListGets: CapturedRequest[];
  retries: CapturedRequest[];
  drops: CapturedRequest[];
}

function newStubs(): Stubs {
  return {
    sagas: [],
    stepsBySagaId: {},
    dlq: [],
    listGets: [],
    detailGets: [],
    dlqListGets: [],
    retries: [],
    drops: [],
  };
}

function sagaFixture(overrides: Partial<MockSaga>): MockSaga {
  return {
    sagaId: overrides.sagaId ?? 'saga-1',
    ontology: ONTOLOGY,
    status: 'SUCCESS',
    requestedBy: 'alice@test',
    createdAt: '2026-05-13T00:00:00Z',
    updatedAt: '2026-05-13T00:00:00Z',
    ...overrides,
  };
}

function stepFixture(overrides: Partial<MockStep>): MockStep {
  return {
    stepId: overrides.stepId ?? 'step-1',
    sagaId: overrides.sagaId ?? 'saga-1',
    stepIndex: overrides.stepIndex ?? 0,
    actionType: 'ri.action.createOrder',
    status: 'APPLIED',
    createdAt: '2026-05-13T00:00:01Z',
    updatedAt: '2026-05-13T00:00:01Z',
    ...overrides,
  };
}

function dlqFixture(overrides: Partial<MockDLQ>): MockDLQ {
  return {
    dlqId: overrides.dlqId ?? 'dlq-1',
    sagaId: overrides.sagaId ?? 'saga-1',
    stepId: overrides.stepId ?? 'step-1',
    ontology: ONTOLOGY,
    status: 'PENDING',
    attempts: 0,
    failureMessage: 'compensator publish failed',
    createdAt: '2026-05-13T00:01:00Z',
    updatedAt: '2026-05-13T00:01:00Z',
    ...overrides,
  };
}

async function stubEndpoints(page: Page, stubs: Stubs): Promise<void> {
  const PREFIX = `**/api/v2/ontologies/${ONTOLOGY}/actions`;

  // DLQ list + retry + drop. Register the more-specific patterns FIRST
  // so Playwright's LIFO route resolution prefers them over the
  // catch-all (per US-023 / US-040 codebase note).
  await page.route(`${PREFIX}/saga/dlq/*/retry`, async (route: Route) => {
    const url = route.request().url();
    stubs.retries.push({ url, method: route.request().method(), body: null });
    const m = url.match(/\/saga\/dlq\/([^/?#]+)\/retry/);
    const dlqId = m?.[1] ?? '';
    const idx = stubs.dlq.findIndex((d) => d.dlqId === dlqId);
    if (idx !== -1) {
      stubs.dlq[idx] = { ...stubs.dlq[idx], status: 'RESOLVED' };
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ dlqId, status: 'RESOLVED' }),
    });
  });

  await page.route(`${PREFIX}/saga/dlq/*/drop`, async (route: Route) => {
    const url = route.request().url();
    stubs.drops.push({ url, method: route.request().method(), body: null });
    const m = url.match(/\/saga\/dlq\/([^/?#]+)\/drop/);
    const dlqId = m?.[1] ?? '';
    const idx = stubs.dlq.findIndex((d) => d.dlqId === dlqId);
    if (idx !== -1) {
      stubs.dlq[idx] = { ...stubs.dlq[idx], status: 'DROPPED' };
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ dlqId, status: 'DROPPED' }),
    });
  });

  await page.route(`${PREFIX}/saga/dlq*`, async (route: Route) => {
    const url = route.request().url();
    stubs.dlqListGets.push({
      url,
      method: route.request().method(),
      body: null,
    });
    const u = new URL(url);
    const status = u.searchParams.get('status') ?? 'PENDING';
    const entries = stubs.dlq.filter((d) => d.status === status);
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ entries }),
    });
  });

  // Saga detail (GET /actions/sagas/{sagaId}).
  await page.route(`${PREFIX}/sagas/*`, async (route: Route) => {
    const url = route.request().url();
    stubs.detailGets.push({
      url,
      method: route.request().method(),
      body: null,
    });
    const pathOnly = url.split('?')[0];
    const m = pathOnly.match(/\/sagas\/([^/?#]+)$/);
    const sagaId = m?.[1] ?? '';
    const saga = stubs.sagas.find((s) => s.sagaId === sagaId);
    if (!saga) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          errorCode: 'NOT_FOUND',
          errorName: 'SagaNotFound',
          errorInstanceId: 'spec',
          parameters: { sagaId },
        }),
      });
      return;
    }
    const steps = stubs.stepsBySagaId[sagaId] ?? [];
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ saga, steps }),
    });
  });

  // Saga list (GET /actions/sagas). Catch-all for the /sagas* path —
  // matches both the bare /sagas list endpoint and /sagas?status=...
  // (the trailing `*` excludes /sagas/<id>, which the precise pattern
  // above already handled).
  await page.route(`${PREFIX}/sagas*`, async (route: Route) => {
    const url = route.request().url();
    stubs.listGets.push({
      url,
      method: route.request().method(),
      body: null,
    });
    const u = new URL(url);
    const status = u.searchParams.get('status');
    let rows = stubs.sagas;
    if (status) {
      rows = rows.filter((s) => s.status === status);
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: rows }),
    });
  });
}

describeFeature('Saga / Job monitoring page', () => {
  test('Scenario: renders saga rows with status badges and the 5-state filter tabs @smoke', async ({
    page,
    request,
  }) => {
    // AC: "新建路由 /actions/:ontology/jobs 列表（5 态机彩色徽标）".
    // Seed sagas across SUCCESS / COMPENSATED / FAILED so multiple
    // colored badges actually render. Then assert the four
    // additional filter tabs (RUNNING + COMPENSATING + ALL) are also
    // present — together with the three rendered ones that proves
    // the 5-state machine is exposed in the UI.
    const stubs = newStubs();
    stubs.sagas = [
      sagaFixture({
        sagaId: 'saga-success',
        status: 'SUCCESS',
        requestedBy: 'alice',
      }),
      sagaFixture({
        sagaId: 'saga-compensated',
        status: 'COMPENSATED',
        requestedBy: 'bob',
        failureMessage: 'step B prepare failed',
      }),
      sagaFixture({
        sagaId: 'saga-failed',
        status: 'FAILED',
        requestedBy: 'carol',
        failureMessage: 'NATS publish failure',
      }),
    ];
    const sagaPage = new SagaJobsPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('three sagas across mixed states exist', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens /actions/northwind/jobs', async () => {
      await sagaPage.goto(ONTOLOGY);
      await expect(sagaPage.root).toBeVisible();
    });

    await Then('the saga list renders all three rows', async () => {
      await expect(sagaPage.list).toBeVisible();
      await expect(sagaPage.rowBySagaId('saga-success')).toBeVisible();
      await expect(sagaPage.rowBySagaId('saga-compensated')).toBeVisible();
      await expect(sagaPage.rowBySagaId('saga-failed')).toBeVisible();
    });

    await Then('each row exposes its status badge', async () => {
      await expect(sagaPage.rowStatusBadge('saga-success')).toContainText(
        'SUCCESS',
      );
      await expect(sagaPage.rowStatusBadge('saga-compensated')).toContainText(
        'COMPENSATED',
      );
      await expect(sagaPage.rowStatusBadge('saga-failed')).toContainText(
        'FAILED',
      );
    });

    await Then('all five state filter tabs are present', async () => {
      // ALL + 5 lifecycle states = 6 total. The five state tabs prove
      // the 5-state machine surface lock.
      await expect(sagaPage.filterTab('ALL')).toBeVisible();
      await expect(sagaPage.filterTab('RUNNING')).toBeVisible();
      await expect(sagaPage.filterTab('SUCCESS')).toBeVisible();
      await expect(sagaPage.filterTab('COMPENSATING')).toBeVisible();
      await expect(sagaPage.filterTab('COMPENSATED')).toBeVisible();
      await expect(sagaPage.filterTab('FAILED')).toBeVisible();
    });

    await When(
      'the user clicks the COMPENSATED filter tab',
      async () => {
        await sagaPage.filterTab('COMPENSATED').click();
      },
    );

    await Then(
      'a GET to the list endpoint with status=COMPENSATED is captured',
      async () => {
        await expect
          .poll(() => {
            const last = stubs.listGets.at(-1);
            if (!last) return null;
            return new URL(last.url).searchParams.get('status');
          })
          .toBe('COMPENSATED');
      },
    );

    await Then(
      'only the saga-compensated row remains visible',
      async () => {
        await expect(sagaPage.rowBySagaId('saga-compensated')).toBeVisible();
        await expect(sagaPage.rowBySagaId('saga-success')).toHaveCount(0);
        await expect(sagaPage.rowBySagaId('saga-failed')).toHaveCount(0);
      },
    );
  });

  test('Scenario: inspecting a compensated saga renders the step timeline with compensation markers and the DLQ link', async ({
    page,
  }) => {
    // AC: "详情页：step timeline、compensation 标记、retry 计数、
    // DLQ 链接". Seed a compensated saga with three steps:
    //   - step-1: APPLIED then COMPENSATED happily
    //   - step-2: FAILED (this is the trigger step)
    //   - step-3: PENDING (never prepared because step-2 failed)
    // Plus one step that ended in COMPENSATION_FAILED so the DLQ
    // link surfaces.
    const stubs = newStubs();
    stubs.sagas = [
      sagaFixture({
        sagaId: 'saga-detail',
        status: 'COMPENSATED',
        requestedBy: 'alice',
        failureMessage: 'step B prepare failed',
        idempotencyKey: 'order-saga-42',
      }),
    ];
    stubs.stepsBySagaId['saga-detail'] = [
      stepFixture({
        stepId: 'step-1',
        sagaId: 'saga-detail',
        stepIndex: 0,
        actionType: 'ri.action.createOrder',
        status: 'COMPENSATED',
        editsJson: [{ kind: 'CREATE', objectType: 'Order' }],
        inverseEditsJson: [{ kind: 'DELETE', objectType: 'Order' }],
      }),
      stepFixture({
        stepId: 'step-2',
        sagaId: 'saga-detail',
        stepIndex: 1,
        actionType: 'ri.action.bookResource',
        status: 'COMPENSATION_FAILED',
        editsJson: [{ kind: 'CREATE', objectType: 'Reservation' }],
      }),
      stepFixture({
        stepId: 'step-3',
        sagaId: 'saga-detail',
        stepIndex: 2,
        actionType: 'ri.action.sendReceipt',
        status: 'PENDING',
      }),
    ];
    const sagaPage = new SagaJobsPage(page);

    await Given(
      'one compensated saga with a mixed-status step timeline exists',
      async () => {
        await stubEndpoints(page, stubs);
      },
    );

    await When('the user opens the page', async () => {
      await sagaPage.goto(ONTOLOGY);
      await expect(sagaPage.root).toBeVisible();
    });

    await When(
      'the user clicks Inspect on the compensated saga row',
      async () => {
        await sagaPage.inspectButton('saga-detail').click();
        await expect(sagaPage.detailDrawer).toBeVisible();
      },
    );

    await Then(
      'a GET to the detail endpoint for the saga is captured',
      async () => {
        await expect
          .poll(() => stubs.detailGets.length)
          .toBeGreaterThanOrEqual(1);
        const last = stubs.detailGets.at(-1)!;
        expect(last.method).toBe('GET');
        expect(last.url).toMatch(/\/sagas\/saga-detail(?:$|\?)/);
      },
    );

    await Then(
      'the header surfaces the COMPENSATED status and compensation count',
      async () => {
        await expect(sagaPage.detailStatusBadge()).toContainText('COMPENSATED');
        // 2 of 3 steps were compensated (step-1 happily, step-2 into DLQ).
        await expect(sagaPage.detailCompensationCount()).toHaveAttribute(
          'data-count',
          '2',
        );
      },
    );

    await Then('the step timeline renders all three steps', async () => {
      await expect(sagaPage.timeline()).toBeVisible();
      await expect(sagaPage.stepRows()).toHaveCount(3);
    });

    await Then(
      'the compensated step carries the compensation marker',
      async () => {
        const step1 = sagaPage.stepRowByStepId('step-1');
        await expect(step1).toHaveAttribute(
          'data-step-status',
          'COMPENSATED',
        );
        await expect(step1).toHaveAttribute('data-step-compensated', 'true');
        await expect(
          step1.getByTestId('saga-step-compensation-marker'),
        ).toBeVisible();
      },
    );

    await Then(
      'the COMPENSATION_FAILED step also carries the marker',
      async () => {
        const step2 = sagaPage.stepRowByStepId('step-2');
        await expect(step2).toHaveAttribute(
          'data-step-status',
          'COMPENSATION_FAILED',
        );
        await expect(step2).toHaveAttribute('data-step-compensated', 'true');
      },
    );

    await Then(
      'the PENDING step is NOT marked as compensated',
      async () => {
        const step3 = sagaPage.stepRowByStepId('step-3');
        await expect(step3).toHaveAttribute(
          'data-step-status',
          'PENDING',
        );
        await expect(step3).toHaveAttribute('data-step-compensated', 'false');
      },
    );

    await Then(
      'the DLQ link surfaces because at least one step ended in COMPENSATION_FAILED',
      async () => {
        await expect(sagaPage.detailDLQLink()).toBeVisible();
      },
    );
  });

  test('Scenario: replaying a DLQ entry requires a second confirmation and hits the retry endpoint @smoke', async ({
    page,
  }) => {
    // AC: "DLQ 抽屉：Replay / Discard（二次确认）".
    // Click Replay → inline confirm banner appears → click Yes →
    // POST to the retry endpoint is captured → row re-renders as
    // RESOLVED on the refetch. The two-step gate prevents accidental
    // operator action on un-investigated DLQ rows.
    const stubs = newStubs();
    stubs.dlq = [
      dlqFixture({
        dlqId: 'dlq-1',
        sagaId: 'saga-detail',
        stepId: 'step-2',
        status: 'PENDING',
        attempts: 0,
        failureMessage: 'compensator publish failed',
      }),
    ];
    const sagaPage = new SagaJobsPage(page);

    await Given('one PENDING DLQ entry exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When(
      'the user opens the page and opens the DLQ drawer',
      async () => {
        await sagaPage.goto(ONTOLOGY);
        await expect(sagaPage.root).toBeVisible();
        await sagaPage.openDLQButton.click();
        await expect(sagaPage.dlqDrawer).toBeVisible();
      },
    );

    await Then('the DLQ row is visible', async () => {
      await expect(sagaPage.dlqRowByDlqId('dlq-1')).toBeVisible();
    });

    await When('the user clicks Replay on the row', async () => {
      await sagaPage.dlqReplayButton('dlq-1').click();
    });

    await Then(
      'an inline confirmation banner appears with the retry phrasing',
      async () => {
        await expect(sagaPage.dlqConfirm()).toBeVisible();
        await expect(sagaPage.dlqConfirm()).toHaveAttribute(
          'data-confirm-kind',
          'retry',
        );
      },
    );

    await Then(
      'no POST to the retry endpoint has fired yet',
      async () => {
        expect(stubs.retries.length).toBe(0);
      },
    );

    await When('the user confirms the replay', async () => {
      await sagaPage.dlqConfirmYes().click();
    });

    await Then(
      'a POST to the retry endpoint is captured',
      async () => {
        await expect
          .poll(() => stubs.retries.length)
          .toBeGreaterThanOrEqual(1);
        const last = stubs.retries.at(-1)!;
        expect(last.method).toBe('POST');
        expect(last.url).toMatch(/\/saga\/dlq\/dlq-1\/retry$/);
      },
    );

    await Then(
      'the row re-renders as RESOLVED after invalidation',
      async () => {
        // The retry handler flipped status to RESOLVED + the React
        // Query invalidate refetches the PENDING filter view, which
        // now returns 0 entries.
        await expect(sagaPage.dlqRowByDlqId('dlq-1')).toHaveCount(0);
        await expect(page.getByTestId('saga-dlq-drawer-empty')).toBeVisible();
      },
    );

    await Then('no discard POST was made', async () => {
      expect(stubs.drops.length).toBe(0);
    });
  });

  test('Scenario: cancelling the discard confirmation leaves the DLQ row untouched', async ({
    page,
  }) => {
    // AC: "DLQ 抽屉：Replay / Discard（二次确认）" — the cancel arm
    // of the two-step gate. Confirms the operator can back out of a
    // destructive Discard without firing the network call.
    const stubs = newStubs();
    stubs.dlq = [
      dlqFixture({
        dlqId: 'dlq-2',
        sagaId: 'saga-detail',
        stepId: 'step-2',
        status: 'PENDING',
        attempts: 0,
      }),
    ];
    const sagaPage = new SagaJobsPage(page);

    await Given('one PENDING DLQ entry exists', async () => {
      await stubEndpoints(page, stubs);
    });

    await When(
      'the user opens the page and opens the DLQ drawer',
      async () => {
        await sagaPage.goto(ONTOLOGY);
        await expect(sagaPage.root).toBeVisible();
        await sagaPage.openDLQButton.click();
        await expect(sagaPage.dlqDrawer).toBeVisible();
      },
    );

    await When('the user clicks Discard', async () => {
      await sagaPage.dlqDiscardButton('dlq-2').click();
    });

    await Then(
      'an inline confirmation banner appears with the discard phrasing',
      async () => {
        await expect(sagaPage.dlqConfirm()).toBeVisible();
        await expect(sagaPage.dlqConfirm()).toHaveAttribute(
          'data-confirm-kind',
          'discard',
        );
      },
    );

    await When('the user clicks Cancel on the confirmation', async () => {
      await sagaPage.dlqConfirmNo().click();
    });

    await Then(
      'the confirmation closes and no drop POST is captured',
      async () => {
        await expect(sagaPage.dlqConfirm()).toHaveCount(0);
        expect(stubs.drops.length).toBe(0);
      },
    );

    await Then('the row is still visible as PENDING', async () => {
      await expect(sagaPage.dlqRowByDlqId('dlq-2')).toBeVisible();
      await expect(sagaPage.dlqRowByDlqId('dlq-2')).toHaveAttribute(
        'data-dlq-status',
        'PENDING',
      );
    });
  });
});
