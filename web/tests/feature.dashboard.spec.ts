import { expect, test } from '@playwright/test';
import {
  DashboardPage,
  Given,
  Then,
  When,
  describeFeature,
  seedOntology,
  signIn,
} from './support';

/**
 * BDD coverage of the Dashboard landing page (`/`) — the post-login
 * default route rendered by `src/components/dashboard/DashboardPage.tsx`.
 *
 * Scenarios cover the AC for US-021 (frontend-backend-gap-coverage PRD):
 *   1. login-then-redirect: the index route renders the Dashboard
 *   2. quick-entry: stats bar + section heading are present
 *   3. ontology-card → switch into Explorer
 *   4. seeded extra ontology surfaces on the grid (recent activity card)
 *   5. empty state when /api/v2/ontologies returns []
 *   6. error state when /api/v2/ontologies fails
 *
 * Selectors use `data-testid` attributes added on the Dashboard /
 * StatsBar / OntologyCard wrapper in the same change. The route mocks
 * for the empty + error scenarios go through `page.route()` so they
 * don't disturb the seeded backend state.
 */

describeFeature('Dashboard landing page', () => {
  test('Scenario: navigating to / renders the Dashboard after login @smoke', async ({
    page,
    request,
  }) => {
    const dashboard = new DashboardPage(page);

    await Given('the visitor is authenticated', async () => {
      // Under AUTH_MODE=dev the backend auto-issues a session; in token
      // mode this seeds the bearer header inside the API context. Either
      // way the helper returns `{}` if no header is needed.
      await signIn(request);
    });

    await When('they navigate to the index route', async () => {
      await dashboard.goto();
    });

    await Then('the Dashboard page renders', async () => {
      await expect(dashboard.root).toBeVisible();
    });

    await Then('the loading skeleton has cleared', async () => {
      await expect(dashboard.loading).toBeHidden();
    });
  });

  test('Scenario: the stats bar and Ontologies section heading are visible @smoke', async ({
    page,
  }) => {
    const dashboard = new DashboardPage(page);

    await Given('the user is on the Dashboard', async () => {
      await dashboard.goto();
      await expect(dashboard.root).toBeVisible();
    });

    await Then('the Ontologies stat cell is visible with a numeric value', async () => {
      await expect(dashboard.statOntologies).toBeVisible();
      const text = (await dashboard.statOntologies.textContent()) ?? '';
      expect(text.trim()).toMatch(/^\d+$/);
    });

    await Then('the Object Types stat cell is visible with a numeric value', async () => {
      await expect(dashboard.statObjectTypes).toBeVisible();
      const text = (await dashboard.statObjectTypes.textContent()) ?? '';
      expect(text.trim()).toMatch(/^\d+$/);
    });
  });

  test('Scenario: clicking the seeded northwind card switches to the Explorer @smoke', async ({
    page,
    request,
  }) => {
    const dashboard = new DashboardPage(page);

    await Given('the seeded northwind ontology exists', async () => {
      await seedOntology(request, { apiName: 'northwind' });
    });

    await Given('the user is on the Dashboard', async () => {
      await dashboard.goto();
      await expect(dashboard.ontologyGrid).toBeVisible();
    });

    await When('they click the northwind ontology card', async () => {
      await dashboard.openOntology('northwind');
    });

    await Then('the URL switches to the Explorer for northwind', async () => {
      await page.waitForURL(/\/explorer\/northwind/);
      expect(page.url()).toContain('/explorer/northwind');
    });
  });

  test('Scenario: the grid surfaces a newly registered ontology on refresh', async ({
    page,
  }) => {
    const dashboard = new DashboardPage(page);
    const apiName = 'bdd_dash_synthetic';
    const rid = 'ri.ontology.main.ontology.bdd-dash-synthetic';

    await Given(
      `the /api/v2/ontologies endpoint advertises a fresh "${apiName}" row`,
      async () => {
        await page.route('**/api/v2/ontologies', async (route, request) => {
          if (request.method() !== 'GET') {
            await route.continue();
            return;
          }
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              data: [
                {
                  rid,
                  apiName,
                  displayName: 'BDD Dashboard Synthetic',
                  description: 'Synthetic ontology injected by feature.dashboard.spec.ts',
                  currentVersion: 0,
                },
              ],
            }),
          });
        });
        await page.route(`**/api/v2/ontologies/${apiName}/objectTypes`, async (route) => {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ data: [] }),
          });
        });
      },
    );

    await When('the user opens the Dashboard', async () => {
      await dashboard.goto();
    });

    await Then('the new ontology card appears on the grid', async () => {
      await expect(dashboard.ontologyGrid).toBeVisible();
      await expect(
        page.locator(`[data-ontology-api-name="${apiName}"]`),
      ).toBeVisible();
    });

    await Then('the stats bar counts the synthetic ontology', async () => {
      await expect(dashboard.statOntologies).toHaveText('1');
    });
  });

  test('Scenario: the Dashboard renders the empty state when there are no ontologies', async ({
    page,
  }) => {
    const dashboard = new DashboardPage(page);

    await Given(
      'the /api/v2/ontologies endpoint is stubbed to return an empty list',
      async () => {
        await page.route('**/api/v2/ontologies', async (route) => {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ data: [] }),
          });
        });
      },
    );

    await When('the user opens the Dashboard', async () => {
      await dashboard.goto();
    });

    await Then('the empty-state panel is visible', async () => {
      await expect(dashboard.emptyState).toBeVisible();
    });

    await Then('the ontology grid is not rendered', async () => {
      await expect(dashboard.ontologyGrid).toHaveCount(0);
    });
  });

  test('Scenario: the Dashboard renders the error state when /ontologies fails', async ({
    page,
  }) => {
    const dashboard = new DashboardPage(page);

    await Given(
      'the /api/v2/ontologies endpoint is stubbed to return 500',
      async () => {
        await page.route('**/api/v2/ontologies', async (route) => {
          await route.fulfill({
            status: 500,
            contentType: 'application/json',
            body: JSON.stringify({ error: { code: 'INTERNAL', message: 'boom' } }),
          });
        });
      },
    );

    await When('the user opens the Dashboard', async () => {
      await dashboard.goto();
    });

    await Then('the error panel is visible', async () => {
      await expect(dashboard.error).toBeVisible();
    });

    await Then('the ontology grid is not rendered', async () => {
      await expect(dashboard.ontologyGrid).toHaveCount(0);
    });
  });
});
