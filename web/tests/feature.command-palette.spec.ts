import { expect, test } from '@playwright/test';
import {
  Given,
  Then,
  When,
  describeFeature,
  seedOntology,
  signIn,
} from './support';

/**
 * BDD smoke coverage for the global command palette inside an active
 * ontology workspace. Unit tests own the full command catalog; this
 * browser probe keeps the shortcut, Shell wiring, cmdk option render, and
 * route transition discoverable through Playwright.
 */

describeFeature('Command palette ontology workspace navigation', () => {
  test('Scenario: Query Builder opens from the command palette in an active ontology @smoke', async ({
    page,
    request,
  }) => {
    await Given('the visitor is authenticated and northwind exists', async () => {
      const authHeaders = await signIn(request);
      await seedOntology(request, { apiName: 'northwind', authHeaders });
    });

    await When('they open the command palette from the northwind Explorer', async () => {
      await page.goto('/explorer/northwind');
      await page.locator('body').click();
      await page.keyboard.press('Control+K');
    });

    await Then('the command palette is visible', async () => {
      await expect(page.getByTestId('command-palette')).toBeVisible();
    });

    await When('they search for Query Builder', async () => {
      await page
        .getByPlaceholder(/search actions, objects, branches, apps, pages/i)
        .fill('Query Builder');
    });

    await Then('the active-ontology Query Builder option is available', async () => {
      const palette = page.getByTestId('command-palette');
      await expect(
        palette.getByRole('option', { name: /query builder/i }),
      ).toBeVisible();
    });

    await When('they select Query Builder', async () => {
      const palette = page.getByTestId('command-palette');
      await palette.getByRole('option', { name: /query builder/i }).click();
    });

    await Then('the app routes to the northwind ObjectSet builder', async () => {
      await page.waitForURL(/\/objectsets\/northwind$/);
      expect(page.url()).toContain('/objectsets/northwind');
    });
  });
});
