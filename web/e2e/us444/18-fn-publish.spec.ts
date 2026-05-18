import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 18 — function publish.
 *
 * Publishes a trivial Function (US-089 + US-217 versioning) on the
 * seeded northwind ontology, lists its versions to confirm the publish
 * landed, then deletes it. Function execution is exercised separately
 * by spec 19 (`fn-replay`).
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

test.describe('US-444 — function publish', () => {
  test('create → list versions → delete round-trip', async ({ request }) => {
    await skipWhenBackendDown(request);

    const fnName = uniqueName('us444Fn');
    let rid = '';
    const create = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/functions`,
      {
        data: {
          name: fnName,
          version: '1.0.0',
          sourceCode: 'function main(input){return {ok: true, name: "us-444"};}',
          signature: { input: {}, output: {} },
        },
      },
    );
    await expectOK(create, 'function create endpoint must be wired');

    try {
      const body = (await create.json()) as { rid?: string; name?: string };
      rid = body.rid ?? '';
      expect(rid).not.toBe('');

      const versions = await request.get(
        `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(fnName)}/versions`,
      );
      await expectOK(versions, 'function versions endpoint must be wired');
      const vb = (await versions.json()) as { data?: { version: string }[] };
      const semvers = (vb.data ?? []).map((v) => v.version);
      expect(semvers).toContain('1.0.0');
    } finally {
      if (rid) {
        const deleted = await request.delete(
          `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(rid)}`,
        );
        await expectOK(deleted, 'function delete endpoint must be wired');
      }
    }
  });
});
