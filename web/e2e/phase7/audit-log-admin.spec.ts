import { test, expect } from '@playwright/test';

const API_BASE = 'http://localhost:9117';

/**
 * US-083: Playwright spec — audit-log-admin.
 *
 * Verifies that only admin-level users can access the audit events
 * endpoint (`GET /api/v2/admin/auditEvents`), and that non-admin
 * (peer/viewer) users receive a 403 Forbidden response.
 *
 * Seed fixtures (test/fixtures/seed_northwind):
 *   - admin@test   (admin role)  → can view audit events
 *   - peer@test    (viewer role) → denied access (403)
 */
test.describe('Audit log admin access (US-083)', () => {
  test('admin user can list audit events via API', async ({ request }) => {
    // Login as admin@test (admin role).
    const adminLogin = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'admin@test', password: 'test1234' },
    });
    expect(
      adminLogin.ok(),
      `login as admin failed: ${adminLogin.status()}`,
    ).toBe(true);
    const adminToken = ((await adminLogin.json()) as { access_token: string })
      .access_token;

    // Fetch audit events — admin should receive 200.
    const auditRes = await request.get(
      `${API_BASE}/api/v2/admin/auditEvents`,
      { headers: { Authorization: `Bearer ${adminToken}` } },
    );
    expect(
      auditRes.ok(),
      `audit events request failed: ${auditRes.status()}`,
    ).toBe(true);

    const body = (await auditRes.json()) as {
      data: Array<Record<string, unknown>>;
      nextPageToken?: string;
    };

    // The response must contain a data array (may be empty on a fresh seed,
    // but the login itself generates audit events).
    expect(Array.isArray(body.data)).toBe(true);
  });

  test('admin sees audit entries with expected fields', async ({ request }) => {
    // Login as admin.
    const login = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'admin@test', password: 'test1234' },
    });
    expect(login.ok()).toBe(true);
    const token = ((await login.json()) as { access_token: string })
      .access_token;

    const auditRes = await request.get(
      `${API_BASE}/api/v2/admin/auditEvents`,
      { headers: { Authorization: `Bearer ${token}` } },
    );
    expect(auditRes.ok()).toBe(true);

    const body = (await auditRes.json()) as {
      data: Array<Record<string, unknown>>;
    };

    expect(
      Array.isArray(body.data),
      'audit events response must return data rows',
    ).toBe(true);
    expect(body.data.length).toBeGreaterThan(0);

    const entry = body.data[0];
    expect(entry).toHaveProperty('id');
    expect(entry).toHaveProperty('action');
    expect(entry).toHaveProperty('timestamp');
  });

  test('peer (viewer) user is denied access to audit events', async ({
    request,
  }) => {
    // Login as peer@test (viewer role).
    const peerLogin = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'peer@test', password: 'test1234' },
    });
    expect(
      peerLogin.ok(),
      `login as peer failed: ${peerLogin.status()}`,
    ).toBe(true);
    const peerToken = ((await peerLogin.json()) as { access_token: string })
      .access_token;

    // Attempt to fetch audit events — viewer should receive 403.
    const auditRes = await request.get(
      `${API_BASE}/api/v2/admin/auditEvents`,
      { headers: { Authorization: `Bearer ${peerToken}` } },
    );
    expect(auditRes.status()).toBe(403);
  });

  test('admin can filter audit events by action', async ({ request }) => {
    // Login as admin.
    const login = await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: 'admin@test', password: 'test1234' },
    });
    expect(login.ok()).toBe(true);
    const token = ((await login.json()) as { access_token: string })
      .access_token;

    // Filter by action=login_success — the admin's own login should appear.
    const auditRes = await request.get(
      `${API_BASE}/api/v2/admin/auditEvents?action=login_success`,
      { headers: { Authorization: `Bearer ${token}` } },
    );
    expect(auditRes.ok()).toBe(true);

    const body = (await auditRes.json()) as {
      data: Array<Record<string, unknown>>;
    };
    expect(Array.isArray(body.data)).toBe(true);
    expect(body.data.length).toBeGreaterThan(0);

    // Every returned entry must have action == login_success.
    for (const entry of body.data) {
      expect(entry.action).toBe('login_success');
    }
  });
});
