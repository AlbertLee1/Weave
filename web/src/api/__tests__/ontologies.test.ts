import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  listOntologies,
  getOntology,
  loadOntologyMetadata,
  getOntologyFullMetadata,
  listObjectTypes,
  getObjectType,
  getObjectTypeFullMetadata,
  listOutgoingLinkTypes,
  listActionTypes,
  getActionType,
  getActionTypeByRid,
  listInterfaceTypes,
  getInterfaceType,
  listValueTypes,
  getValueType,
  listQueryTypes,
  getQueryType,
  executeQuery,
} from '../ontologies';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('ontologies API', () => {
  it('listOntologies() GETs /api/v2/ontologies and unwraps data', async () => {
    const items = [
      { rid: 'ri.1', apiName: 'test', displayName: 'Test' },
    ];
    server.use(
      http.get('/api/v2/ontologies', () => HttpResponse.json({ data: items })),
    );

    const result = await listOntologies();
    expect(result).toEqual(items);
  });

  it('getOntology() GETs /api/v2/ontologies/:name', async () => {
    const data = { rid: 'ri.1', apiName: 'test', displayName: 'Test' };
    server.use(
      http.get('/api/v2/ontologies/test', () => HttpResponse.json(data)),
    );

    const result = await getOntology('test');
    expect(result).toEqual(data);
  });

  it('listObjectTypes() GETs correct URL and unwraps data', async () => {
    const items = [
      {
        rid: 'ri.ot.1',
        apiName: 'Employee',
        displayName: 'Employee',
        primaryKey: 'employeeId',
        status: 'ACTIVE',
        visibility: 'NORMAL',
      },
    ];
    server.use(
      http.get('/api/v2/ontologies/test/objectTypes', () =>
        HttpResponse.json({ data: items }),
      ),
    );

    const result = await listObjectTypes('test');
    expect(result).toEqual(items);
  });

  it('getObjectType() GETs correct URL', async () => {
    const data = {
      rid: 'ri.ot.1',
      apiName: 'Employee',
      displayName: 'Employee',
      primaryKey: 'employeeId',
      status: 'ACTIVE',
      visibility: 'NORMAL',
    };
    server.use(
      http.get('/api/v2/ontologies/test/objectTypes/Employee', () =>
        HttpResponse.json(data),
      ),
    );

    const result = await getObjectType('test', 'Employee');
    expect(result).toEqual(data);
  });

  it('listOutgoingLinkTypes() GETs correct URL and unwraps data', async () => {
    const items = [
      {
        rid: 'ri.lt.1',
        apiName: 'manages',
        displayName: 'Manages',
        cardinality: 'ONE_TO_MANY',
      },
    ];
    server.use(
      http.get(
        '/api/v2/ontologies/test/objectTypes/Employee/outgoingLinkTypes',
        () => HttpResponse.json({ data: items }),
      ),
    );

    const result = await listOutgoingLinkTypes('test', 'Employee');
    expect(result).toEqual(items);
  });

  it('listActionTypes() GETs correct URL and unwraps data', async () => {
    const items = [
      {
        rid: 'ri.at.1',
        apiName: 'createEmployee',
        displayName: 'Create Employee',
        status: 'ACTIVE',
      },
    ];
    server.use(
      http.get('/api/v2/ontologies/test/actionTypes', () =>
        HttpResponse.json({ data: items }),
      ),
    );

    const result = await listActionTypes('test');
    expect(result).toEqual(items);
  });

  // --- Phase 1 new endpoints ---

  it('loadOntologyMetadata() POSTs subsets', async () => {
    const metadata = { objectTypes: [{ apiName: 'Employee' }] };
    server.use(
      http.post('/api/v2/ontologies/test/metadata', async ({ request: req }) => {
        const body = (await req.json()) as Record<string, unknown>;
        expect(body.objectTypes).toBe(true);
        return HttpResponse.json(metadata);
      }),
    );

    const result = await loadOntologyMetadata('test', { objectTypes: true });
    expect(result).toEqual(metadata);
  });

  it('getOntologyFullMetadata() GETs with preview param', async () => {
    const metadata = { ontology: { apiName: 'test' }, objectTypes: {} };
    server.use(
      http.get('/api/v2/ontologies/test/fullMetadata', ({ request: req }) => {
        const url = new URL(req.url);
        expect(url.searchParams.get('preview')).toBe('true');
        return HttpResponse.json(metadata);
      }),
    );

    const result = await getOntologyFullMetadata('test');
    expect(result).toEqual(metadata);
  });

  it('getObjectTypeFullMetadata() GETs correct URL with preview', async () => {
    const metadata = { objectType: { apiName: 'Employee' }, properties: {} };
    server.use(
      http.get('/api/v2/ontologies/test/objectTypes/Employee/fullMetadata', ({ request: req }) => {
        const url = new URL(req.url);
        expect(url.searchParams.get('preview')).toBe('true');
        return HttpResponse.json(metadata);
      }),
    );

    const result = await getObjectTypeFullMetadata('test', 'Employee');
    expect(result).toEqual(metadata);
  });

  it('getActionType() GETs correct URL', async () => {
    const data = { rid: 'ri.at.1', apiName: 'createEmployee', displayName: 'Create Employee', status: 'ACTIVE' };
    server.use(
      http.get('/api/v2/ontologies/test/actionTypes/createEmployee', () =>
        HttpResponse.json(data),
      ),
    );

    const result = await getActionType('test', 'createEmployee');
    expect(result.apiName).toBe('createEmployee');
  });

  it('getActionTypeByRid() GETs byRid path', async () => {
    const data = { rid: 'ri.at.1', apiName: 'createEmployee', displayName: 'Create Employee', status: 'ACTIVE' };
    server.use(
      http.get('/api/v2/ontologies/test/actionTypes/byRid/ri.at.1', () =>
        HttpResponse.json(data),
      ),
    );

    const result = await getActionTypeByRid('test', 'ri.at.1');
    expect(result.rid).toBe('ri.at.1');
  });

  it('listInterfaceTypes() GETs with preview and unwraps data', async () => {
    const items = [{ rid: 'ri.if.1', apiName: 'Auditable', displayName: 'Auditable' }];
    server.use(
      http.get('/api/v2/ontologies/test/interfaceTypes', ({ request: req }) => {
        const url = new URL(req.url);
        expect(url.searchParams.get('preview')).toBe('true');
        return HttpResponse.json({ data: items });
      }),
    );

    const result = await listInterfaceTypes('test');
    expect(result).toEqual(items);
  });

  it('getInterfaceType() GETs correct URL', async () => {
    const data = { rid: 'ri.if.1', apiName: 'Auditable', displayName: 'Auditable' };
    server.use(
      http.get('/api/v2/ontologies/test/interfaceTypes/Auditable', () =>
        HttpResponse.json(data),
      ),
    );

    const result = await getInterfaceType('test', 'Auditable');
    expect(result.apiName).toBe('Auditable');
  });

  it('listValueTypes() GETs with preview and unwraps data', async () => {
    const items = [{ rid: 'ri.vt.1', apiName: 'EmailAddress', displayName: 'Email', baseType: 'string' }];
    server.use(
      http.get('/api/v2/ontologies/test/valueTypes', ({ request: req }) => {
        const url = new URL(req.url);
        expect(url.searchParams.get('preview')).toBe('true');
        return HttpResponse.json({ data: items });
      }),
    );

    const result = await listValueTypes('test');
    expect(result).toEqual(items);
  });

  it('getValueType() GETs correct URL', async () => {
    const data = { rid: 'ri.vt.1', apiName: 'EmailAddress', displayName: 'Email', baseType: 'string' };
    server.use(
      http.get('/api/v2/ontologies/test/valueTypes/EmailAddress', () =>
        HttpResponse.json(data),
      ),
    );

    const result = await getValueType('test', 'EmailAddress');
    expect(result.apiName).toBe('EmailAddress');
  });

  it('listQueryTypes() GETs correct URL and unwraps data', async () => {
    const items = [{ rid: 'ri.qt.1', apiName: 'topCustomers', displayName: 'Top Customers', status: 'ACTIVE' }];
    server.use(
      http.get('/api/v2/ontologies/test/queryTypes', () =>
        HttpResponse.json({ data: items }),
      ),
    );

    const result = await listQueryTypes('test');
    expect(result).toEqual(items);
  });

  it('getQueryType() GETs correct URL', async () => {
    const data = { rid: 'ri.qt.1', apiName: 'topCustomers', displayName: 'Top Customers', status: 'ACTIVE' };
    server.use(
      http.get('/api/v2/ontologies/test/queryTypes/topCustomers', () =>
        HttpResponse.json(data),
      ),
    );

    const result = await getQueryType('test', 'topCustomers');
    expect(result.apiName).toBe('topCustomers');
  });

  it('executeQuery() POSTs parameters to correct URL', async () => {
    server.use(
      http.post('/api/v2/ontologies/test/queries/topCustomers/execute', async ({ request: req }) => {
        const body = (await req.json()) as Record<string, unknown>;
        expect((body.parameters as Record<string, unknown>).limit).toBe(10);
        return HttpResponse.json({ data: [{ name: 'Alice' }] });
      }),
    );

    const result = await executeQuery('test', 'topCustomers', { limit: 10 });
    expect(result.data).toBeDefined();
  });
});
