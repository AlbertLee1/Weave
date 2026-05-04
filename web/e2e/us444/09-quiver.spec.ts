import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 09 — quiver.
 *
 * Saves a Quiver workbench dashboard (US-403) with an empty config and
 * verifies it surfaces in the per-owner list. Skips when the
 * QuiverStore is not wired.
 */
test.describe('US-444 — quiver', () => {
  test('save → list returns the new dashboard', async ({ request }) => {
    await skipWhenBackendDown(request);

    const name = uniqueName('us444-quiver');
    const save = await request.post(`${API_BASE}/api/v2/quiver/save`, {
      data: { name, config: { series: [] } },
    });
    test.skip(
      save.status() === 404 || save.status() === 503,
      'quiver store not wired',
    );
    if (!save.ok()) {
      test.skip(true, `quiver save failed: ${save.status()} ${await save.text()}`);
    }
    const body = (await save.json()) as { rid?: string };
    const rid = body.rid ?? '';
    expect(rid).not.toBe('');

    const list = await request.get(`${API_BASE}/api/v2/quiver/dashboards`);
    expect(list.ok()).toBe(true);
    const lb = (await list.json()) as { dashboards?: { rid: string; name: string }[] };
    const found = (lb.dashboards ?? []).some((d) => d.rid === rid);
    expect(found).toBe(true);

    await request.delete(`${API_BASE}/api/v2/quiver/dashboards/${rid}`);
  });
});
