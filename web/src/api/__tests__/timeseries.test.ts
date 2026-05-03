import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { streamTimeSeriesPoints } from '../timeseries';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('timeseries API', () => {
  it('streamTimeSeriesPoints() POSTs to the streamPoints endpoint', async () => {
    const points = [
      { time: '2026-04-01T00:00:00Z', value: 21.0 },
      { time: '2026-04-02T00:00:00Z', value: 22.5 },
    ];
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Server/s1/timeseries/cpu/streamPoints',
        async ({ request: req }) => {
          const body = (await req.json()) as unknown;
          expect(body).toEqual({});
          return HttpResponse.json(points);
        },
      ),
    );

    const result = await streamTimeSeriesPoints({
      ontologyApiName: 'test',
      objectType: 'Server',
      primaryKey: 's1',
      property: 'cpu',
    });
    expect(result).toHaveLength(2);
    expect(result[0].time).toBe('2026-04-01T00:00:00Z');
  });

  it('encodes path segments with special characters', async () => {
    let capturedUrl = '';
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/objects/:objectType/:pk/timeseries/:property/streamPoints',
        ({ request: req }) => {
          capturedUrl = new URL(req.url).pathname;
          return HttpResponse.json([]);
        },
      ),
    );

    await streamTimeSeriesPoints({
      ontologyApiName: 'my ontology',
      objectType: 'Server',
      primaryKey: 'pk/with/slashes',
      property: 'cpu',
    });
    expect(capturedUrl).toContain('my%20ontology');
    expect(capturedUrl).toContain('pk%2Fwith%2Fslashes');
  });

  it('returns an empty array when the series has no points', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Server/s1/timeseries/cpu/streamPoints',
        () => HttpResponse.json([]),
      ),
    );

    const result = await streamTimeSeriesPoints({
      ontologyApiName: 'test',
      objectType: 'Server',
      primaryKey: 's1',
      property: 'cpu',
    });
    expect(result).toEqual([]);
  });

  // US-404: explicit branch overrides the global ?branch= injection.
  it('appends ?branch= when an explicit branch is supplied', async () => {
    let capturedSearch = '';
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Server/s1/timeseries/cpu/streamPoints',
        ({ request: req }) => {
          capturedSearch = new URL(req.url).search;
          return HttpResponse.json([]);
        },
      ),
    );

    await streamTimeSeriesPoints({
      ontologyApiName: 'test',
      objectType: 'Server',
      primaryKey: 's1',
      property: 'cpu',
      branch: 'feature-x',
    });
    expect(capturedSearch).toContain('branch=feature-x');
  });

  it('does not append ?branch= for blank or missing values', async () => {
    let capturedSearch = '';
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Server/s1/timeseries/cpu/streamPoints',
        ({ request: req }) => {
          capturedSearch = new URL(req.url).search;
          return HttpResponse.json([]);
        },
      ),
    );

    await streamTimeSeriesPoints({
      ontologyApiName: 'test',
      objectType: 'Server',
      primaryKey: 's1',
      property: 'cpu',
      branch: '   ',
    });
    expect(capturedSearch).toBe('');
  });

  it('encodes branch values with reserved URL characters', async () => {
    let capturedSearch = '';
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Server/s1/timeseries/cpu/streamPoints',
        ({ request: req }) => {
          capturedSearch = new URL(req.url).search;
          return HttpResponse.json([]);
        },
      ),
    );

    await streamTimeSeriesPoints({
      ontologyApiName: 'test',
      objectType: 'Server',
      primaryKey: 's1',
      property: 'cpu',
      branch: 'feature/with spaces',
    });
    expect(capturedSearch).toContain('branch=feature%2Fwith%20spaces');
  });
});
