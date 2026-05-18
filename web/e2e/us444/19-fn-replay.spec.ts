import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 19 — function replay.
 *
 * Verifies the deterministic replay endpoint (US-370). Publishes a
 * deterministic Function, executes it once to record an
 * input/output hash pair, then replays at the captured version. The
 * replay must succeed (200) without surfacing the
 * `WEAVE_FUNCTION_NONDETERMINISTIC` warning code.
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

test.describe('US-444 — function replay', () => {
  test('publish → execute → replay returns the same output hash', async ({ request }) => {
    await skipWhenBackendDown(request);

    const fnName = uniqueName('us444Replay');
    let rid = '';
    const create = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/functions`,
      {
        data: {
          name: fnName,
          version: '1.0.0',
          sourceCode: 'function main(input){return {echo: input.parameters.x};}',
          signature: { input: {}, output: {} },
        },
      },
    );
    await expectOK(create, 'function create endpoint must be wired');

    try {
      const body = (await create.json()) as { rid?: string };
      rid = body.rid ?? '';
      expect(rid).not.toBe('');

      const exec = await request.post(
        `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(rid)}/execute`,
        { data: { parameters: { x: 42 } } },
      );
      await expectOK(exec, 'function execute endpoint must be wired');

      const replay = await request.post(
        `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/functions/${encodeURIComponent(rid)}/replay`,
        { data: { input: { x: 42 }, version: '1.0.0' } },
      );
      await expectOK(replay, 'function replay endpoint must be wired');
      const text = await replay.text();
      expect(text).not.toContain('WEAVE_FUNCTION_NONDETERMINISTIC');
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
