import { test, expect } from '@playwright/test';
import { API_BASE, skipWhenBackendDown } from './helpers';

/**
 * US-444 spec 12 — SDK mock contract surface.
 *
 * Verifies the canonical OpenAPI surface that powers `weave-mock` and
 * the multi-language quickstart contract suite (US-423). The Weave
 * server serves the same spec at `/api/openapi.yaml` (US-422) so the
 * spec-derived mock and the live server agree on the wire format.
 */
test.describe('US-444 — sdk mock contract', () => {
  test('canonical openapi spec is served', async ({ request }) => {
    await skipWhenBackendDown(request);

    const res = await request.get(`${API_BASE}/api/openapi.yaml`);
    expect(res.ok(), `openapi spec request failed: ${res.status()}`).toBe(true);
    const body = await res.text();
    expect(body).toContain('openapi:');
    expect(body).toMatch(/paths\s*:/);
    // Every quickstart asserts the customer endpoint shape; pin its path
    // so a future spec rewrite can't silently break the SDKs.
    expect(body).toContain('/objects/{objectType}');
  });
});
