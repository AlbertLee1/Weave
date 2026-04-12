import { test, expect } from '@playwright/test';

const API_BASE = 'http://localhost:9117';
const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'employee';

/**
 * US-081: Playwright spec — policy-column-hiding.
 *
 * Verifies that the PROPERTY-scope security policy on the Employee object
 * type hides the salary column from peer users (role=viewer) while
 * showing it to manager users (role=editor).
 *
 * Seed fixtures (test/fixtures/seed_northwind):
 *   - Employee object type with properties: employeeId, name, department, salary
 *   - PROPERTY policy granting salary only to editor/admin roles
 *   - manager@test (editor role), peer@test (viewer role)
 *
 * The backend column filter (pkg/oss/service_impl.go applyPropertyVisibility)
 * omits restricted property keys from the WireObject JSON response.
 */
test.describe('Policy column-hiding (US-081)', () => {
  test('manager sees salary in API response, peer does not', async ({
    request,
  }) => {
    // 1. Login as manager (editor role) and fetch employee objects.
    const mgrLogin = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'manager@test', password: 'test1234' },
    });
    expect(mgrLogin.ok(), `login as manager failed: ${mgrLogin.status()}`).toBe(
      true,
    );
    const mgrToken = ((await mgrLogin.json()) as { access_token: string })
      .access_token;

    const mgrRes = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/objects/${OBJECT_TYPE}`,
      { headers: { Authorization: `Bearer ${mgrToken}` } },
    );
    expect(
      mgrRes.ok(),
      `list objects as manager failed: ${mgrRes.status()}`,
    ).toBe(true);
    const mgrBody = (await mgrRes.json()) as {
      data: Record<string, unknown>[];
    };
    expect(mgrBody.data.length).toBeGreaterThan(0);

    // Manager should see the salary property on every row.
    for (const obj of mgrBody.data) {
      expect(obj).toHaveProperty('salary');
    }

    // 2. Login as peer (viewer role) and fetch employee objects.
    const peerLogin = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'peer@test', password: 'test1234' },
    });
    expect(
      peerLogin.ok(),
      `login as peer failed: ${peerLogin.status()}`,
    ).toBe(true);
    const peerToken = ((await peerLogin.json()) as { access_token: string })
      .access_token;

    const peerRes = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/objects/${OBJECT_TYPE}`,
      { headers: { Authorization: `Bearer ${peerToken}` } },
    );
    expect(
      peerRes.ok(),
      `list objects as peer failed: ${peerRes.status()}`,
    ).toBe(true);
    const peerBody = (await peerRes.json()) as {
      data: Record<string, unknown>[];
    };
    expect(peerBody.data.length).toBeGreaterThan(0);

    // Peer should NOT see the salary property on any row.
    for (const obj of peerBody.data) {
      expect(obj).not.toHaveProperty('salary');
    }
  });

  test('manager login → Employee browser → salary data visible', async ({
    page,
  }) => {
    // Login via the UI form.
    await page.goto('/login');
    await page.getByLabel(/email/i).fill('manager@test');
    await page.getByLabel(/password/i).fill('test1234');
    await page.getByRole('button', { name: /sign in/i }).click();

    // Wait for redirect away from /login.
    await page.waitForURL((url) => !url.pathname.includes('/login'), {
      timeout: 10_000,
    });

    // Navigate to the Employee browser page.
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    // Wait for the data table to render.
    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    // Assert at least one salary value is visible in the table.
    // The seed rows have salary values: 120000, 95000, 110000.
    await expect(
      table.getByRole('cell', { name: '120000' }),
    ).toBeVisible({ timeout: 5_000 });
  });

  test('peer login → Employee browser → salary data absent', async ({
    page,
  }) => {
    // Login via the UI form as peer.
    await page.goto('/login');
    await page.getByLabel(/email/i).fill('peer@test');
    await page.getByLabel(/password/i).fill('test1234');
    await page.getByRole('button', { name: /sign in/i }).click();

    // Wait for redirect away from /login.
    await page.waitForURL((url) => !url.pathname.includes('/login'), {
      timeout: 10_000,
    });

    // Navigate to the Employee browser page.
    await page.goto(`/browser/${ONTOLOGY}/${OBJECT_TYPE}`);
    await page.waitForLoadState('domcontentloaded');

    // Wait for the data table to render with data rows.
    const table = page.getByTestId('data-table');
    await expect(table).toBeVisible({ timeout: 10_000 });

    // Verify data rows are present (employee names are visible).
    await expect(
      table.getByRole('cell', { name: 'Alice Chen' }),
    ).toBeVisible({ timeout: 5_000 });

    // Assert salary values are NOT visible — the backend omits the
    // salary property from the WireObject JSON for viewer-role users.
    await expect(
      table.getByRole('cell', { name: '120000' }),
    ).not.toBeVisible();
    await expect(
      table.getByRole('cell', { name: '95000' }),
    ).not.toBeVisible();
    await expect(
      table.getByRole('cell', { name: '110000' }),
    ).not.toBeVisible();
  });
});
