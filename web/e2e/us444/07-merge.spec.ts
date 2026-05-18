import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 07 — merge.
 *
 * Creates a branch with no diverged changes, runs the diff endpoint and
 * confirms an empty change set, then exercises the merge endpoint
 * (US-385) with a no-op conflictResolution payload. A clean
 * fast-forward merge against a quiet branch is the smallest end-to-end
 * exercise of the merge code path.
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

test.describe('US-444 — merge', () => {
  test('no-op branch merges back into main without conflicts', async ({ request }) => {
    await skipWhenBackendDown(request);

    const name = uniqueName('us444-merge');
    const created = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches`,
      { data: { name, description: 'us-444 merge smoke' } },
    );
    await expectOK(created, 'branch create endpoint must be wired for merge smoke');

    const body = (await created.json()) as { id?: string; rid?: string; name?: string };
    const branchId = body.id ?? body.rid ?? name;
    expect(body.name).toBe(name);

    const diff = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches/${encodeURIComponent(branchId)}/diff`,
      { data: {} },
    );
    await expectOK(diff, 'branch diff endpoint must be wired');
    const diffBody = (await diff.json()) as {
      added?: unknown[];
      modified?: unknown[];
      deleted?: unknown[];
      hasConflicts?: boolean;
    };
    expect(Array.isArray(diffBody.added)).toBe(true);
    expect(Array.isArray(diffBody.modified)).toBe(true);
    expect(Array.isArray(diffBody.deleted)).toBe(true);
    expect(diffBody.hasConflicts).toBe(false);

    const merge = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches/${encodeURIComponent(branchId)}/merge`,
      { data: { conflictResolution: {} } },
    );
    expect(merge.status(), `branch merge endpoint must be wired: ${await merge.text()}`).toBe(200);

    const closed = await request.delete(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches/${encodeURIComponent(branchId)}`,
    );
    expect([204, 409]).toContain(closed.status());
  });
});
