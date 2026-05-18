import { describe, expect, it } from 'vitest';
import sseReconnectSpecSource from '../../../e2e/phase7/sse-reconnect.spec.ts?raw';
import optimisticConcurrencySpecSource from '../../../e2e/phase6/optimistic-concurrency.spec.ts?raw';
import us444ActionSpecSource from '../../../e2e/us444/04-action.spec.ts?raw';
import us444BranchSpecSource from '../../../e2e/us444/06-branch.spec.ts?raw';
import us444MergeSpecSource from '../../../e2e/us444/07-merge.spec.ts?raw';
import us444LineageSpecSource from '../../../e2e/us444/13-lineage-view.spec.ts?raw';

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

  it('keeps the US-444 lineage gate wired to mandatory lineage endpoints', () => {
    const source = us444LineageSpecSource;

    expect(source).not.toMatch(/test\.skip\s*\(/);
    expect(source).not.toMatch(/test\.fixme\s*\(/);
    expect(source).toContain('/objectTypes/customer/fullMetadata');
    expect(source).toContain('/api/v2/lineage/property/${propRid}');
    expect(source).toContain('/api/v2/lineage/dataset-columns/impact?dataset=');
    expect(source).toContain('impacted');
    expect(source).not.toContain('res.ok() || res.status() === 503');
  });

  it('keeps the US-444 branch lifecycle gates wired and skip-free', () => {
    const sources = [us444BranchSpecSource, us444MergeSpecSource];

    for (const source of sources) {
      expect(source).not.toMatch(/test\.skip\s*\(/);
      expect(source).not.toMatch(/test\.fixme\s*\(/);
      expect(source).not.toContain('.status() === 404');
      expect(source).not.toContain(', 404');
      expect(source).not.toContain('404,');
    }

    expect(us444BranchSpecSource).toContain(
      `/api/v2/ontologies/\${ONTOLOGY}/branches`,
    );
    expect(us444BranchSpecSource).toContain('toContain(branchName)');
    expect(us444BranchSpecSource).toContain('delete');

    expect(us444MergeSpecSource).toContain('/diff');
    expect(us444MergeSpecSource).toContain('/merge');
    expect(us444MergeSpecSource).toMatch(/expectOK\(diff,\s*['"]branch diff endpoint must be wired/);
    expect(us444MergeSpecSource).toContain('merge.status()');
    expect(us444MergeSpecSource).toContain('conflictResolution');
  });
});
