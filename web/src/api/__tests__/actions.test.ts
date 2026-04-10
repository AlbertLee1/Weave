import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { applyAction } from '../actions';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('actions API', () => {
  it('applyAction() POSTs to path /{action}/apply and returns SyncApplyActionResponseV2', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/actions/createEmployee/apply',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          // Foundry OSv2: action API name lives in the URL, not the body.
          expect(body).not.toHaveProperty('actionType');
          expect(body.parameters).toEqual({ name: 'Alice' });
          return HttpResponse.json({
            operationId: 'op-123',
            edits: {
              type: 'edits',
              addedObjectCount: 1,
              modifiedObjectCount: 0,
              deletedObjectCount: 0,
              addedLinksCount: 0,
              deletedLinksCount: 0,
            },
          });
        },
      ),
    );

    const result = await applyAction('test', 'createEmployee', {
      parameters: { name: 'Alice' },
    });
    expect(result.operationId).toBe('op-123');
    expect(result.edits).toBeDefined();
    expect(result.edits!.type).toBe('edits');
    expect(result.edits!.addedObjectCount).toBe(1);
  });

  it('applyAction() percent-encodes the action name', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/actions/weird%20action/apply',
        async () =>
          HttpResponse.json({
            edits: {
              type: 'edits',
              addedObjectCount: 0,
              modifiedObjectCount: 0,
              deletedObjectCount: 0,
              addedLinksCount: 0,
              deletedLinksCount: 0,
            },
          }),
      ),
    );

    const result = await applyAction('test', 'weird action', { parameters: {} });
    expect(result.edits).toBeDefined();
    expect(result.edits!.type).toBe('edits');
  });
});
