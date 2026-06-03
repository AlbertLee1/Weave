import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { getQuiverData } from '../quiver';

// Unit 12 C1 — windowed /data read surface for a saved Quiver dashboard.
// These tests pin the wire contract from the SPA side: the required
// `step` and the optional `from`/`to` are forwarded verbatim as query
// params and the bucketed DataResponse envelope round-trips.

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('getQuiverData API', () => {
  it('GETs the dashboard /data endpoint with step + from + to', async () => {
    let capturedSearch = '';
    let capturedPath = '';
    server.use(
      http.get(
        '/api/v2/quiver/dashboards/:rid/data',
        ({ request: req }) => {
          const url = new URL(req.url);
          capturedSearch = url.search;
          capturedPath = url.pathname;
          return HttpResponse.json({
            rid: 'ri.quiver.main.dashboard.u12',
            from: '2026-01-01T00:00:00Z',
            to: '2026-01-02T00:00:00Z',
            step: '5m',
            series: [
              {
                id: 's1',
                label: 'CPU',
                color: '#22d3ee',
                objectType: 'Host',
                primaryKey: 'h1',
                property: 'cpu',
                points: [{ time: '2026-01-01T00:00:00Z', value: 12.5 }],
              },
            ],
          });
        },
      ),
    );

    const resp = await getQuiverData('ri.quiver.main.dashboard.u12', {
      from: '2026-01-01T00:00:00Z',
      to: '2026-01-02T00:00:00Z',
      step: '5m',
    });

    expect(capturedPath).toBe(
      '/api/v2/quiver/dashboards/ri.quiver.main.dashboard.u12/data',
    );
    const params = new URLSearchParams(capturedSearch);
    expect(params.get('step')).toBe('5m');
    expect(params.get('from')).toBe('2026-01-01T00:00:00Z');
    expect(params.get('to')).toBe('2026-01-02T00:00:00Z');

    expect(resp.step).toBe('5m');
    expect(resp.series).toHaveLength(1);
    expect(resp.series[0].points[0].value).toBe(12.5);
  });

  it('omits from / to when not supplied but always sends step', async () => {
    let capturedSearch = '';
    server.use(
      http.get('/api/v2/quiver/dashboards/:rid/data', ({ request: req }) => {
        capturedSearch = new URL(req.url).search;
        return HttpResponse.json({
          rid: 'r',
          from: '0001-01-01T00:00:00Z',
          to: '0001-01-01T00:00:00Z',
          step: '1h',
          series: [],
        });
      }),
    );

    await getQuiverData('r', { step: '1h' });
    const params = new URLSearchParams(capturedSearch);
    expect(params.get('step')).toBe('1h');
    expect(params.has('from')).toBe(false);
    expect(params.has('to')).toBe(false);
  });

  it('encodes the dashboard RID in the path', async () => {
    let capturedPath = '';
    server.use(
      http.get('/api/v2/quiver/dashboards/:rid/data', ({ request: req }) => {
        capturedPath = new URL(req.url).pathname;
        return HttpResponse.json({ rid: '', from: '', to: '', step: '5m', series: [] });
      }),
    );

    await getQuiverData('ri with space', { step: '5m' });
    expect(capturedPath).toContain('ri%20with%20space');
  });
});
