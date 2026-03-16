import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { applyAction } from '../actions';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('actions API', () => {
  it('applyAction() POSTs to correct URL with body', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/actions/apply',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.actionType).toBe('createEmployee');
          expect(body.parameters).toEqual({ name: 'Alice' });
          return HttpResponse.json({
            edits: [
              {
                type: 'addObject',
                objectType: 'Employee',
                primaryKey: '1',
                properties: { name: 'Alice' },
              },
            ],
          });
        },
      ),
    );

    const result = await applyAction('test', {
      actionType: 'createEmployee',
      parameters: { name: 'Alice' },
    });
    expect(result.edits).toHaveLength(1);
    expect(result.edits![0].type).toBe('addObject');
  });
});
