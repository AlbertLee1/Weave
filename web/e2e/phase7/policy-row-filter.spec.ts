import { test, expect } from '@playwright/test';

const API_BASE = 'http://localhost:9117';
const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'customer';

/**
 * US-082: Playwright spec — policy-row-filter.
 *
 * Verifies that the OBJECT-scope markingSubset security policy on the
 * Customer object type filters rows based on the user's held markings.
 *
 * Seed fixtures (test/fixtures/seed_northwind):
 *   - Customer objects with __markings:
 *       ALFKI, BERGS, CHOPS → "ACME"
 *       BLONP, CACTU         → "ACME2"
 *   - OBJECT policy: markingSubset rule on __markings
 *   - acme@test  (viewer role, marking grant: ACME)
 *   - acme2@test (viewer role, marking grant: ACME2)
 *
 * The backend policy engine compiles the markingSubset rule into a Bleve
 * TermQuery that filters objects at query time based on the user's
 * markings (injected into the JWT by the login handler).
 */
test.describe('Policy row-filter (US-082)', () => {
  test('ACME user sees only ACME-tagged customers via API', async ({
    request,
  }) => {
    // Login as acme@test (holds marking ACME).
    const acmeLogin = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'acme@test', password: 'test1234' },
    });
    expect(
      acmeLogin.ok(),
      `login as acme failed: ${acmeLogin.status()}`,
    ).toBe(true);
    const acmeToken = ((await acmeLogin.json()) as { access_token: string })
      .access_token;

    const acmeRes = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/objects/${OBJECT_TYPE}`,
      { headers: { Authorization: `Bearer ${acmeToken}` } },
    );
    expect(
      acmeRes.ok(),
      `list objects as acme failed: ${acmeRes.status()}`,
    ).toBe(true);
    const acmeBody = (await acmeRes.json()) as {
      data: Record<string, unknown>[];
    };

    // ACME user should see exactly 3 customers: ALFKI, BERGS, CHOPS.
    const acmeIDs = acmeBody.data.map((obj) => obj['customerID']);
    expect(acmeIDs).toHaveLength(3);
    expect(acmeIDs.sort()).toEqual(['ALFKI', 'BERGS', 'CHOPS']);
  });

  test('ACME2 user sees only ACME2-tagged customers via API', async ({
    request,
  }) => {
    // Login as acme2@test (holds marking ACME2).
    const acme2Login = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'acme2@test', password: 'test1234' },
    });
    expect(
      acme2Login.ok(),
      `login as acme2 failed: ${acme2Login.status()}`,
    ).toBe(true);
    const acme2Token = ((await acme2Login.json()) as { access_token: string })
      .access_token;

    const acme2Res = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/objects/${OBJECT_TYPE}`,
      { headers: { Authorization: `Bearer ${acme2Token}` } },
    );
    expect(
      acme2Res.ok(),
      `list objects as acme2 failed: ${acme2Res.status()}`,
    ).toBe(true);
    const acme2Body = (await acme2Res.json()) as {
      data: Record<string, unknown>[];
    };

    // ACME2 user should see exactly 2 customers: BLONP, CACTU.
    const acme2IDs = acme2Body.data.map((obj) => obj['customerID']);
    expect(acme2IDs).toHaveLength(2);
    expect(acme2IDs.sort()).toEqual(['BLONP', 'CACTU']);
  });

  test('ACME user → Customer browser → sees only ACME rows', async ({
    page,
    request,
  }) => {
    // Obtain a JWT for acme@test via the API.
    const login = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'acme@test', password: 'test1234' },
    });
    expect(login.ok()).toBe(true);
    const token = ((await login.json()) as { access_token: string })
      .access_token;

    // Intercept all API requests from the browser page and inject the
    // Authorization header so the backend sees the ACME user's markings.
    await page.route('**/api/**', async (route) => {
      const headers = {
        ...route.request().headers(),
        authorization: `Bearer ${token}`,
      };
      await route.continue({ headers });
    });

    // Navigate to the Customer browser page.
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    // Wait for the data table to render.
    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    // ACME-tagged company names should be visible.
    await expect(
      table.getByRole('cell', { name: 'Alfreds Futterkiste' }),
    ).toBeVisible({ timeout: 5_000 });
    await expect(
      table.getByRole('cell', { name: 'Chop-suey Chinese' }),
    ).toBeVisible();

    // ACME2-tagged company names should NOT be visible.
    await expect(
      table.getByRole('cell', { name: 'Cactus Comidas para llevar' }),
    ).not.toBeVisible();
  });

  test('ACME2 user → Customer browser → sees only ACME2 rows', async ({
    page,
    request,
  }) => {
    // Obtain a JWT for acme2@test via the API.
    const login = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'acme2@test', password: 'test1234' },
    });
    expect(login.ok()).toBe(true);
    const token = ((await login.json()) as { access_token: string })
      .access_token;

    // Intercept all API requests from the browser page and inject the
    // Authorization header so the backend sees the ACME2 user's markings.
    await page.route('**/api/**', async (route) => {
      const headers = {
        ...route.request().headers(),
        authorization: `Bearer ${token}`,
      };
      await route.continue({ headers });
    });

    // Navigate to the Customer browser page.
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    // Wait for the data table to render.
    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    // ACME2-tagged company names should be visible.
    await expect(
      table.getByRole('cell', { name: 'Cactus Comidas para llevar' }),
    ).toBeVisible({ timeout: 5_000 });

    // ACME-tagged company names should NOT be visible.
    await expect(
      table.getByRole('cell', { name: 'Alfreds Futterkiste' }),
    ).not.toBeVisible();
    await expect(
      table.getByRole('cell', { name: 'Chop-suey Chinese' }),
    ).not.toBeVisible();
  });
});
