import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 08 — app builder.
 *
 * Exercises the Workshop-lite Apps surface (US-391) with a minimal
 * 1-row Layout DSL. Creates an App, fetches it back, and tears it down.
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

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
    await expectOK(create, 'app create endpoint must be wired');

    const body = (await create.json()) as { rid?: string; id?: string };
    const rid = body.rid ?? body.id ?? '';
    expect(rid).not.toBe('');

    const got = await request.get(`${API_BASE}/api/v2/apps/${rid}`);
    await expectOK(got, 'app get endpoint must be wired');

    const deleted = await request.delete(`${API_BASE}/api/v2/apps/${rid}`);
    await expectOK(deleted, 'app delete endpoint must be wired');
  });
});
