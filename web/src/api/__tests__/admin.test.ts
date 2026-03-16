import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import {
  createOntology,
  createObjectType,
  updateObjectType,
  deleteObjectType,
  createProperty,
  deleteProperty,
  createLinkType,
  createActionType,
} from '../admin';

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('admin API', () => {
  it('createOntology() POSTs to /api/admin/ontologies', async () => {
    server.use(
      http.post('/api/admin/ontologies', async ({ request: req }) => {
        const body = (await req.json()) as Record<string, unknown>;
        return HttpResponse.json({
          rid: 'ri.ont.1',
          apiName: body.apiName,
          displayName: body.displayName,
        });
      }),
    );

    const result = await createOntology({
      apiName: 'test',
      displayName: 'Test',
    });
    expect(result.apiName).toBe('test');
  });

  it('createObjectType() POSTs to correct URL', async () => {
    server.use(
      http.post('/api/admin/ontologies/test/objectTypes', async ({ request: req }) => {
        const body = (await req.json()) as Record<string, unknown>;
        return HttpResponse.json({
          rid: 'ri.ot.1',
          apiName: body.apiName,
          displayName: body.displayName,
          primaryKey: body.primaryKey,
          status: 'ACTIVE',
          visibility: 'NORMAL',
        });
      }),
    );

    const result = await createObjectType('test', {
      apiName: 'Employee',
      displayName: 'Employee',
      primaryKey: 'employeeId',
    });
    expect(result.apiName).toBe('Employee');
  });

  it('updateObjectType() PUTs to correct URL', async () => {
    server.use(
      http.put('/api/admin/objectTypes/ri.ot.1', async ({ request: req }) => {
        const body = (await req.json()) as Record<string, unknown>;
        return HttpResponse.json({
          rid: 'ri.ot.1',
          displayName: body.displayName,
        });
      }),
    );

    const result = await updateObjectType('ri.ot.1', {
      displayName: 'Updated',
    });
    expect(result.displayName).toBe('Updated');
  });

  it('deleteObjectType() DELETEs correct URL', async () => {
    server.use(
      http.delete('/api/admin/objectTypes/ri.ot.1', () => {
        return new HttpResponse(null, { status: 204 });
      }),
    );

    // Should not throw
    await deleteObjectType('ri.ot.1');
  });

  it('createProperty() POSTs to correct URL', async () => {
    server.use(
      http.post('/api/admin/objectTypes/ri.ot.1/properties', async ({ request: req }) => {
        const body = (await req.json()) as Record<string, unknown>;
        return HttpResponse.json({
          rid: 'ri.prop.1',
          apiName: body.apiName,
          baseType: body.baseType,
        });
      }),
    );

    const result = await createProperty('ri.ot.1', {
      apiName: 'name',
      baseType: 'string',
    });
    expect(result.apiName).toBe('name');
  });

  it('deleteProperty() DELETEs correct URL', async () => {
    server.use(
      http.delete('/api/admin/properties/ri.prop.1', () => {
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await deleteProperty('ri.prop.1');
  });

  it('createLinkType() POSTs to correct URL', async () => {
    server.use(
      http.post('/api/admin/ontologies/test/linkTypes', async ({ request: req }) => {
        const body = (await req.json()) as Record<string, unknown>;
        return HttpResponse.json({
          rid: 'ri.lt.1',
          apiName: body.apiName,
          displayName: body.displayName,
        });
      }),
    );

    const result = await createLinkType('test', {
      apiName: 'manages',
      displayName: 'Manages',
      sourceObjectType: 'ri.ot.1',
      targetObjectType: 'ri.ot.2',
      cardinality: 'ONE_TO_MANY',
    });
    expect(result.apiName).toBe('manages');
  });

  it('createActionType() POSTs to correct URL', async () => {
    server.use(
      http.post('/api/admin/ontologies/test/actionTypes', async ({ request: req }) => {
        const body = (await req.json()) as Record<string, unknown>;
        return HttpResponse.json({
          rid: 'ri.at.1',
          apiName: body.apiName,
          displayName: body.displayName,
          status: 'ACTIVE',
        });
      }),
    );

    const result = await createActionType('test', {
      apiName: 'createEmployee',
      displayName: 'Create Employee',
    });
    expect(result.apiName).toBe('createEmployee');
  });
});
