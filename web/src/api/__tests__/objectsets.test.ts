import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  loadObjectSet,
  aggregateObjectSet,
  createTemporaryObjectSet,
  getObjectSet,
  loadLinks,
} from '../objectsets';
import type { ObjectSetDefinition } from '../types';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('objectsets API', () => {
  const baseObjectSet: ObjectSetDefinition = {
    type: 'base',
    objectType: 'Employee',
  };

  it('loadObjectSet() POSTs to correct URL with objectSet body', async () => {
    const data = {
      data: [{ __rid: 'ri.1', __primaryKey: '1', __apiName: 'Employee' }],
      totalCount: '10',
    };
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/loadObjects',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.objectSet).toEqual(baseObjectSet);
          return HttpResponse.json(data);
        },
      ),
    );

    const result = await loadObjectSet('test', {
      objectSet: baseObjectSet,
      select: ['__primaryKey'],
    });
    expect(result.data).toHaveLength(1);
    expect(result.totalCount).toBe('10');
  });

  it('loadObjectSet() sends pageSize and pageToken', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/loadObjects',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.pageSize).toBe(25);
          expect(body.pageToken).toBe('tok123');
          return HttpResponse.json({ data: [], totalCount: '0' });
        },
      ),
    );

    await loadObjectSet('test', {
      objectSet: baseObjectSet,
      select: ['__primaryKey'],
      pageSize: 25,
      pageToken: 'tok123',
    });
  });

  it('loadObjectSet() sends select fields', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/loadObjects',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.select).toEqual(['name', 'age']);
          return HttpResponse.json({ data: [], totalCount: '0' });
        },
      ),
    );

    await loadObjectSet('test', {
      objectSet: baseObjectSet,
      select: ['name', 'age'],
    });
  });

  it('aggregateObjectSet() POSTs aggregation spec with the definition', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/aggregate',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.objectSet).toEqual(baseObjectSet);
          expect(body.aggregation).toEqual([{ type: 'count' }]);
          expect(body.groupBy).toEqual([{ field: 'department', type: 'exact' }]);
          return HttpResponse.json({
            data: [
              { group: { department: 'Engineering' }, metrics: { count: 12 } },
            ],
          });
        },
      ),
    );

    const result = await aggregateObjectSet(
      'test',
      baseObjectSet,
      [{ type: 'count' }],
      [{ field: 'department', type: 'exact' }],
    );
    expect(result.data).toHaveLength(1);
    expect(result.data[0].metrics.count).toBe(12);
  });

  it('aggregateObjectSet() omits empty groupBy', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/aggregate',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.groupBy).toBeUndefined();
          return HttpResponse.json({ data: [{ metrics: { count: 7 } }] });
        },
      ),
    );

    const result = await aggregateObjectSet(
      'test',
      baseObjectSet,
      [{ type: 'count' }],
    );
    expect(result.data).toHaveLength(1);
  });

  it('createTemporaryObjectSet() POSTs and returns objectSetRid', async () => {
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/createTemporary',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.objectSet).toEqual(baseObjectSet);
          return HttpResponse.json({ objectSetRid: 'ri.objectset.main.1234' });
        },
      ),
    );

    const result = await createTemporaryObjectSet('test', baseObjectSet);
    expect(result.objectSetRid).toBe('ri.objectset.main.1234');
  });

  it('getObjectSet() GETs objectSet by RID', async () => {
    const def = { type: 'base', objectType: 'Employee' };
    server.use(
      http.get(
        '/api/v2/ontologies/test/objectSets/ri.objectset.main.1234',
        () => HttpResponse.json(def),
      ),
    );

    const result = await getObjectSet('test', 'ri.objectset.main.1234');
    expect(result).toEqual(def);
  });

  it('loadLinks() POSTs with objectSet, linkType, and select', async () => {
    const data = {
      data: [{ __rid: 'ri.2', __primaryKey: '2', __apiName: 'Department' }],
      totalCount: '1',
    };
    server.use(
      http.post(
        '/api/v2/ontologies/test/objectSets/loadLinks',
        async ({ request: req }) => {
          const body = (await req.json()) as Record<string, unknown>;
          expect(body.objectSet).toEqual(baseObjectSet);
          expect(body.linkType).toBe('department');
          expect(body.select).toEqual(['name', 'deptId']);
          return HttpResponse.json(data);
        },
      ),
    );

    const result = await loadLinks('test', baseObjectSet, 'department', ['name', 'deptId']);
    expect(result.data).toHaveLength(1);
    expect(result.totalCount).toBe('1');
  });
});
