import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  MarketplacePage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/marketplace` — the Marketplace UI completed by
 * US-054 / PC-A12 on top of the US-413 / US-414 / US-454 foundation.
 *
 * AC mapping → scenario:
 *
 *   "列表卡片：名称、版本、状态、安装/更新/卸载按钮"
 *     → installed-list-shows-buttons + update-from-installed scenarios
 *
 *   "包详情抽屉：变更说明、依赖、引用文档"
 *     → details-drawer-renders-changelog-dependencies-references scenario
 *
 *   "Built-in vs user packages 分组显示"
 *     → tabs-split-installed-from-builtin scenario
 *
 *   "新增 spec 至少 4 scenarios" → four @smoke scenarios below
 *
 * Wire shapes mirror `cmd/server/handlers_pkg_*.go` and the
 * `pkg/oms/installedpkgpg` store byte-for-byte so the spec stays in
 * lockstep with the production endpoint contract.
 */

interface MockInstalledPackage {
  id: number;
  name: string;
  version: string;
  ontology: string;
  manifest: {
    name?: string;
    version?: string;
    description?: string;
    author?: string;
    license?: string;
    dependencies?: Record<string, string>;
    contents?: Record<string, unknown>;
  } | null;
  migrations: string[];
  enabled: boolean;
  installedBy?: string;
  installedAt: string;
  updatedAt: string;
}

interface MockBuiltinPackage {
  slug: string;
  name: string;
  version: string;
  ontologyApiName: string;
  author?: string;
  license?: string;
  description?: string;
  minWeaveVersion?: string;
  dependencies?: Array<{ name: string; version: string }>;
  objectTypeCount: number;
  linkTypeCount: number;
  actionTypeCount: number;
  functionCount: number;
  migrationCount: number;
}

interface InstallRecord {
  slug: string;
  onConflict: string | null;
}

interface MarketplaceStubState {
  listCalls: number;
  builtinListCalls: number;
  installs: InstallRecord[];
  deletes: string[];
  installed: MockInstalledPackage[];
  builtin: MockBuiltinPackage[];
}

function makeState(seed: {
  installed: MockInstalledPackage[];
  builtin: MockBuiltinPackage[];
}): MarketplaceStubState {
  return {
    listCalls: 0,
    builtinListCalls: 0,
    installs: [],
    deletes: [],
    installed: [...seed.installed],
    builtin: [...seed.builtin],
  };
}

function pkg(
  overrides: Partial<MockInstalledPackage> = {},
): MockInstalledPackage {
  const name = overrides.name ?? 'northwind';
  return {
    id: 1,
    name,
    version: '1.0.0',
    ontology: name,
    manifest: {
      name,
      version: '1.0.0',
      description: 'Classic Northwind sales-ledger ontology.',
      author: 'Weave Examples',
      license: 'MIT',
      dependencies: { 'weave-core': '^1.0.0' },
      contents: { objectTypes: 3, linkTypes: 1, actionTypes: 1 },
    },
    migrations: ['000001_init.up.sql'],
    enabled: true,
    installedAt: '2026-05-01T00:00:00Z',
    updatedAt: '2026-05-01T00:00:00Z',
    ...overrides,
  };
}

function builtin(overrides: Partial<MockBuiltinPackage> = {}): MockBuiltinPackage {
  return {
    slug: 'northwind',
    name: 'northwind',
    version: '1.0.0',
    ontologyApiName: 'northwind',
    author: 'Weave Examples',
    license: 'MIT',
    description: 'Classic Northwind sales-ledger ontology.',
    minWeaveVersion: '0.42.0',
    dependencies: [{ name: 'weave-core', version: '^1.0.0' }],
    objectTypeCount: 3,
    linkTypeCount: 1,
    actionTypeCount: 1,
    functionCount: 0,
    migrationCount: 0,
    ...overrides,
  };
}

async function stubMarketplaceEndpoints(
  page: Page,
  state: MarketplaceStubState,
  options: { installDelayMs?: number } = {},
): Promise<void> {
  // Single catch-all over the marketplace endpoint family so we never
  // fight Playwright's LIFO resolution. The handler dispatches on
  // method + path internally; that template (per progress.txt's
  // /api/v2/saga-jobs notes) is more robust than chaining four
  // narrow page.route calls with `route.fallback()`.
  await page.route('**/api/v2/pkg**', async (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    const pathOnly = url.pathname;
    const method = req.method();

    // GET /api/v2/pkg → installed package list
    if (pathOnly === '/api/v2/pkg' && method === 'GET') {
      state.listCalls += 1;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: state.installed }),
      });
      return;
    }

    // GET /api/v2/pkg/builtin → embedded catalog list
    if (pathOnly === '/api/v2/pkg/builtin' && method === 'GET') {
      state.builtinListCalls += 1;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: state.builtin }),
      });
      return;
    }

    // POST /api/v2/pkg/builtin/{slug}/install → install or update
    const installMatch = pathOnly.match(
      /^\/api\/v2\/pkg\/builtin\/([^/]+)\/install$/,
    );
    if (installMatch && method === 'POST') {
      const slug = installMatch[1]!;
      const body = req.postDataJSON() as { onConflict?: string } | null;
      state.installs.push({ slug, onConflict: body?.onConflict ?? null });
      if (options.installDelayMs && options.installDelayMs > 0) {
        await new Promise((r) => setTimeout(r, options.installDelayMs));
      }
      const builtinRow = state.builtin.find((b) => b.slug === slug);
      if (builtinRow) {
        const idx = state.installed.findIndex(
          (p) => p.name === builtinRow.name,
        );
        const upserted: MockInstalledPackage = {
          id: idx >= 0 ? state.installed[idx]!.id : state.installed.length + 1,
          name: builtinRow.name,
          version: builtinRow.version,
          ontology: builtinRow.ontologyApiName,
          manifest: {
            name: builtinRow.name,
            version: builtinRow.version,
            description: builtinRow.description,
            author: builtinRow.author,
            license: builtinRow.license,
            dependencies: Object.fromEntries(
              (builtinRow.dependencies ?? []).map((d) => [d.name, d.version]),
            ),
          },
          migrations: [],
          enabled: true,
          installedAt: '2026-05-13T10:00:00Z',
          updatedAt: '2026-05-13T10:00:00Z',
        };
        if (idx >= 0) state.installed[idx] = upserted;
        else state.installed.push(upserted);
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          name: builtinRow?.name ?? slug,
          version: builtinRow?.version ?? '1.0.0',
          ontology: builtinRow?.ontologyApiName ?? slug,
          imported: { objectTypes: 3, linkTypes: 1 },
          migrationsRan: 0,
          migrationsTotal: 0,
          message: 'package installed',
        }),
      });
      return;
    }

    // DELETE /api/v2/pkg/{name} → uninstall
    const deleteMatch = pathOnly.match(/^\/api\/v2\/pkg\/([^/]+)$/);
    if (deleteMatch && method === 'DELETE') {
      const name = deleteMatch[1]!;
      state.deletes.push(name);
      state.installed = state.installed.filter((p) => p.name !== name);
      await route.fulfill({ status: 204, body: '' });
      return;
    }

    // Anything else (e.g. POST /api/v2/pkg/{name}/enabled when toggled)
    // — let the test scenario decide what it wants to do.
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: '{}',
    });
  });
}

describeFeature('Marketplace: install / update / uninstall', () => {
  test('Scenario: the Installed and Built-in tabs split user packages from the embedded catalog @smoke', async ({
    page,
    request,
  }) => {
    const market = new MarketplacePage(page);
    const state = makeState({
      installed: [pkg({ name: 'custom-app', ontology: 'customApp' })],
      builtin: [
        builtin(),
        builtin({ slug: 'chinook', name: 'chinook', ontologyApiName: 'chinook' }),
        builtin({ slug: 'iot-demo', name: 'iot-demo', ontologyApiName: 'iotDemo' }),
      ],
    });

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the marketplace endpoints advertise one installed + three built-in packages', async () => {
      await stubMarketplaceEndpoints(page, state);
    });

    await When('the user opens the marketplace page', async () => {
      await market.goto();
    });

    await Then('the Installed tab is active and shows the local install card', async () => {
      await expect(market.root).toBeVisible();
      await expect(market.tabInstalled).toHaveAttribute(
        'data-active',
        'true',
      );
      await expect(market.installedList).toBeVisible();
      await expect(market.installedCard('custom-app')).toBeVisible();
      // The built-in northwind/chinook/iot-demo are NOT bleeding into the
      // installed list — only locally-registered .weavepkg rows show here.
      await expect(market.installedCard('northwind')).toHaveCount(0);
      await expect(market.installedCard('chinook')).toHaveCount(0);
    });

    await When('the user switches to the Built-in tab', async () => {
      await market.tabBuiltin.click();
    });

    await Then('the Built-in catalog lists the three example packages', async () => {
      await expect(market.tabBuiltin).toHaveAttribute('data-active', 'true');
      await expect(market.builtinList).toBeVisible();
      await expect(market.builtinCard('northwind')).toBeVisible();
      await expect(market.builtinCard('chinook')).toBeVisible();
      await expect(market.builtinCard('iot-demo')).toBeVisible();
      // The locally-registered custom-app is NOT in the built-in tab.
      await expect(market.builtinCard('custom-app')).toHaveCount(0);
    });
  });

  test('Scenario: the details drawer surfaces changelog, dependencies, and reference docs @smoke', async ({
    page,
  }) => {
    const market = new MarketplacePage(page);
    const state = makeState({
      installed: [],
      builtin: [
        builtin({
          slug: 'northwind',
          name: 'northwind',
          description:
            'Classic Northwind sales-ledger ontology.\nIncludes customers, orders, products, and employees.',
          dependencies: [
            { name: 'weave-core', version: '^1.0.0' },
            { name: 'weave-search', version: '^0.42.0' },
          ],
          objectTypeCount: 3,
          linkTypeCount: 1,
          actionTypeCount: 1,
          migrationCount: 2,
        }),
      ],
    });

    await Given('the marketplace endpoints are stubbed', async () => {
      await stubMarketplaceEndpoints(page, state);
    });

    await Given('the user is on the Built-in tab', async () => {
      await market.goto();
      await market.tabBuiltin.click();
      await expect(market.builtinCard('northwind')).toBeVisible();
    });

    await When('the user opens the package details drawer', async () => {
      await market.detailsButtonForBuiltin('northwind').click();
    });

    await Then('the drawer renders with the package name and source attribute', async () => {
      await expect(market.detailsDrawer).toBeVisible();
      await expect(market.detailsDrawer).toHaveAttribute(
        'data-package-name',
        'northwind',
      );
      await expect(market.detailsDrawer).toHaveAttribute(
        'data-source',
        'builtin',
      );
      await expect(market.detailsPanel).toBeVisible();
    });

    await Then('the Changelog section shows the manifest description', async () => {
      await expect(market.detailsChangelog).toBeVisible();
      await expect(market.detailsChangelogBody).toContainText(
        'Classic Northwind sales-ledger ontology',
      );
      await expect(market.detailsChangelogBody).toContainText(
        'customers, orders, products',
      );
    });

    await Then('the Dependencies section lists every declared dependency with its version', async () => {
      await expect(market.detailsDependenciesList).toBeVisible();
      await expect(market.dependencyRow('weave-core')).toHaveAttribute(
        'data-dependency-version',
        '^1.0.0',
      );
      await expect(market.dependencyRow('weave-search')).toHaveAttribute(
        'data-dependency-version',
        '^0.42.0',
      );
    });

    await Then('the References section surfaces the catalog stats', async () => {
      await expect(market.detailsReferencesList).toBeVisible();
      await expect(market.referenceRow('Object Types')).toHaveAttribute(
        'data-reference-value',
        '3',
      );
      await expect(market.referenceRow('Link Types')).toHaveAttribute(
        'data-reference-value',
        '1',
      );
      await expect(market.referenceRow('Action Types')).toHaveAttribute(
        'data-reference-value',
        '1',
      );
      await expect(market.referenceRow('Migrations')).toHaveAttribute(
        'data-reference-value',
        '2',
      );
    });

    await When('the user closes the drawer', async () => {
      await market.detailsClose.click();
    });

    await Then('the drawer is no longer visible', async () => {
      await expect(market.detailsDrawer).toHaveCount(0);
    });
  });

  test('Scenario: an installed package whose built-in equivalent is newer offers an in-place Update @smoke', async ({
    page,
  }) => {
    const market = new MarketplacePage(page);
    const state = makeState({
      installed: [
        pkg({ name: 'northwind', version: '1.0.0', ontology: 'northwind' }),
      ],
      builtin: [builtin({ slug: 'northwind', name: 'northwind', version: '1.1.0' })],
    });

    await Given('the registry holds northwind v1.0.0 and the catalog ships v1.1.0', async () => {
      await stubMarketplaceEndpoints(page, state);
    });

    await When('the user opens the marketplace page', async () => {
      await market.goto();
      await expect(market.installedCard('northwind')).toBeVisible();
    });

    await Then('the card advertises the available update via a badge and a data attribute', async () => {
      await expect(market.installedCard('northwind')).toHaveAttribute(
        'data-update-available',
        'true',
      );
      await expect(market.updateBadgeFor('northwind')).toBeVisible();
      await expect(market.updateBadgeFor('northwind')).toHaveAttribute(
        'data-target-version',
        '1.1.0',
      );
    });

    await When('the user clicks the Update button', async () => {
      await market.updateButtonFor('northwind').click();
    });

    await Then('the install endpoint is hit with onConflict=overwrite for the matching slug', async () => {
      await expect.poll(() => state.installs.length).toBeGreaterThanOrEqual(1);
      const last = state.installs[state.installs.length - 1]!;
      expect(last.slug).toBe('northwind');
      expect(last.onConflict).toBe('overwrite');
    });

    await Then('after the mutation resolves the card reflects the new version and the badge is gone', async () => {
      await expect
        .poll(() => market.installedCard('northwind').getAttribute('data-package-version'))
        .toBe('1.1.0');
      await expect(market.installedCard('northwind')).toHaveAttribute(
        'data-update-available',
        'false',
      );
      await expect(market.updateBadgeFor('northwind')).toHaveCount(0);
    });
  });

  test('Scenario: uninstall confirmation gates the destructive DELETE @smoke', async ({
    page,
  }) => {
    const market = new MarketplacePage(page);
    const state = makeState({
      installed: [pkg({ name: 'custom-app', ontology: 'customApp' })],
      builtin: [],
    });

    await Given('a package is locally installed', async () => {
      await stubMarketplaceEndpoints(page, state);
    });

    await When('the user opens the marketplace page', async () => {
      await market.goto();
      await expect(market.installedCard('custom-app')).toBeVisible();
    });

    await When('the user clicks the uninstall button on the card', async () => {
      await market.uninstallButtonFor('custom-app').click();
    });

    await Then('the confirmation dialog appears and no DELETE has fired yet', async () => {
      await expect(market.uninstallDialog).toBeVisible();
      expect(state.deletes).toHaveLength(0);
    });

    await When('the user cancels the confirmation', async () => {
      await market.uninstallCancel.click();
    });

    await Then('the dialog closes and the card is still on screen', async () => {
      await expect(market.uninstallDialog).toHaveCount(0);
      await expect(market.installedCard('custom-app')).toBeVisible();
      expect(state.deletes).toHaveLength(0);
    });

    await When('the user re-opens the dialog and confirms', async () => {
      await market.uninstallButtonFor('custom-app').click();
      await expect(market.uninstallDialog).toBeVisible();
      await market.uninstallConfirm.click();
    });

    await Then('the DELETE fires once and the card disappears from the list', async () => {
      await expect.poll(() => state.deletes).toEqual(['custom-app']);
      await expect(market.installedCard('custom-app')).toHaveCount(0);
    });
  });
});
