import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { searchObjects } from '../objects';

// BDD: the search API client must send Foundry's SearchOrderByV2 wire shape.
//
// The backend (pkg/oss handlers) — like real Foundry — reads
// SearchObjectsRequestV2.orderBy as:
//
//   {"fields": [{"field": "<prop>", "direction": "asc"|"desc"}]}
//
// The client previously serialized `orderBy: {field, direction}` (a bare
// object, no `fields` array). The backend tolerates unknown keys, so that
// body deserialized to an EMPTY ordering: HTTP 200, data unsorted — the
// Browser page's "sort while filtering/searching" was silently broken.
//
// Given  a caller sorting by age descending while searching
// When   searchObjects() issues the POST
// Then   the request body carries orderBy.fields = [{field, direction}]
//        so the backend actually sorts the page.
const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe('searchObjects orderBy wire contract (BDD)', () => {
  const capture = () => {
    const captured: { body?: Record<string, unknown> } = {};
    server.use(
      http.post(
        '/api/v2/ontologies/test/objects/Employee/search',
        async ({ request: req }) => {
          captured.body = (await req.json()) as Record<string, unknown>;
          return HttpResponse.json({ data: [], totalCount: '0' });
        },
      ),
    );
    return captured;
  };

  it('sends Foundry SearchOrderByV2 {fields:[{field,direction}]} in the body', async () => {
    const captured = capture();

    await searchObjects({
      ontologyApiName: 'test',
      objectType: 'Employee',
      select: ['age'],
      orderBy: { field: 'age', direction: 'desc' },
    });

    expect(captured.body?.orderBy).toEqual({
      fields: [{ field: 'age', direction: 'desc' }],
    });
  });

  it('defaults direction to asc when the caller omits it', async () => {
    const captured = capture();

    await searchObjects({
      ontologyApiName: 'test',
      objectType: 'Employee',
      select: ['age'],
      orderBy: { field: 'age' },
    });

    expect(captured.body?.orderBy).toEqual({
      fields: [{ field: 'age', direction: 'asc' }],
    });
  });

  it('omits orderBy entirely when the caller does not sort', async () => {
    const captured = capture();

    await searchObjects({
      ontologyApiName: 'test',
      objectType: 'Employee',
      select: ['age'],
    });

    expect(captured.body).not.toHaveProperty('orderBy');
  });
});
