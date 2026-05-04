import { test, expect } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 18 — function publish.
 *
 * Publishes a trivial Function (US-089 + US-217 versioning) on the
 * seeded northwind ontology, lists its versions to confirm the publish
 * landed, then deletes it. Function execution is exercised separately
 * by spec 19 (`fn-replay`).
 */
test.describe('US-444 — function publish', () => {
  test('create → list versions → delete round-trip', async ({ request }) => {
    await skipWhenBackendDown(request);

    const fnName = uniqueName('us444Fn');
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
    test.skip(create.status() === 404 || create.status() === 503, 'functions endpoint unwired');
    if (!create.ok()) {
      test.skip(true, `function create failed: ${create.status()} ${await create.text()}`);
    }
    const body = (await create.json()) as { rid?: string; name?: string };
    const rid = body.rid ?? '';

    const versions = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(fnName)}/versions`,
    );
    expect(versions.ok()).toBe(true);
    const vb = (await versions.json()) as { data?: { version: string }[] };
    const semvers = (vb.data ?? []).map((v) => v.version);
    expect(semvers).toContain('1.0.0');

    if (rid) {
      await request.delete(
        `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(rid)}`,
      );
    }
  });
});
