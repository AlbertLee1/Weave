import { describe, expect, it } from 'vitest';
import ciWorkflowSource from '../../../../.github/workflows/ci.yml?raw';
import auditLogAdminSpecSource from '../../../e2e/phase7/audit-log-admin.spec.ts?raw';
import browserRealtimeModeSpecSource from '../../../e2e/phase7/browser-realtime-mode.spec.ts?raw';
import policyColumnHidingSpecSource from '../../../e2e/phase7/policy-column-hiding.spec.ts?raw';
import policyRowFilterSpecSource from '../../../e2e/phase7/policy-row-filter.spec.ts?raw';
import sseReconnectSpecSource from '../../../e2e/phase7/sse-reconnect.spec.ts?raw';
import phase6AggregationSpecSource from '../../../e2e/phase6/aggregation-multi-groupby.spec.ts?raw';
import phase6InterfacePagingSpecSource from '../../../e2e/phase6/interface-multitype-paging.spec.ts?raw';
import optimisticConcurrencySpecSource from '../../../e2e/phase6/optimistic-concurrency.spec.ts?raw';
import phase6WithPropertiesSpecSource from '../../../e2e/phase6/withproperties-derived.spec.ts?raw';
import us444BrowseSpecSource from '../../../e2e/us444/02-browse.spec.ts?raw';
import us444AggregateSpecSource from '../../../e2e/us444/03-aggregate.spec.ts?raw';
import us444ActionSpecSource from '../../../e2e/us444/04-action.spec.ts?raw';
import us444AppBuilderSpecSource from '../../../e2e/us444/08-app-builder.spec.ts?raw';
import us444BranchSpecSource from '../../../e2e/us444/06-branch.spec.ts?raw';
import us444MergeSpecSource from '../../../e2e/us444/07-merge.spec.ts?raw';
import us444SagaSpecSource from '../../../e2e/us444/05-saga.spec.ts?raw';
import us444MarketplaceSpecSource from '../../../e2e/us444/10-marketplace.spec.ts?raw';
import us444PackageInstallSpecSource from '../../../e2e/us444/11-pkg-install.spec.ts?raw';
import us444QuiverSpecSource from '../../../e2e/us444/09-quiver.spec.ts?raw';
import us444LineageSpecSource from '../../../e2e/us444/13-lineage-view.spec.ts?raw';
import us444PitrSpecSource from '../../../e2e/us444/14-pitr.spec.ts?raw';
import us444RoleMgmtSpecSource from '../../../e2e/us444/15-role-mgmt.spec.ts?raw';
import us444ColumnMaskSpecSource from '../../../e2e/us444/16-mask.spec.ts?raw';
import us444CellMaskSpecSource from '../../../e2e/us444/17-cell-mask.spec.ts?raw';
import us444FunctionPublishSpecSource from '../../../e2e/us444/18-fn-publish.spec.ts?raw';
import us444FunctionReplaySpecSource from '../../../e2e/us444/19-fn-replay.spec.ts?raw';
import us444SubscribeSpecSource from '../../../e2e/us444/20-subscribe.spec.ts?raw';
import us444HelpersSource from '../../../e2e/us444/helpers.ts?raw';
import us444ReadmeSource from '../../../e2e/us444/README.md?raw';
import vtx099SystemGraphSpecSource from '../../../e2e/vtx-099-system-graph-render.spec.ts?raw';

describe('Playwright spec contracts', () => {
  it('keeps CI Playwright discovery aligned with every upper-layer E2E probe group', () => {
    const stepStart = ciWorkflowSource.indexOf('- name: Playwright spec discovery');
    expect(stepStart).toBeGreaterThanOrEqual(0);

    const discoveryStep = ciWorkflowSource.slice(stepStart);
    const requiredTargets = [
      'us444/',
      'phase6/',
      'phase7/',
      'vtx-099-system-graph-render.spec.ts',
      'dogfood-verify.spec.ts',
      'dogfood-diagnose.spec.ts',
      'dogfood-empty-states.spec.ts',
      'tests/',
      'us-456-perf-dashboard.spec.ts',
      'us-457-timeseries-tab.spec.ts',
      'us-458-hotkey-help.spec.ts',
      'zzz-login-rate-limit.spec.ts',
    ];

    expect(discoveryStep).toContain('npx playwright test --list');
    for (const target of requiredTargets) {
      expect(discoveryStep).toContain(target);
    }
  });

  it('discovers the entire Playwright BDD suite in CI list-only mode', () => {
    const stepStart = ciWorkflowSource.indexOf('- name: Playwright spec discovery');
    expect(stepStart).toBeGreaterThanOrEqual(0);

    const discoveryStep = ciWorkflowSource.slice(stepStart);
    expect(discoveryStep).toMatch(/(^|\s)tests\/(\s|$)/);
  });

  it('keeps the Phase 7 SSE reconnect scenario discoverable and deterministic', () => {
    const source = sseReconnectSpecSource;

    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain("getByTestId('search-input')");
    expect(source).toContain(`/api/v2/ontologies/\${ONTOLOGY}/objects/\${OBJECT_TYPE}/search`);
    expect(source).toContain("getByTestId('live-status')");
    expect(source).not.toContain("getByTestId('realtime-indicator')");
  });

  it('keeps all Phase 7 probes active against reachable service failures', () => {
    const phase7Sources = [
      auditLogAdminSpecSource,
      browserRealtimeModeSpecSource,
      policyColumnHidingSpecSource,
      policyRowFilterSpecSource,
      sseReconnectSpecSource,
    ];

    for (const source of phase7Sources) {
      expect(source).not.toMatch(/test\.skip\s*\(/);
      expect(source).not.toMatch(/test\.fixme\s*\(/);
      expect(source).not.toMatch(/not yet wired|unavailable/i);
      expect(source).not.toContain('res.ok() ||');
      expect(source).not.toMatch(/status\(\)\s*===\s*(404|503)/);
      expect(source).not.toContain('toContain(res.status())');
    }
  });

  it('keeps Phase 7 policy and audit probes asserting seeded identity semantics', () => {
    expect(policyColumnHidingSpecSource).toContain("email: 'manager@test'");
    expect(policyColumnHidingSpecSource).toContain("email: 'peer@test'");
    expect(policyColumnHidingSpecSource).toContain('/objects/${OBJECT_TYPE}');
    expect(policyColumnHidingSpecSource).toContain('Array.isArray(mgrBody.data)');
    expect(policyColumnHidingSpecSource).toContain('Array.isArray(peerBody.data)');
    expect(policyColumnHidingSpecSource).toContain(
      "expect(obj).toHaveProperty('salary')",
    );
    expect(policyColumnHidingSpecSource).toContain(
      "expect(obj).not.toHaveProperty('salary')",
    );
    expect(policyColumnHidingSpecSource).toContain(
      "getByRole('cell', { name: '120000' })",
    );
    expect(policyColumnHidingSpecSource).toContain(
      "getByRole('cell', { name: 'Alice Chen' })",
    );

    expect(policyRowFilterSpecSource).toContain("email: 'acme@test'");
    expect(policyRowFilterSpecSource).toContain("email: 'acme2@test'");
    expect(policyRowFilterSpecSource).toContain('Array.isArray(acmeBody.data)');
    expect(policyRowFilterSpecSource).toContain('Array.isArray(acme2Body.data)');
    expect(policyRowFilterSpecSource).toContain(
      "expect(acmeIDs.sort()).toEqual(['ALFKI', 'BERGS', 'CHOPS'])",
    );
    expect(policyRowFilterSpecSource).toContain(
      "expect(acme2IDs.sort()).toEqual(['BLONP', 'CACTU'])",
    );
    expect(policyRowFilterSpecSource).toContain(
      "getByRole('cell', { name: 'Alfreds Futterkiste' })",
    );
    expect(policyRowFilterSpecSource).toContain(
      "getByRole('cell', { name: 'Cactus Comidas para llevar' })",
    );

    expect(auditLogAdminSpecSource).toContain("email: 'admin@test'");
    expect(auditLogAdminSpecSource).toContain("email: 'peer@test'");
    expect(auditLogAdminSpecSource).toContain('/api/v2/admin/auditEvents');
    expect(auditLogAdminSpecSource).toContain('Array.isArray(body.data)');
    expect(auditLogAdminSpecSource).toContain(
      'expect(body.data.length).toBeGreaterThan(0)',
    );
    expect(auditLogAdminSpecSource).toContain(
      "expect(entry.action).toBe('login_success')",
    );
    expect(auditLogAdminSpecSource).toContain('expect(auditRes.status()).toBe(403)');
    expect(auditLogAdminSpecSource).not.toContain('if (body.data.length > 0)');
  });

  it('keeps Phase 7 realtime probes tied to seeded actions and live streams', () => {
    const realtimeSources = [browserRealtimeModeSpecSource, sseReconnectSpecSource];

    for (const source of realtimeSources) {
      expect(source).toContain('/actionTypes');
      expect(source).toContain('Array.isArray(body.data)');
      expect(source).toContain("a.apiName === 'createCustomer'");
      expect(source).toContain('/actions/createCustomer/apply');
      expect(source).toContain("getByTestId('search-input')");
      expect(source).toContain(
        `/api/v2/ontologies/\${ONTOLOGY}/objects/\${OBJECT_TYPE}/search`,
      );
    }

    expect(browserRealtimeModeSpecSource).toContain('waitForRealtimeSubscribed');
    expect(browserRealtimeModeSpecSource).toContain('waitForRealtimeObjectChange');
    expect(browserRealtimeModeSpecSource).toContain('realtimePayloadMatchesPrimaryKey');
    expect(browserRealtimeModeSpecSource).toContain("getByTestId('realtime-indicator')");

    expect(sseReconnectSpecSource).toContain('page.context().setOffline(true)');
    expect(sseReconnectSpecSource).toContain('page.context().setOffline(false)');
    expect(sseReconnectSpecSource).toContain("getByTestId('live-status')");
  });

  it('keeps all Phase 6 probes active against reachable service failures', () => {
    const phase6Sources = [
      phase6AggregationSpecSource,
      phase6InterfacePagingSpecSource,
      optimisticConcurrencySpecSource,
      phase6WithPropertiesSpecSource,
    ];

    for (const source of phase6Sources) {
      expect(source).not.toMatch(/test\.skip\s*\(/);
      expect(source).not.toMatch(/test\.fixme\s*\(/);
      expect(source).not.toMatch(/not yet wired|unavailable/i);
      expect(source).not.toContain('res.ok() ||');
      expect(source).not.toMatch(/status\(\)\s*===\s*(404|503)/);
      expect(source).not.toContain('toContain(res.status())');
    }
  });

  it('keeps Phase 6 probes asserting concrete OSv2 service response shape and rendered data', () => {
    expect(phase6WithPropertiesSpecSource).toContain('Array.isArray(otBody.data)');
    expect(phase6WithPropertiesSpecSource).toContain('Array.isArray(ltBody.linkTypes)');
    expect(phase6WithPropertiesSpecSource).toContain("getByTestId('derived-property-row')");
    expect(phase6WithPropertiesSpecSource).toContain('data-derived-column="${DERIVED_NAME}"');
    expect(phase6WithPropertiesSpecSource).toContain('Number.isFinite(n)');

    expect(phase6AggregationSpecSource).toContain('Array.isArray(body.data)');
    expect(phase6AggregationSpecSource).toContain("getByTestId('aggregation-bucket-tree')");
    expect(phase6AggregationSpecSource).toContain(
      "toHaveAttribute('data-groupby-depth', '3')",
    );
    expect(phase6AggregationSpecSource).toContain("getByTestId('aggregation-accuracy-badge')");
    expect(phase6AggregationSpecSource).toContain('bodyRows.count()');

    expect(optimisticConcurrencySpecSource).toContain('Array.isArray(body.data)');
    expect(optimisticConcurrencySpecSource).toContain(
      "const ACTION_API_NAME = 'updateCustomerContact'",
    );
    expect(optimisticConcurrencySpecSource).toContain("getByTestId('object-version')");
    expect(optimisticConcurrencySpecSource).toContain('toBe(startingVersionB)');
    expect(optimisticConcurrencySpecSource).toContain('This object was updated elsewhere');

    expect(phase6InterfacePagingSpecSource).toContain('Array.isArray(body.data)');
    expect(phase6InterfacePagingSpecSource).toContain('Array.isArray(page.data)');
    expect(phase6InterfacePagingSpecSource).toContain(
      'loadObjectsOrInterfaces must return data rows',
    );
    expect(phase6InterfacePagingSpecSource).toContain(
      'server must report totalCount on every page',
    );
    expect(phase6InterfacePagingSpecSource).toContain('duplicate row ${compositeKey}');
  });

  it('keeps the Phase 6 optimistic-concurrency scenario active', () => {
    const source = optimisticConcurrencySpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain("getByTestId('object-version')");
    expect(source).toContain('StaleObject banner');
  });

  it('keeps the US-444 action gate tied to a seeded action dispatcher', () => {
    const source = us444ActionSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).not.toContain('types.length === 0');
    expect(source).not.toContain('types[0].apiName');
    expect(source).toContain("const ACTION_API_NAME = 'createCustomer'");
    expect(source).toContain(
      `/api/v2/ontologies/\${ONTOLOGY}/actions/\${ACTION_API_NAME}/apply`,
    );
    expect(source).toContain('errorCode');
    expect(source).toContain('errorName');
  });

  it('keeps the US-444 aggregate gate wired and skip-free', () => {
    const source = us444AggregateSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain('/objects/customer/aggregate');
    expect(source).toContain('aggregate endpoint must be wired');
    expect(source).not.toContain('aggregate endpoint unavailable');
  });

  it('keeps the US-444 browse and role-management gates wired and skip-free', () => {
    const sources = [us444BrowseSpecSource, us444RoleMgmtSpecSource];

    for (const source of sources) {
      expect(source).not.toMatch(/test\.skip\s*\(/);
      expect(source).not.toMatch(/test\.fixme\s*\(/);
    }

    expect(us444BrowseSpecSource).toContain('/objects/customer');
    expect(us444BrowseSpecSource).toContain('customer objects endpoint must be wired');
    expect(us444BrowseSpecSource).toContain('northwind seed must include customer rows');
    expect(us444BrowseSpecSource).not.toContain('objects endpoint unavailable');
    expect(us444BrowseSpecSource).not.toContain('northwind seed produced 0 customer rows');

    expect(us444RoleMgmtSpecSource).toContain('/api/v2/me');
    expect(us444RoleMgmtSpecSource).toContain('me endpoint must be wired');
    expect(us444RoleMgmtSpecSource).not.toContain('me endpoint unavailable');
  });

  it('keeps the US-444 saga gate wired and skip-free', () => {
    const source = us444SagaSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain('/api/v2/ontologies/${ONTOLOGY}/actions/applySaga');
    expect(source).toContain('/api/v2/ontologies/${ONTOLOGY}/actions/saga/dlq');
    expect(source).toContain('saga apply endpoint must be wired');
    expect(source).toContain('saga DLQ endpoint must be wired');
    expect(source).not.toContain('saga endpoint not wired');
    expect(source).not.toContain('saga DLQ endpoint not wired');
    expect(source).not.toContain('res.ok() || res.status() === 503');
    expect(source).not.toContain('res.status() === 404');
  });

  it('keeps the US-444 lineage gate wired to mandatory lineage endpoints', () => {
    const source = us444LineageSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain('/objectTypes/customer/fullMetadata');
    expect(source).toContain('/api/v2/lineage/property/${propRid}');
    expect(source).toContain('/api/v2/lineage/dataset-columns/impact?dataset=');
    expect(source).toContain('impacted');
    expect(source).not.toContain('res.ok() || res.status() === 503');
  });

  it('keeps the US-444 PITR gates wired and skip-free', () => {
    const source = us444PitrSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain('/api/v2/datasets/us444-unknown/history');
    expect(source).toContain('/api/v2/datasets/us444-unknown/rollback');
    expect(source).toContain('dataset history endpoint must be wired');
    expect(source).toContain('rollback endpoint must validate missing target');
    expect(source).not.toContain('route unwired');
    expect(source).not.toContain('res.status() === 404 && (await res.text()).length === 0');
  });

  it('keeps the US-444 branch lifecycle gates wired and skip-free', () => {
    const sources = [us444BranchSpecSource, us444MergeSpecSource];

    for (const source of sources) {
      expect(source).not.toMatch(/test\.skip\s*\(/);
      expect(source).not.toMatch(/test\.fixme\s*\(/);
      expect(source).not.toContain('.status() === 404');
      expect(source).not.toContain(', 404');
      expect(source).not.toContain('404,');
    }

    expect(us444BranchSpecSource).toContain(
      `/api/v2/ontologies/\${ONTOLOGY}/branches`,
    );
    expect(us444BranchSpecSource).toContain('toContain(branchName)');
    expect(us444BranchSpecSource).toContain('delete');

    expect(us444MergeSpecSource).toContain('/diff');
    expect(us444MergeSpecSource).toContain('/merge');
    expect(us444MergeSpecSource).toMatch(/expectOK\(diff,\s*['"]branch diff endpoint must be wired/);
    expect(us444MergeSpecSource).toContain('merge.status()');
    expect(us444MergeSpecSource).toContain('conflictResolution');
  });

  it('keeps the US-444 App Builder gate wired and skip-free', () => {
    const source = us444AppBuilderSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain('/api/v2/apps');
    expect(source).toContain('app create endpoint must be wired');
    expect(source).toContain('app get endpoint must be wired');
    expect(source).toContain('app delete endpoint must be wired');
    expect(source).not.toContain('apps store not wired');
    expect(source).not.toContain('create.status() === 404');
    expect(source).not.toContain('create.status() === 503');
  });

  it('keeps the US-444 Quiver gate wired and skip-free', () => {
    const source = us444QuiverSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain('/api/v2/quiver/save');
    expect(source).toContain('/api/v2/quiver/dashboards');
    expect(source).toContain('quiver save endpoint must be wired');
    expect(source).toContain('quiver list endpoint must be wired');
    expect(source).toContain('quiver delete endpoint must be wired');
    expect(source).not.toContain('quiver store not wired');
    expect(source).not.toContain('save.status() === 404');
    expect(source).not.toContain('save.status() === 503');
  });

  it('keeps the US-444 package lifecycle gates wired and skip-free', () => {
    const sources = [us444MarketplaceSpecSource, us444PackageInstallSpecSource];

    for (const source of sources) {
      expect(source).not.toMatch(/test\.skip\s*\(/);
      expect(source).not.toMatch(/test\.fixme\s*\(/);
      expect(source).toContain('/api/v2/pkg/builtin');
    }

    expect(us444MarketplaceSpecSource).toContain('/api/v2/pkg');
    expect(us444MarketplaceSpecSource).toContain('built-in catalog endpoint must be wired');
    expect(us444MarketplaceSpecSource).toContain('built-in catalog must include seeded packages');
    expect(us444MarketplaceSpecSource).toContain('installed packages list endpoint must be wired');
    expect(us444MarketplaceSpecSource).not.toContain('no built-in packages compiled into this binary');
    expect(us444MarketplaceSpecSource).not.toContain('toContain(res.status())');

    expect(us444PackageInstallSpecSource).toContain('/api/v2/pkg/builtin/iot-demo/install');
    expect(us444PackageInstallSpecSource).toContain('built-in catalog endpoint must be wired');
    expect(us444PackageInstallSpecSource).toContain('iot-demo must be present in the built-in catalog');
    expect(us444PackageInstallSpecSource).toContain('package installer endpoint must be wired');
    expect(us444PackageInstallSpecSource).not.toContain('built-in catalog endpoint unavailable');
    expect(us444PackageInstallSpecSource).not.toContain('package installer not wired');
  });

  it('keeps the US-444 function publish and replay gates wired and skip-free', () => {
    const sources = [us444FunctionPublishSpecSource, us444FunctionReplaySpecSource];

    for (const source of sources) {
      expect(source).not.toMatch(/test\.skip\s*\(/);
      expect(source).not.toMatch(/test\.fixme\s*\(/);
      expect(source).toContain(`/api/v2/ontologies/\${ONTOLOGY}/functions`);
      expect(source).not.toContain('functions endpoint unwired');
      expect(source).not.toContain('function create failed');
    }

    expect(us444FunctionPublishSpecSource).toContain('function create endpoint must be wired');
    expect(us444FunctionPublishSpecSource).toContain('function versions endpoint must be wired');
    expect(us444FunctionPublishSpecSource).toContain('function delete endpoint must be wired');

    expect(us444FunctionReplaySpecSource).toContain('function create endpoint must be wired');
    expect(us444FunctionReplaySpecSource).toContain('function execute endpoint must be wired');
    expect(us444FunctionReplaySpecSource).toContain('function replay endpoint must be wired');
    expect(us444FunctionReplaySpecSource).toContain('function delete endpoint must be wired');
    expect(us444FunctionReplaySpecSource).not.toContain('execute failed:');
    expect(us444FunctionReplaySpecSource).not.toContain('replay endpoint not wired');
    expect(us444FunctionReplaySpecSource).not.toContain('expect([200, 503]).toContain(replay.status())');
  });

  it('keeps the US-444 mask admin gates wired and skip-free', () => {
    const sources = [us444ColumnMaskSpecSource, us444CellMaskSpecSource];

    for (const source of sources) {
      expect(source).not.toMatch(/test\.skip\s*\(/);
      expect(source).not.toMatch(/test\.fixme\s*\(/);
      expect(source).toContain('/api/admin/');
      expect(source).not.toContain('store not wired');
      expect(source).not.toContain('res.status() === 404');
      expect(source).not.toContain('401, 403, 503');
    }

    expect(us444ColumnMaskSpecSource).toContain('/api/admin/column-masks');
    expect(us444ColumnMaskSpecSource).toContain('column-mask list endpoint must be wired');
    expect(us444ColumnMaskSpecSource).toContain('column-mask create endpoint must be wired');
    expect(us444ColumnMaskSpecSource).toContain('InvalidColumnMask');

    expect(us444CellMaskSpecSource).toContain('/api/admin/cell-masks');
    expect(us444CellMaskSpecSource).toContain('cell-mask list endpoint must be wired');
    expect(us444CellMaskSpecSource).toContain('cell-mask create endpoint must be wired');
    expect(us444CellMaskSpecSource).toContain('InvalidCellMask');
  });

  it('keeps the US-444 subscribe gate tied to the WebSocket subscribe handshake', () => {
    const source = us444SubscribeSpecSource;

    expect(source).not.toContain('no welcome frame within 3s');
    expect(source).not.toContain('endpoint unwired or quiet');
    expect(source).not.toContain('test.skip(firstMessage === null');
    expect(source).toMatch(/type:\s*['"]subscribe['"]/);
    expect(source).toMatch(/objectType:\s*['"]customer['"]/);
    expect(source).toContain("toBe('subscribed')");
    expect(source).toContain('subscriptionId');
  });

  it('keeps US-444 helper guidance aligned with mandatory wired-service gates', () => {
    const sources = [us444HelpersSource, us444ReadmeSource];

    for (const source of sources) {
      expect(source).toContain('skipWhenBackendDown');
      expect(source).not.toMatch(/optional feature/i);
      expect(source).not.toMatch(/404[\s\S]{0,80}503[\s\S]{0,80}skip/i);
      expect(source).not.toContain('feature surface returns 503/404');
      expect(source).not.toContain('endpoint unavailable');
    }

    expect(us444HelpersSource).toMatch(
      /Reachable backend service failures should be[\s\S]{0,40}asserted in the spec body\./,
    );
    expect(us444ReadmeSource).toMatch(
      /A reachable[\s\S]{0,40}backend with an unwired service endpoint is a test failure\./,
    );
  });

  it('keeps the VTX-099 system graph gate wired and skip-free after backend health', () => {
    const source = vtx099SystemGraphSpecSource;

    expect(source).not.toContain('systemGraphReachable');
    expect(source).not.toContain('VTX-018 System Graph page not yet wired up');
    expect(source).not.toMatch(/test\.skip\s*\(\s*!\s*\(\s*await\s+systemGraphReachable/);
    expect(source).toContain('/api/vertex/v1/graphs/${encodeURIComponent(rid)}');
    expect(source).toContain('system graph payload must include nodes');
    expect(source).toContain('system graph payload must include edges');
    expect(source).toContain("getByTestId('vertex-canvas-host')");

    const skipLines = source.match(/test\.skip[^\n]+/g) ?? [];
    expect(skipLines).toEqual([
      "test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');",
      "test.skip(!(await backendReachable(request)), 'weave backend not reachable on :9117');",
    ]);
  });
});
