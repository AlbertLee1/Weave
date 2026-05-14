import { expect, test } from '@playwright/test';
import { Given, Then, When, describeFeature, signIn } from './support';

/**
 * Dogfood Round 3 #1 + #3: the Notification drawer used to remain in the
 * DOM (translated off-screen) on every route, AND a second copy was
 * mounted by the Topbar even when the user was already standing on the
 * dedicated `/notifications` full page. Both surveys flagged it as a
 * duplicate / "looks open again" bug.
 *
 * This spec navigates from `/` → `/notifications` and asserts:
 *
 *  - the Topbar's slide-panel is NOT present in the DOM on /notifications
 *    (component is conditionally rendered, not just CSS-hidden),
 *  - the "Notifications" h1 heading from the full page appears exactly
 *    once (proving the duplicate Topbar drawer is gone).
 */

describeFeature('Notifications drawer does not duplicate on /notifications', () => {
  test('Scenario: navigating to /notifications removes the Topbar slide-panel @smoke', async ({
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

    await When('they navigate to /notifications', async () => {
      await page.goto('/notifications');
    });

    await Then(
      'the Topbar notification slide-panel is not present in the DOM',
      async () => {
        await expect(page.getByTestId('slide-panel')).toHaveCount(0);
      },
    );

    await Then(
      'the Notifications heading appears exactly once',
      async () => {
        await expect(
          page.getByRole('heading', { name: 'Notifications', level: 1 }),
        ).toHaveCount(1);
      },
    );
  });
});
