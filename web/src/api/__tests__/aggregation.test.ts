import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { aggregate } from '../aggregation';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('aggregation API', () => {
  it('aggregate() POSTs to correct URL with body', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Employee/aggregate',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.aggregation).toEqual([{ type: 'count' }]);
          return HttpResponse.json({
            data: [{ metrics: { count: 42 } }],
          });
        },
      ),
    );

    const result = await aggregate('test', 'Employee', {
      aggregation: [{ type: 'count' }],
    });
    expect(result.data).toHaveLength(1);
    expect(result.data[0].metrics.count).toBe(42);
  });

  it('aggregate() supports groupBy', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Employee/aggregate',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.groupBy).toEqual([{ field: 'department', type: 'exact' }]);
          return HttpResponse.json({
            data: [
              { group: { department: 'Engineering' }, metrics: { count: 10 } },
              { group: { department: 'Sales' }, metrics: { count: 5 } },
            ],
          });
        },
      ),
    );

    const result = await aggregate('test', 'Employee', {
      aggregation: [{ type: 'count' }],
      groupBy: [{ field: 'department', type: 'exact' }],
    });
    expect(result.data).toHaveLength(2);
  });
});
