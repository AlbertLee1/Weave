import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  ImportWizardPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/import/:ontology` — the CSV Import Wizard rendered
 * by `src/components/import/ImportWizardPage.tsx`.
 *
 * Scenarios map the PRD AC for US-036 (新的 Playwright BDD: feature.import-wizard.spec.ts):
 *   AC: "至少 4 scenarios：CSV 上传 4 步向导、schema inference 预览、
 *        字段映射修改、错误回滚"
 *
 * Honest mapping (mirroring US-024/026/028/033 conventions):
 *   - "CSV 上传 4 步向导" → @smoke happy path: walks step 1→4, asserts
 *     each `data-state` flip on the step indicator + parse-summary text
 *     + auto-mapping of all three CSV columns + final apply success
 *     counts. Captures the wire-shape of each POST body so a future PR
 *     that drops parameters from the action apply payload (or accidentally
 *     stops percent-encoding the action name) breaks this scenario.
 *   - "schema inference 预览" → schema inference is performed by
 *     `validateCell(raw, baseType)` against the selected ObjectType's
 *     property `dataType.type`. The preview surface (step 3) renders a
 *     yellow warning badge for every cell whose convert-attempt would
 *     fail. We seed a CSV with a non-numeric `age` value plus an empty
 *     row to lock both the "bad integer" + "empty cell stays valid"
 *     branches of `convertCellValue`.
 *   - "字段映射修改" → user edits an auto-mapped column to '— skip —'.
 *     Locks: (a) `activeMappings` drops the unmapped header so the
 *     step-3 preview hides the column, (b) `buildParameters` omits the
 *     dropped property from the apply body. The wire-level absence
 *     mirrors the US-030 narrow-DTO negative-assertion pattern.
 *   - "错误回滚" → apply returns 400 for the first row, 200 for the
 *     second. Locks: (a) per-row error isolation — the second row still
 *     applies (no rollback of the whole import on per-row failure), (b)
 *     `failure-summary` lists the failed row + index, (c) per-row apply
 *     calls increment both `processed-count` and either `success-count`
 *     or `failure-count` deterministically. Mirrors the US-026 "modal
 *     stays + error surface + partial state" three-pillar template.
 *
 * Every scenario stubs the three endpoints the page hits:
 *   - GET /api/v2/ontologies/{ont}/objectTypes for `useObjectTypes`.
 *   - GET /api/v2/ontologies/{ont}/actionTypes for `useActionTypes`.
 *   - POST /api/v2/ontologies/{ont}/actions/{action}/apply for
 *     `applyAction`. Captures the body + action segment on every call.
 */

const ONTOLOGY = 'northwind';

interface ApplyCall {
  action: string;
  body: { parameters: Record<string, unknown>; options?: unknown };
}

interface StubOptions {
  /**
   * Index (0-based) of an apply call that should be rejected with HTTP 400.
   * Subsequent calls within the same import run succeed normally. Default:
   * undefined → all calls succeed.
   */
  failApplyAt?: number;
  /**
   * Override the ActionType list (defaults to a single createEmployee
   * with `createObject` rule on Employee). Pass [] to simulate the
   * "no create action defined" gap, though that scenario is intentionally
   * not part of US-036 — the four required scenarios use the default.
   */
  actionTypes?: unknown[];
}

const DEFAULT_OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp',
    apiName: 'Employee',
    displayName: 'Employee',
    primaryKey: 'id',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    properties: {
      id: { dataType: { type: 'string' }, rid: 'ri.prop.id' },
      name: { dataType: { type: 'string' }, rid: 'ri.prop.name' },
      age: { dataType: { type: 'integer' }, rid: 'ri.prop.age' },
    },
  },
];

const DEFAULT_ACTION_TYPES = [
  {
    rid: 'ri.action.create-emp',
    apiName: 'createEmployee',
    displayName: 'Create Employee',
    status: 'ACTIVE',
    parameters: {
      id: { dataType: { type: 'string' }, required: true },
      name: { dataType: { type: 'string' }, required: true },
      age: { dataType: { type: 'integer' }, required: false },
    },
    rules: [
      {
        type: 'createObject',
        objectType: 'Employee',
        propertyBindings: {
          id: { type: 'parameter', value: 'id' },
          name: { type: 'parameter', value: 'name' },
          age: { type: 'parameter', value: 'age' },
        },
      },
    ],
  },
];

async function stubImportEndpoints(
  page: Page,
  captured: ApplyCall[],
  opts: StubOptions = {},
): Promise<void> {
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      // The bare GET path returns the list-wrapper {data:[]}; ignore
      // any query string (withActiveBranch may append `?branch=...`).
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: DEFAULT_OBJECT_TYPES }),
      });
    },
  );

  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actionTypes`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: opts.actionTypes ?? DEFAULT_ACTION_TYPES }),
      });
    },
  );

  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/actions/*/apply`,
    async (route: Route) => {
      if (route.request().method() !== 'POST') {
        await route.continue();
        return;
      }
      const url = new URL(route.request().url());
      // /api/v2/ontologies/northwind/actions/{action}/apply
      const parts = url.pathname.split('/');
      const actionApiName = decodeURIComponent(parts[parts.length - 2] ?? '');
      const body = (route.request().postDataJSON() ?? {}) as ApplyCall['body'];
      const ordinal = captured.length;
      captured.push({ action: actionApiName, body });

      if (opts.failApplyAt !== undefined && opts.failApplyAt === ordinal) {
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'ApplyFailed',
            errorName: 'ActionApplyFailed',
            errorInstanceId: 'spec-stub',
            parameters: { error: 'simulated per-row failure' },
          }),
        });
        return;
      }

      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ edits: { type: 'edits', edits: [] } }),
      });
    },
  );
}

describeFeature('Import Wizard page', () => {
  test('Scenario: CSV upload walks the four-step wizard and applies every row @smoke', async ({
    page,
    request,
  }) => {
    // Locks the AC "CSV 上传 4 步向导" happy path end-to-end: parse →
    // map → preview → import. We assert the `data-state` on each
    // `step-{n}` badge flips through pending → active → done, the
    // per-row apply POST body shape is wire-correct, and the
    // success-count converges to the row total. Captures every body so
    // a future PR that drops `parameters` (or accidentally re-enables
    // a deprecated `actionType` field) breaks the scenario.
    const wizard = new ImportWizardPage(page);
    const captured: ApplyCall[] = [];

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the import endpoints are stubbed for happy path', async () => {
      await stubImportEndpoints(page, captured);
    });

    await When('the user opens /import/northwind', async () => {
      await wizard.goto(ONTOLOGY);
      await expect(wizard.heading).toContainText(/Import CSV Data/i);
      await expect(wizard.stepIndicator).toBeVisible();
    });

    await Then('step 1 is active and steps 2-4 are pending', async () => {
      await expect(wizard.stepBadge(1)).toHaveAttribute('data-state', 'active');
      await expect(wizard.stepBadge(2)).toHaveAttribute('data-state', 'pending');
      await expect(wizard.stepBadge(3)).toHaveAttribute('data-state', 'pending');
      await expect(wizard.stepBadge(4)).toHaveAttribute('data-state', 'pending');
      await expect(wizard.next2Btn).toBeDisabled();
    });

    await When('the user uploads a 2-row CSV', async () => {
      await wizard.uploadCsv(
        'employees.csv',
        'id,name,age\r\n1,Alice,30\r\n2,Bob,25',
      );
    });

    await Then('the parse summary reports 2 rows and 3 columns', async () => {
      await expect(wizard.fileName).toHaveText('employees.csv');
      await expect(wizard.parseSummary).toContainText(/Parsed 2 rows, 3 columns/);
      await expect(wizard.next2Btn).toBeEnabled();
    });

    await When('the user advances to step 2 and picks the Employee object type', async () => {
      await wizard.next2Btn.click();
      await expect(wizard.stepBadge(2)).toHaveAttribute('data-state', 'active');
      await expect(wizard.stepBadge(1)).toHaveAttribute('data-state', 'done');
      await expect(wizard.objectTypeSelect).toBeVisible();
      await wizard.objectTypeSelect.selectOption('Employee');
    });

    await Then('all three CSV columns are auto-mapped to matching properties', async () => {
      await expect(wizard.mappingSelect('id')).toHaveValue('id');
      await expect(wizard.mappingSelect('name')).toHaveValue('name');
      await expect(wizard.mappingSelect('age')).toHaveValue('age');
      await expect(wizard.next3Btn).toBeEnabled();
    });

    await When('the user advances to the preview step', async () => {
      await wizard.next3Btn.click();
      await expect(wizard.stepBadge(3)).toHaveAttribute('data-state', 'active');
    });

    await Then('the preview hides the no-create-action banner because the action resolves', async () => {
      await expect(wizard.noCreateAction).toHaveCount(0);
      // No warning badges for the all-clean CSV.
      await expect(wizard.warningBadge(0, 'age')).toHaveCount(0);
      await expect(wizard.warningBadge(1, 'age')).toHaveCount(0);
    });

    await When('the user advances to step 4 and clicks Start Import', async () => {
      await wizard.next4Btn.click();
      await expect(wizard.stepBadge(4)).toHaveAttribute('data-state', 'active');
      await expect(wizard.rowCount).toHaveText('2');
      await wizard.startImport.click();
    });

    await Then('processed/success counts converge to 2 and no failures are reported', async () => {
      await expect(wizard.processedCount).toHaveText('2', { timeout: 5_000 });
      await expect(wizard.successCount).toHaveText('2');
      await expect(wizard.failureCount).toHaveText('0');
      await expect(wizard.failureSummary).toHaveCount(0);
    });

    await Then('the captured apply calls carry wire-correct parameter shapes', async () => {
      expect(captured).toHaveLength(2);
      expect(captured[0]!.action).toBe('createEmployee');
      expect(captured[0]!.body).toEqual({
        parameters: { id: '1', name: 'Alice', age: 30 },
      });
      expect(captured[1]!.body).toEqual({
        parameters: { id: '2', name: 'Bob', age: 25 },
      });
      // Wire shape stays narrow: no stray `actionType` / `bypassValidation`
      // leaks from the form into the body.
      for (const call of captured) {
        expect(
          Object.prototype.hasOwnProperty.call(call.body, 'actionType'),
        ).toBe(false);
      }
    });
  });

  test('Scenario: schema inference flags type-mismatched cells with a warning badge', async ({
    page,
  }) => {
    // Locks AC "schema inference 预览": the step-3 preview runs
    // `validateCell(raw, baseType)` per cell using the selected
    // ObjectType's property dataTypes. We seed a CSV whose `age` column
    // contains a non-numeric value plus an empty value to cover both
    // sides of `convertCellValue`: the bad integer path (returns
    // `{error: ...}` → warning rendered) and the empty path (returns
    // `{value: null}` → no warning, even on an integer column).
    const wizard = new ImportWizardPage(page);
    const captured: ApplyCall[] = [];

    await Given('the import endpoints are stubbed for happy path', async () => {
      await stubImportEndpoints(page, captured);
    });

    await When('the user uploads a CSV with one bad integer and one empty age', async () => {
      await wizard.goto(ONTOLOGY);
      await wizard.uploadCsv(
        'inferred.csv',
        'id,name,age\r\n1,Alice,not-a-number\r\n2,Bob,\r\n3,Carol,42',
      );
      await expect(wizard.parseSummary).toContainText(/3 rows, 3 columns/);
    });

    await When('the user walks step 2 with auto-mapping on Employee', async () => {
      await wizard.next2Btn.click();
      await wizard.objectTypeSelect.selectOption('Employee');
      await expect(wizard.mappingSelect('age')).toHaveValue('age');
      await wizard.next3Btn.click();
      await expect(wizard.stepBadge(3)).toHaveAttribute('data-state', 'active');
    });

    await Then('the preview surfaces a warning badge only on the bad integer cell', async () => {
      // Row 0 (Alice / "not-a-number") → warning visible with the
      // baseType-specific message produced by convertCellValue.
      await expect(wizard.warningBadge(0, 'age')).toBeVisible();
      await expect(wizard.warningBadge(0, 'age')).toContainText(/not a valid integer/i);

      // Row 1 (Bob / empty) → no warning because empty cells are
      // treated as null, not as a coercion failure.
      await expect(wizard.warningBadge(1, 'age')).toHaveCount(0);

      // Row 2 (Carol / "42") → clean integer, no warning.
      await expect(wizard.warningBadge(2, 'age')).toHaveCount(0);

      // String columns never raise type warnings even when populated
      // with arbitrary characters (locks "schema inference scoped to
      // type-aware properties, not pass-through strings").
      await expect(wizard.warningBadge(0, 'id')).toHaveCount(0);
      await expect(wizard.warningBadge(0, 'name')).toHaveCount(0);
    });
  });

  test('Scenario: user can override an auto-mapping to "skip" a column from the apply body', async ({
    page,
  }) => {
    // Locks AC "字段映射修改": the user explicitly unmaps a CSV column
    // by switching its select to "— skip —" (value=""). The wizard's
    // `activeMappings` filter drops the entry, the step-3 preview no
    // longer renders the column header, and most importantly the
    // step-4 apply body omits the property — same narrow-DTO contract
    // pattern as US-030's negative-assertion (no leaked fields wired
    // through despite the column being present in the parsed CSV).
    const wizard = new ImportWizardPage(page);
    const captured: ApplyCall[] = [];

    await Given('the import endpoints are stubbed for happy path', async () => {
      await stubImportEndpoints(page, captured);
    });

    await When('the user uploads a CSV and lands on the mapping step', async () => {
      await wizard.goto(ONTOLOGY);
      await wizard.uploadCsv(
        'mapping.csv',
        'id,name,age\r\n1,Alice,30',
      );
      await wizard.next2Btn.click();
      await wizard.objectTypeSelect.selectOption('Employee');
    });

    await Then('the auto-mapping fills all three columns', async () => {
      await expect(wizard.mappingSelect('id')).toHaveValue('id');
      await expect(wizard.mappingSelect('name')).toHaveValue('name');
      await expect(wizard.mappingSelect('age')).toHaveValue('age');
    });

    await When('the user unmaps the "name" column', async () => {
      // value="" is the "— skip —" option in production.
      await wizard.mappingSelect('name').selectOption('');
    });

    await Then('the unmapped column is recorded on the select', async () => {
      await expect(wizard.mappingSelect('name')).toHaveValue('');
      await expect(wizard.next3Btn).toBeEnabled();
    });

    await When('the user walks through preview and starts the import', async () => {
      await wizard.next3Btn.click();
      // The preview header for `name` should not be rendered because
      // its column is no longer in activeMappings; lock that contract
      // by asserting the `<th>` text count is 0 in the preview table.
      const previewHeaders = page.getByRole('columnheader');
      await expect(previewHeaders.filter({ hasText: /^name\b/ })).toHaveCount(0);
      await wizard.next4Btn.click();
      await wizard.startImport.click();
    });

    await Then('the apply body omits the skipped property', async () => {
      await expect.poll(() => captured.length).toBe(1);
      const body = captured[0]!.body;
      expect(body.parameters).toEqual({ id: '1', age: 30 });
      // Negative assertion locks the wire-shape narrowness: the
      // `name` key MUST NOT have leaked through.
      expect(
        Object.prototype.hasOwnProperty.call(body.parameters, 'name'),
      ).toBe(false);
    });
  });

  test('Scenario: per-row apply failure surfaces in the failure summary without halting subsequent rows', async ({
    page,
  }) => {
    // Locks AC "错误回滚": the wizard treats each row as an independent
    // unit of work. When row 0 returns 400, the failure-summary lists
    // it but row 1 still applies (no all-or-nothing rollback at the
    // wizard layer — that semantic belongs to the server-side
    // applyBatch path, which the wizard doesn't use). We assert:
    //   - failure-count → 1, success-count → 1
    //   - failure-summary contains "Row 1" + the error message
    //   - processed-count → 2 (the loop never short-circuits)
    //   - reset button surfaces post-import for re-running.
    const wizard = new ImportWizardPage(page);
    const captured: ApplyCall[] = [];

    await Given('the apply endpoint rejects only the first row', async () => {
      await stubImportEndpoints(page, captured, { failApplyAt: 0 });
    });

    await When('the user walks the whole wizard with a 2-row CSV', async () => {
      await wizard.goto(ONTOLOGY);
      await wizard.uploadCsv(
        'partial.csv',
        'id,name,age\r\n1,Alice,30\r\n2,Bob,25',
      );
      await wizard.next2Btn.click();
      await wizard.objectTypeSelect.selectOption('Employee');
      await wizard.next3Btn.click();
      await wizard.next4Btn.click();
      await wizard.startImport.click();
    });

    await Then('the wizard reports one success and one failure', async () => {
      await expect(wizard.processedCount).toHaveText('2', { timeout: 5_000 });
      await expect(wizard.successCount).toHaveText('1');
      await expect(wizard.failureCount).toHaveText('1');
    });

    await Then('the failure summary lists row 1 with the surfaced error message', async () => {
      await expect(wizard.failureSummary).toBeVisible();
      await expect(wizard.failureSummary).toContainText(/Row 1/);
      // The page renders the message from the thrown ApiRequestError,
      // whose `Error.message` is `${errorCode}: ${errorName}` — assert
      // on the errorName which is the human-readable half.
      await expect(wizard.failureSummary).toContainText(/ActionApplyFailed/);
    });

    await Then('the reset button surfaces so the user can re-run the import', async () => {
      await expect(wizard.resetBtn).toBeVisible();
    });

    await Then('both rows were attempted in order (no early abort)', async () => {
      expect(captured).toHaveLength(2);
      expect(captured[0]!.body.parameters).toMatchObject({ id: '1' });
      expect(captured[1]!.body.parameters).toMatchObject({ id: '2' });
    });
  });
});
