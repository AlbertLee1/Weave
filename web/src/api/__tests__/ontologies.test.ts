import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  listOntologies,
  getOntology,
  listObjectTypes,
  getObjectType,
  listOutgoingLinkTypes,
  listActionTypes,
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
});
