import { expect, test } from '@playwright/test';
import { Given, Then, When, describeFeature, signIn } from './support';

/**
 * Dogfood Round 3 #1 + #3 (revisit): the Topbar slide drawer was removed
 * entirely. The bell is now a single Link → /notifications full page.
 * Surveyors' "always open" / "duplicates the page" complaint had a
 * common root cause — two notification surfaces (drawer + page) — and
 * the fix is to keep exactly one.
 *
 * This spec asserts the bell behaves as a link, and that the slide-panel
 * drawer is absent from the DOM on both `/` and `/notifications`.
 */

describeFeature('Topbar bell is the only notification entry point', () => {
  test('Scenario: bell links to /notifications and no drawer mounts @smoke', async ({
    page,
    request,
  }) => {
    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await When('they land on the dashboard', async () => {
      await page.goto('/');
      await expect(page.getByTestId('topbar')).toBeVisible();
    });

    await Then(
      'the Topbar notification bell is a link with href="/notifications"',
      async () => {
        const bell = page.getByTestId('notification-bell');
        await expect(bell).toHaveAttribute('href', '/notifications');
      },
    );

    await Then(
      'no slide-panel drawer is mounted on the dashboard',
      async () => {
        await expect(page.getByTestId('slide-panel')).toHaveCount(0);
      },
    );

    await When('they click the bell', async () => {
      await page.getByTestId('notification-bell').click();
      await page.waitForURL('**/notifications');
    });

    await Then(
      'the URL is /notifications and the page heading renders exactly once',
      async () => {
        await expect(page).toHaveURL(/\/notifications$/);
        await expect(
          page.getByRole('heading', { name: 'Notifications', level: 1 }),
        ).toHaveCount(1);
      },
    );

    await Then(
      'still no slide-panel drawer on /notifications',
      async () => {
        await expect(page.getByTestId('slide-panel')).toHaveCount(0);
      },
    );
  });
});
