import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { request, ApiRequestError } from '../client';

const server = setupServer();

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('request()', () => {
  it('sends GET request and returns parsed JSON', async () => {
    server.use(
      http.get('/api/test', () => {
        return HttpResponse.json({ name: 'test' });
      }),
    );

    const result = await request<{ name: string }>('GET', '/api/test');
    expect(result).toEqual({ name: 'test' });
  });

  it('sends POST request with JSON body', async () => {
    server.use(
      http.post('/api/test', async ({ request: req }) => {
        const body = await req.json();
        return HttpResponse.json(body);
      }),
    );

    const result = await request<{ foo: string }>('POST', '/api/test', {
      foo: 'bar',
    });
    expect(result).toEqual({ foo: 'bar' });
  });

  it('sets Content-Type header to application/json', async () => {
    let contentType = '';
    server.use(
      http.get('/api/test', ({ request: req }) => {
        contentType = req.headers.get('Content-Type') ?? '';
        return HttpResponse.json({});
      }),
    );

    await request('GET', '/api/test');
    expect(contentType).toBe('application/json');
  });

  it('throws ApiRequestError on non-2xx response', async () => {
    server.use(
      http.get('/api/test', () => {
        return HttpResponse.json(
          {
            errorCode: 'NOT_FOUND',
            errorName: 'Resource not found',
            errorInstanceId: 'abc-123',
          },
          { status: 404 },
        );
      }),
    );

    await expect(request('GET', '/api/test')).rejects.toThrow(ApiRequestError);

    try {
      await request('GET', '/api/test');
    } catch (e) {
      const err = e as ApiRequestError;
      expect(err.statusCode).toBe(404);
      expect(err.errorCode).toBe('NOT_FOUND');
      expect(err.errorName).toBe('Resource not found');
    }
  });

  it('handles non-JSON error responses', async () => {
    server.use(
      http.get('/api/test', () => {
        return new HttpResponse('Internal Server Error', { status: 500 });
      }),
    );

    await expect(request('GET', '/api/test')).rejects.toThrow(ApiRequestError);

    try {
      await request('GET', '/api/test');
    } catch (e) {
      const err = e as ApiRequestError;
      expect(err.statusCode).toBe(500);
      expect(err.errorCode).toBe('UNKNOWN');
    }
  });
});
