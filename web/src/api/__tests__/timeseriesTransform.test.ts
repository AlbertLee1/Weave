import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { transformTimeSeries, type TransformSpec } from '../timeseries';

// Unit 12 C6 — transform-chain endpoint. These tests pin the POST body
// the SPA sends (a source descriptor + an ordered transforms array) and
// the `{points:[…]}` envelope it expects back.

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('transformTimeSeries API', () => {
  it('POSTs the source + transform chain to /timeseries/transform', async () => {
    let capturedBody: unknown = null;
    let capturedPath = '';
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/timeseries/transform',
        async ({ request: req }) => {
          capturedBody = await req.json();
          capturedPath = new URL(req.url).pathname;
          return HttpResponse.json({
            points: [
              { time: '2026-01-01T00:00:00Z', value: 10 },
              { time: '2026-01-01T01:00:00Z', value: 20 },
            ],
          });
        },
      ),
    );

    const transforms: TransformSpec[] = [
      { op: 'resample', params: { interval: '1h', agg: 'avg' } },
    ];
    const resp = await transformTimeSeries('demo', {
      source: { objectType: 'Host', primaryKey: 'h1', property: 'cpu' },
      transforms,
    });

    expect(capturedPath).toBe('/api/v2/ontologies/demo/timeseries/transform');
    expect(capturedBody).toEqual({
      source: { objectType: 'Host', primaryKey: 'h1', property: 'cpu' },
      transforms: [{ op: 'resample', params: { interval: '1h', agg: 'avg' } }],
    });
    expect(resp.points).toHaveLength(2);
    expect(resp.points[1].value).toBe(20);
  });

  it('supports inline points instead of a source descriptor', async () => {
    let capturedBody: unknown = null;
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/timeseries/transform',
        async ({ request: req }) => {
          capturedBody = await req.json();
          return HttpResponse.json({ points: [] });
        },
      ),
    );

    await transformTimeSeries('demo', {
      points: [{ time: '2026-01-01T00:00:00Z', value: 1 }],
      transforms: [{ op: 'diff' }],
    });

    expect(capturedBody).toEqual({
      points: [{ time: '2026-01-01T00:00:00Z', value: 1 }],
      transforms: [{ op: 'diff' }],
    });
  });

  it('surfaces a 400 invalid-step error from the backend', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/timeseries/transform',
        () =>
          HttpResponse.json(
            {
              errorCode: 'INVALID_ARGUMENT',
              errorName: 'TimeSeriesTransformInvalidStep',
              errorInstanceId: 'x',
              parameters: { reason: 'bad' },
            },
            { status: 400 },
          ),
      ),
    );

    await expect(
      transformTimeSeries('demo', {
        points: [{ time: '2026-01-01T00:00:00Z', value: 1 }],
        transforms: [{ op: 'resample', params: { interval: 'nope' } }],
      }),
    ).rejects.toMatchObject({ errorName: 'TimeSeriesTransformInvalidStep' });
  });
});
