import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 09 — quiver.
 *
 * Saves a Quiver workbench dashboard (US-403) with an empty config and
 * verifies it surfaces in the per-owner list before teardown.
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

test.describe('US-444 — quiver', () => {
  test('save → list returns the new dashboard', async ({ request }) => {
    await skipWhenBackendDown(request);

    const name = uniqueName('us444-quiver');
    const save = await request.post(`${API_BASE}/api/v2/quiver/save`, {
      data: { name, config: { series: [] } },
    });
    await expectOK(save, 'quiver save endpoint must be wired');

    const body = (await save.json()) as { rid?: string };
    const rid = body.rid ?? '';
    expect(rid).not.toBe('');

    const list = await request.get(`${API_BASE}/api/v2/quiver/dashboards`);
    await expectOK(list, 'quiver list endpoint must be wired');
    const lb = (await list.json()) as { dashboards?: { rid: string; name: string }[] };
    const found = (lb.dashboards ?? []).some((d) => d.rid === rid);
    expect(found).toBe(true);

    const deleted = await request.delete(`${API_BASE}/api/v2/quiver/dashboards/${rid}`);
    await expectOK(deleted, 'quiver delete endpoint must be wired');
  });
});
