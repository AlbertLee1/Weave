import { expect, test } from '@playwright/test';
import { Given, Then, When, describeFeature, signIn } from './support';

/**
 * Dogfood Round 3 #2: `/admin/datasets/:dataset/rollback` accidentally
 * dropped the ontology-scoped sidebar (Query Builder, Quiver TS, Object
 * Types, ...) because the route declared `:dataset` as the URL parameter
 * while `Sidebar`/`Topbar` only read `:ontology`. The fix is to accept
 * `:dataset` as an alias on the same kind of admin route.
 *
 * This spec navigates between `/admin/:ontology/security` (a route that
 * still uses `:ontology`) and `/admin/datasets/:dataset/rollback` and
 * asserts that the sidebar continues to surface the ontology-scoped
 * navigation entries on both.
 */

describeFeature('Sidebar ontology scope across :dataset routes', () => {
  test('Scenario: dataset rollback page preserves ontology sidebar @smoke', async ({
    page,
    request,
  }) => {
    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await When(
      'they navigate to /admin/iotDemo/security (an :ontology route)',
      async () => {
        await page.goto('/admin/iotDemo/security');
      },
    );

    await Then(
      'the sidebar shows the ontology-scoped Query Builder entry',
      async () => {
        const sidebar = page.getByTestId('sidebar');
        await expect(sidebar).toBeVisible();
        await expect(sidebar.getByText('Query Builder')).toBeVisible();
      },
    );

    await When(
      'they navigate to /admin/datasets/iotDemo/rollback (a :dataset route)',
      async () => {
        await page.goto('/admin/datasets/iotDemo/rollback');
      },
    );

    await Then(
      'the sidebar still surfaces Query Builder scoped to iotDemo',
      async () => {
        const sidebar = page.getByTestId('sidebar');
        await expect(sidebar).toBeVisible();
        const queryBuilder = sidebar.getByText('Query Builder');
        await expect(queryBuilder).toBeVisible();
        await expect(queryBuilder.locator('xpath=ancestor::a')).toHaveAttribute(
          'href',
          '/objectsets/iotDemo',
        );
      },
    );

    await Then(
      'the sidebar still surfaces Quiver TS scoped to iotDemo',
      async () => {
        const sidebar = page.getByTestId('sidebar');
        const quiver = sidebar.getByText('Quiver TS');
        await expect(quiver).toBeVisible();
        await expect(quiver.locator('xpath=ancestor::a')).toHaveAttribute(
          'href',
          '/quiver/iotDemo',
        );
      },
    );
  });
});
