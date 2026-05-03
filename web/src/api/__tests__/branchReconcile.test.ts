import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  MergeBranchConflictError,
  mergeBranch,
  postBranchDiff,
} from '../ontologies';
import { ApiRequestError } from '../client';

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

const branch = {
  id: 'br-x',
  ontologyRid: 'ri.ontology.foundry',
  name: 'feature-x',
  baseVersion: 1,
  status: 'open' as const,
  createdBy: 'tester',
  createdAt: '2026-05-01T00:00:00Z',
  updatedAt: '2026-05-01T00:00:00Z',
};

describe('branch reconcile API helpers (US-387)', () => {
  it('postBranchDiff posts and returns the categorised diff', async () => {
    server.use(
      http.post('/api/v2/ontologies/foundry/branches/br-x/diff', () =>
        HttpResponse.json({
          branch,
          added: [],
          modified: [],
          deleted: [],
          conflicts: [],
          hasConflicts: false,
        }),
      ),
    );
    const resp = await postBranchDiff('foundry', 'br-x');
    expect(resp.branch.id).toBe('br-x');
    expect(resp.hasConflicts).toBe(false);
  });

  it('mergeBranch returns the success envelope on 200', async () => {
    server.use(
      http.post('/api/v2/ontologies/foundry/branches/br-x/merge', () =>
        HttpResponse.json({
          branch: { ...branch, status: 'merged' },
          appliedCount: 2,
          skippedCount: 1,
        }),
      ),
    );
    const resp = await mergeBranch('foundry', 'br-x', {
      conflictResolution: { 'objectType:X': 'use-branch' },
    });
    expect(resp.appliedCount).toBe(2);
    expect(resp.branch.status).toBe('merged');
  });

  it('mergeBranch throws MergeBranchConflictError on a 409 MERGE_CONFLICT body', async () => {
    server.use(
      http.post('/api/v2/ontologies/foundry/branches/br-x/merge', () =>
        HttpResponse.json(
          {
            errorCode: 'MERGE_CONFLICT',
            conflicts: [
              {
                entityType: 'objectType',
                entityRid: 'ri.x',
                apiName: 'X',
                resolutionKey: 'objectType:X',
                changeType: 'MODIFIED',
                branchState: { apiName: 'X' },
                mainState: { apiName: 'X' },
              },
            ],
            unresolved: [
              {
                entityType: 'objectType',
                entityRid: 'ri.x',
                apiName: 'X',
                resolutionKey: 'objectType:X',
                changeType: 'MODIFIED',
                branchState: { apiName: 'X' },
                mainState: { apiName: 'X' },
              },
            ],
          },
          { status: 409 },
        ),
      ),
    );
    await expect(
      mergeBranch('foundry', 'br-x', {}),
    ).rejects.toBeInstanceOf(MergeBranchConflictError);
  });

  it('mergeBranch surfaces non-409 failures as ApiRequestError', async () => {
    server.use(
      http.post('/api/v2/ontologies/foundry/branches/br-x/merge', () =>
        HttpResponse.json(
          {
            errorCode: 'BranchNotFound',
            errorName: 'BranchNotFound',
            errorInstanceId: 'instance-1',
          },
          { status: 404 },
        ),
      ),
    );
    await expect(
      mergeBranch('foundry', 'br-x', {}),
    ).rejects.toBeInstanceOf(ApiRequestError);
  });
});
