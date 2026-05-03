import { describe, it, expect, beforeEach, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { extractOntologyApiName, request, withActiveBranch } from '../client';
import { useBranchStore, DEFAULT_BRANCH } from '../../stores/branchStore';

const server = setupServer();

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

beforeEach(() => {
  useBranchStore.setState({ selections: {} });
});

describe('extractOntologyApiName', () => {
  it('matches /api/v2/ontologies/{name}/...', () => {
    expect(extractOntologyApiName('/api/v2/ontologies/foundry/objectTypes')).toBe('foundry');
  });

  it('matches /api/admin/ontologies/{name}/...', () => {
    expect(extractOntologyApiName('/api/admin/ontologies/foundry/branches')).toBe('foundry');
  });

  it('returns null for non-ontology paths', () => {
    expect(extractOntologyApiName('/api/v2/notifications')).toBeNull();
    expect(extractOntologyApiName('/api/auth/login')).toBeNull();
    expect(extractOntologyApiName('/health')).toBeNull();
  });

  it('returns null when ontology segment is empty', () => {
    expect(extractOntologyApiName('/api/v2/ontologies/')).toBeNull();
  });
});

describe('withActiveBranch', () => {
  it('leaves non-ontology paths untouched', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    expect(withActiveBranch('/api/v2/notifications')).toBe('/api/v2/notifications');
  });

  it('does not inject branch when default is selected', () => {
    expect(withActiveBranch('/api/v2/ontologies/foundry/objectTypes')).toBe(
      '/api/v2/ontologies/foundry/objectTypes',
    );
  });

  it('injects ?branch= when a non-default branch is active for the ontology', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    expect(withActiveBranch('/api/v2/ontologies/foundry/objectTypes')).toBe(
      '/api/v2/ontologies/foundry/objectTypes?branch=feature-x',
    );
  });

  it('preserves existing query parameters when injecting branch', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    const out = withActiveBranch('/api/v2/ontologies/foundry/objects?pageSize=50');
    expect(out).toContain('pageSize=50');
    expect(out).toContain('branch=feature-x');
  });

  it('honours an explicit ?branch= override on the path', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    expect(
      withActiveBranch('/api/v2/ontologies/foundry/objectTypes?branch=other'),
    ).toBe('/api/v2/ontologies/foundry/objectTypes?branch=other');
  });

  it('uses per-ontology selection (does not leak across ontologies)', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    expect(withActiveBranch('/api/v2/ontologies/other/objectTypes')).toBe(
      '/api/v2/ontologies/other/objectTypes',
    );
  });

  it('admin ontology paths inherit the same branch selection', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    expect(withActiveBranch('/api/admin/ontologies/foundry/objectTypes')).toBe(
      '/api/admin/ontologies/foundry/objectTypes?branch=feature-x',
    );
  });

  it('clearBranch reverts to default behaviour (no injection)', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    useBranchStore.getState().clearBranch('foundry');
    expect(withActiveBranch('/api/v2/ontologies/foundry/objectTypes')).toBe(
      '/api/v2/ontologies/foundry/objectTypes',
    );
  });

  it('treats DEFAULT_BRANCH explicitly as a clear', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    useBranchStore.getState().setBranch('foundry', DEFAULT_BRANCH);
    expect(withActiveBranch('/api/v2/ontologies/foundry/objectTypes')).toBe(
      '/api/v2/ontologies/foundry/objectTypes',
    );
  });
});

describe('request() branch injection (e2e through msw)', () => {
  it('GET to ontology surface includes ?branch= when store has a selection', async () => {
    let observedUrl = '';
    server.use(
      http.get('/api/v2/ontologies/foundry/objectTypes', ({ request: req }) => {
        observedUrl = req.url;
        return HttpResponse.json({ data: [] });
      }),
    );

    useBranchStore.getState().setBranch('foundry', 'feature-x');
    await request<{ data: unknown[] }>('GET', '/api/v2/ontologies/foundry/objectTypes');
    expect(observedUrl).toContain('branch=feature-x');
  });

  it('non-ontology surface is left untouched', async () => {
    let observedUrl = '';
    server.use(
      http.get('/api/v2/notifications', ({ request: req }) => {
        observedUrl = req.url;
        return HttpResponse.json({ data: [] });
      }),
    );

    useBranchStore.getState().setBranch('foundry', 'feature-x');
    await request<{ data: unknown[] }>('GET', '/api/v2/notifications');
    expect(observedUrl).not.toContain('branch=');
  });
});
