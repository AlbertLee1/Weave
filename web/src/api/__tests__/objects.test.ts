import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { listObjects, searchObjects, getObject, listLinkedObjects, countObjects, getLinkedObject } from '../objects';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('objects API', () => {
  it('listObjects() GETs with pageSize query param', async () => {
    const data = {
      data: [{ __rid: 'ri.1', __primaryKey: '1', __apiName: 'Employee' }],
      totalCount: '1',
    };
    server.use(
      http.get('/api/v2/ontologies/test/objects/Employee', ({ request: req }) => {
        const url = new URL(req.url);
        expect(url.searchParams.get('pageSize')).toBe('25');
        return HttpResponse.json(data);
      }),
    );

    const result = await listObjects({
      ontologyApiName: 'test',
      objectType: 'Employee',
      pageSize: 25,
    });
    expect(result.data).toHaveLength(1);
  });

  it('searchObjects() POSTs with where clause and select', async () => {
    const data = {
      data: [{ __rid: 'ri.1', __primaryKey: '1', __apiName: 'Employee' }],
    };
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Employee/search',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.where).toEqual({
            type: 'eq',
            field: 'name',
            value: 'Alice',
          });
          expect(body.select).toEqual(['name', 'employeeId']);
          return HttpResponse.json(data);
        },
      ),
    );

    const result = await searchObjects({
      ontologyApiName: 'test',
      objectType: 'Employee',
      where: { type: 'eq', field: 'name', value: 'Alice' },
      select: ['name', 'employeeId'],
    });
    expect(result.data).toHaveLength(1);
  });

  it('getObject() GETs by primary key', async () => {
    const data = {
      __rid: 'ri.1',
      __primaryKey: '42',
      __apiName: 'Employee',
      name: 'Alice',
    };
    server.use(
      http.get('/api/v2/ontologies/test/objects/Employee/42', () =>
        HttpResponse.json(data),
      ),
    );

    const result = await getObject({
      ontologyApiName: 'test',
      objectType: 'Employee',
      primaryKey: '42',
    });
    expect(result.__primaryKey).toBe('42');
    expect(result.name).toBe('Alice');
  });

  it('listLinkedObjects() GETs correct URL', async () => {
    const data = {
      data: [{ __rid: 'ri.2', __primaryKey: '2', __apiName: 'Department' }],
    };
    server.use(
      http.get(
        '/api/v2/ontologies/test/objects/Employee/42/links/department',
        () => HttpResponse.json(data),
      ),
    );

    const result = await listLinkedObjects({
      ontologyApiName: 'test',
      objectType: 'Employee',
      primaryKey: '42',
      linkType: 'department',
    });
    expect(result.data).toHaveLength(1);
  });

  it('countObjects() POSTs and returns count', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Employee/count',
        () => HttpResponse.json({ count: 42 }),
      ),
    );

    const result = await countObjects('test', 'Employee');
    expect(result.count).toBe(42);
  });

  it('getLinkedObject() GETs specific linked object by PK', async () => {
    const data = {
      __rid: 'ri.3',
      __primaryKey: 'DEPT-1',
      __apiName: 'Department',
      name: 'Engineering',
    };
    server.use(
      http.get(
        '/api/v2/ontologies/test/objects/Employee/42/links/department/DEPT-1',
        () => HttpResponse.json(data),
      ),
    );

    const result = await getLinkedObject('test', 'Employee', '42', 'department', 'DEPT-1');
    expect(result.__primaryKey).toBe('DEPT-1');
    expect(result.name).toBe('Engineering');
  });
});
