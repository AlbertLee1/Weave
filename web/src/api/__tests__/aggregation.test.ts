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

  // US-039: backend wire format ships metrics as MetricValue[] and exposes a
  // top-level `accuracy` + `excludedItems` marker. The client must normalise
  // the array shape to a Record<string, number> so UI code keeps working,
  // and surface accuracy/excludedItems for AccuracyBadge rendering.
  it('aggregate() normalises MetricValue[] and surfaces accuracy + excludedItems', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Order/aggregate',
        async () =>
          HttpResponse.json({
            excludedItems: 0,
            accuracy: 'ACCURATE',
            data: [
              {
                group: { shipCountry: 'germany', priceBucket: 0, quarter: 1 },
                metrics: [
                  { name: 'count', value: 12 },
                  { name: 'totalFreight', value: 384.5 },
                ],
              },
              {
                group: { shipCountry: 'sweden', priceBucket: 20, quarter: 2 },
                metrics: [{ name: 'count', value: 7 }],
              },
            ],
          }),
      ),
    );

    const result = await aggregate('test', 'Order', {
      aggregation: [
        { type: 'count', name: 'count' },
        { type: 'sum', field: 'freight', name: 'totalFreight' },
      ],
      groupBy: [
        { field: 'shipCountry', type: 'exact' },
        { field: 'freight', type: 'fixedWidth' },
        { field: 'orderDate', type: 'exact' },
      ],
    });

    expect(result.accuracy).toBe('ACCURATE');
    expect(result.excludedItems).toBe(0);
    expect(result.data).toHaveLength(2);
    expect(result.data[0].metrics.count).toBe(12);
    expect(result.data[0].metrics.totalFreight).toBeCloseTo(384.5);
    expect(result.data[1].metrics.count).toBe(7);
  });

  it('aggregate() passes through record-shaped metrics unchanged', async () => {
    server.use(
      http.post('/api/v2/ontologies/test/objects/Legacy/aggregate', async () =>
        HttpResponse.json({
          accuracy: 'APPROXIMATE',
          data: [{ metrics: { count: 99 } }],
        }),
      ),
    );

    const result = await aggregate('test', 'Legacy', { aggregation: [{ type: 'count' }] });
    expect(result.accuracy).toBe('APPROXIMATE');
    expect(result.data[0].metrics.count).toBe(99);
  });
});
