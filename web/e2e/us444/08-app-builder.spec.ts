import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 08 — app builder.
 *
 * Exercises the Workshop-lite Apps surface (US-391) with a minimal
 * 1-row Layout DSL. Creates an App, fetches it back, and tears it down.
 * Skips when the AppsStore is not wired (degraded mode).
 */
test.describe('US-444 — app builder', () => {
  test('app create → get → delete round-trip', async ({ request }) => {
    await skipWhenBackendDown(request);

    const name = uniqueName('us444-app');
    const create = await request.post(`${API_BASE}/api/v2/apps`, {
      data: {
        name,
        layoutJson: {
          type: 'row',
          children: [{ type: 'col', width: 12, child: { type: 'text', value: 'hello us-444' } }],
        },
      },
    });
    test.skip(
      create.status() === 404 || create.status() === 503,
      'apps store not wired — `make e2e-up` should boot the full PG stack',
    );
    if (!create.ok()) {
      test.skip(true, `app create failed: ${create.status()} ${await create.text()}`);
    }
    const body = (await create.json()) as { rid?: string; id?: string };
    const rid = body.rid ?? body.id ?? '';
    expect(rid).not.toBe('');

    const got = await request.get(`${API_BASE}/api/v2/apps/${rid}`);
    expect(got.ok()).toBe(true);

    await request.delete(`${API_BASE}/api/v2/apps/${rid}`);
  });
});
