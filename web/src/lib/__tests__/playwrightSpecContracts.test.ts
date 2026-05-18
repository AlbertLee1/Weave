import { describe, expect, it } from 'vitest';
import sseReconnectSpecSource from '../../../e2e/phase7/sse-reconnect.spec.ts?raw';

describe('Playwright spec contracts', () => {
  it('keeps the Phase 7 SSE reconnect scenario discoverable and deterministic', () => {
    const source = sseReconnectSpecSource;

    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain("getByTestId('search-input')");
    expect(source).toContain(`/api/v2/ontologies/\${ONTOLOGY}/objects/\${OBJECT_TYPE}/search`);
    expect(source).toContain("getByTestId('live-status')");
    expect(source).not.toContain("getByTestId('realtime-indicator')");
  });
});
