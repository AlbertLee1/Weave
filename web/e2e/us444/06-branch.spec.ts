import { test, expect, type APIResponse } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 06 — branch.
 *
 * Creates an Ontology branch (US-383) on the seeded northwind ontology,
 * verifies it surfaces in the list, and then closes it. Acts as a
 * smoke test for the branch lifecycle endpoints.
 */
async function expectOK(res: APIResponse, context: string): Promise<void> {
  if (res.ok()) return;

  expect(res.ok(), `${context}: ${res.status()} ${await res.text()}`).toBe(true);
}

test.describe('US-444 — branch', () => {
  test('branch lifecycle: create → list → close', async ({ request }) => {
    await skipWhenBackendDown(request);

    const branchName = uniqueName('us444-branch');
    const created = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches`,
      { data: { name: branchName, description: 'us-444 lifecycle smoke' } },
    );
    await expectOK(created, 'branch create endpoint must be wired');

    const body = (await created.json()) as { id?: string; rid?: string; name?: string };
    const branchId = body.id ?? body.rid ?? branchName;
    expect(typeof branchId).toBe('string');
    expect(body.name).toBe(branchName);

    const list = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches`,
    );
    await expectOK(list, 'branch list endpoint must be wired');

    const listBody = (await list.json()) as { data?: { name: string }[] };
    const names = (listBody.data ?? []).map((b) => b.name);
    expect(names).toContain(branchName);

    const closed = await request.delete(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches/${encodeURIComponent(branchId)}`,
    );
    await expectOK(closed, 'branch close endpoint must be wired');
  });
});
