import { describe, expect, it } from 'vitest';
import sseReconnectSpecSource from '../../../e2e/phase7/sse-reconnect.spec.ts?raw';
import optimisticConcurrencySpecSource from '../../../e2e/phase6/optimistic-concurrency.spec.ts?raw';
import us444ActionSpecSource from '../../../e2e/us444/04-action.spec.ts?raw';

describe('Playwright spec contracts', () => {
  it('keeps the Phase 7 SSE reconnect scenario discoverable and deterministic', () => {
    const source = sseReconnectSpecSource;

    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain("getByTestId('search-input')");
    expect(source).toContain(`/api/v2/ontologies/\${ONTOLOGY}/objects/\${OBJECT_TYPE}/search`);
    expect(source).toContain("getByTestId('live-status')");
    expect(source).not.toContain("getByTestId('realtime-indicator')");
  });

  it('keeps the Phase 6 optimistic-concurrency scenario active', () => {
    const source = optimisticConcurrencySpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain("getByTestId('object-version')");
    expect(source).toContain('StaleObject banner');
  });

  it('keeps the US-444 action gate tied to a seeded action dispatcher', () => {
    const source = us444ActionSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).not.toContain('types.length === 0');
    expect(source).not.toContain('types[0].apiName');
    expect(source).toContain("const ACTION_API_NAME = 'createCustomer'");
    expect(source).toContain(
      `/api/v2/ontologies/\${ONTOLOGY}/actions/\${ACTION_API_NAME}/apply`,
    );
    expect(source).toContain('errorCode');
    expect(source).toContain('errorName');
  });
});
