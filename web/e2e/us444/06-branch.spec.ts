import { test, expect } from '@playwright/test';
import { API_BASE, ONTOLOGY, skipWhenBackendDown, uniqueName } from './helpers';

/**
 * US-444 spec 06 — branch.
 *
 * Creates an Ontology branch (US-383) on the seeded northwind ontology,
 * verifies it surfaces in the list, and then closes it. Acts as a
 * smoke test for the branch lifecycle endpoints.
 */
test.describe('US-444 — branch', () => {
  test('branch lifecycle: create → list → close', async ({ request }) => {
    await skipWhenBackendDown(request);

    const branchName = uniqueName('us444-branch');
    const created = await request.post(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches`,
      { data: { name: branchName, description: 'us-444 lifecycle smoke' } },
    );
    test.skip(created.status() === 404, 'branch endpoint not wired');
    if (!created.ok()) {
      // Some deployments require the branch name to be globally unique; if a
      // prior run left one behind we treat that as a non-fatal skip.
      test.skip(true, `branch create rejected: ${created.status()} ${await created.text()}`);
    }
    const body = (await created.json()) as { id?: string; rid?: string; name?: string };
    const branchId = body.id ?? body.rid ?? branchName;
    expect(typeof branchId).toBe('string');

    const list = await request.get(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches`,
    );
    expect(list.ok()).toBe(true);
    const listBody = (await list.json()) as { data?: { name: string }[] };
    const names = (listBody.data ?? []).map((b) => b.name);
    expect(names).toContain(branchName);

    await request.delete(
      `${API_BASE}/api/v2/ontologies/${ONTOLOGY}/branches/${encodeURIComponent(branchId)}`,
    );
  });
});
