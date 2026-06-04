import {
  describe,
  it,
  expect,
  vi,
  beforeAll,
  afterAll,
  beforeEach,
  afterEach,
} from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AuthProvider } from '../../../auth/AuthContext';
import { ObjectTable } from '../ObjectTable';
import type { ObjectType, WireObject } from '../../../api/types';

// BDD: the object list must show each row's reactions, and it must collapse
// the N per-row GET /api/v2/reactions calls into ONE batched POST
// /api/v2/reactions/batch (PG: WHERE target_rid = ANY($1)). The response is
// index-aligned with the request's targetRids so summaries[i] belongs to
// targetRids[i].

const objectType: ObjectType = {
  rid: 'ri.ot',
  apiName: 'Employee',
  displayName: 'Employee',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
  },
};

const rows: WireObject[] = [
  {
    __primaryKey: '1',
    __apiName: 'Employee',
    __rid: 'ri.ontology.main.object.emp1',
    id: '1',
    name: 'Alice',
  },
  {
    __primaryKey: '2',
    __apiName: 'Employee',
    __rid: 'ri.ontology.main.object.emp2',
    id: '2',
    name: 'Bob',
  },
  {
    __primaryKey: '3',
    __apiName: 'Employee',
    __rid: 'ri.ontology.main.object.emp3',
    id: '3',
    name: 'Carol',
  },
];

interface BatchBody {
  targetRids: string[];
}

let batchCalls: BatchBody[] = [];
let singleGetCalls = 0;

const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: 'bypass' }));
beforeEach(() => {
  batchCalls = [];
  singleGetCalls = 0;
  server.use(
    http.get('/api/v2/me', () =>
      HttpResponse.json({
        id: 'alice',
        email: 'alice@example.test',
        name: 'alice',
        roles: ['viewer'],
        ontologyRoles: {},
        permissions: ['object.read'],
      }),
    ),
    // The N+1 single-target path must NOT be hit by the list.
    http.get('/api/v2/reactions', () => {
      singleGetCalls += 1;
      return HttpResponse.json({ targetRid: '', emojis: [] });
    }),
    http.post('/api/v2/reactions/batch', async ({ request }) => {
      const body = (await request.json()) as BatchBody;
      batchCalls.push(body);
      // Index-aligned response. emp1 has reactions, emp2 has none, emp3 has one.
      const byRid: Record<
        string,
        { emoji: string; count: number; mine: boolean }[]
      > = {
        'ri.ontology.main.object.emp1': [
          { emoji: '👍', count: 3, mine: true },
          { emoji: '🎉', count: 1, mine: false },
        ],
        'ri.ontology.main.object.emp2': [],
        'ri.ontology.main.object.emp3': [{ emoji: '🚀', count: 2, mine: false }],
      };
      return HttpResponse.json({
        summaries: body.targetRids.map((rid) => ({
          targetRid: rid,
          emojis: byRid[rid] ?? [],
        })),
      });
    }),
  );
});
afterEach(() => {
  server.resetHandlers();
  vi.clearAllMocks();
});
afterAll(() => server.close());

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(
      QueryClientProvider,
      { client },
      createElement(AuthProvider, null, children),
    );
}

describe('ObjectTable per-row reactions (batch endpoint)', () => {
  it('issues exactly ONE batch request covering every visible row RID', async () => {
    render(
      <ObjectTable
        ontologyApiName="ont"
        objectType={objectType}
        data={rows}
        showReactions
      />,
      { wrapper: makeWrapper() },
    );

    await waitFor(() => {
      expect(batchCalls.length).toBe(1);
    });

    // Give any rogue N+1 path a tick to (wrongly) fire.
    await new Promise((r) => setTimeout(r, 20));

    expect(batchCalls.length).toBe(1);
    expect(singleGetCalls).toBe(0);
    expect(batchCalls[0].targetRids).toEqual([
      'ri.ontology.main.object.emp1',
      'ri.ontology.main.object.emp2',
      'ri.ontology.main.object.emp3',
    ]);
  });

  it('renders each row reaction summary index-aligned to its RID', async () => {
    render(
      <ObjectTable
        ontologyApiName="ont"
        objectType={objectType}
        data={rows}
        showReactions
      />,
      { wrapper: makeWrapper() },
    );

    // The cell exists immediately (empty placeholder) and fills once the single
    // batched fetch resolves; wait on the content, not just the testid.
    await waitFor(() => {
      expect(
        screen.getByTestId('row-reactions-ri.ontology.main.object.emp1'),
      ).toHaveTextContent('👍');
    });

    // emp1: top emoji 👍 with total count 4 (3 + 1).
    const emp1 = screen.getByTestId(
      'row-reactions-ri.ontology.main.object.emp1',
    );
    expect(emp1).toHaveTextContent('👍');
    expect(emp1).toHaveTextContent('4');

    // emp3: only 🚀 with total 2.
    const emp3 = screen.getByTestId(
      'row-reactions-ri.ontology.main.object.emp3',
    );
    expect(emp3).toHaveTextContent('🚀');
    expect(emp3).toHaveTextContent('2');

    // emp2 has no reactions: cell present but shows no other-row emoji/count.
    const emp2 = screen.getByTestId(
      'row-reactions-ri.ontology.main.object.emp2',
    );
    expect(emp2).not.toHaveTextContent('👍');
    expect(emp2).not.toHaveTextContent('🚀');
  });

  it('does not fetch reactions at all when showReactions is omitted', async () => {
    render(
      <ObjectTable ontologyApiName="ont" objectType={objectType} data={rows} />,
      { wrapper: makeWrapper() },
    );
    await new Promise((r) => setTimeout(r, 20));
    expect(batchCalls.length).toBe(0);
    expect(singleGetCalls).toBe(0);
    expect(
      screen.queryByTestId('row-reactions-ri.ontology.main.object.emp1'),
    ).not.toBeInTheDocument();
  });
});
