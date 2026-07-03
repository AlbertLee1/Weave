import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { applyAction, applyBatch } from '../actions';

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
              modifiedObjectsCount: 0,
              deletedObjectsCount: 0,
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
              modifiedObjectsCount: 0,
              deletedObjectsCount: 0,
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

  it('applyAction() sends options when provided', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/actions/validateMe/apply',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          const opts = body.options as Record<string, string>;
          expect(opts.mode).toBe('VALIDATE_ONLY');
          // Foundry parity: apply's validation payload is the full
          // ValidateActionResponseV2 shape (result + submissionCriteria +
          // per-parameter map), same as the dedicated /validate endpoint.
          return HttpResponse.json({
            validation: {
              result: 'VALID',
              submissionCriteria: [],
              parameters: {
                name: {
                  result: 'VALID',
                  required: true,
                  evaluatedConstraints: [],
                },
              },
            },
          });
        },
      ),
    );

    const result = await applyAction('test', 'validateMe', {
      parameters: { name: 'Test' },
      options: { mode: 'VALIDATE_ONLY' },
    });
    expect(result.validation).toBeDefined();
    expect(result.validation!.result).toBe('VALID');
    expect(result.validation!.submissionCriteria).toEqual([]);
    expect(result.validation!.parameters.name.required).toBe(true);
    expect(result.validation!.parameters.name.result).toBe('VALID');
    expect(result.edits).toBeUndefined();
  });

  it('applyBatch() POSTs batch request to correct URL', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/actions/createEmployee/applyBatch',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.requests).toHaveLength(2);
          return HttpResponse.json({
            edits: {
              type: 'edits',
              addedObjectCount: 2,
              modifiedObjectsCount: 0,
              deletedObjectsCount: 0,
              addedLinksCount: 0,
              deletedLinksCount: 0,
            },
          });
        },
      ),
    );

    const result = await applyBatch('test', 'createEmployee', {
      requests: [
        { parameters: { name: 'Alice' } },
        { parameters: { name: 'Bob' } },
      ],
    });
    expect(result.edits).toBeDefined();
    expect(result.edits!.addedObjectCount).toBe(2);
  });
});
