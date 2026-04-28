import { describe, it, expect, vi, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectSetDiffPage } from '../ObjectSetDiffPage';
import { localStorageKey } from '../../../lib/objectSetBuilder';

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectTypes: () => ({
    data: [
      { apiName: 'Employee', displayName: 'Employee' },
    ],
    isLoading: false,
  }),
  useObjectType: (_ontology: string, apiName: string) => ({
    data: apiName
      ? {
          rid: 'ri.ot',
          apiName,
          displayName: apiName,
          primaryKey: 'id',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          properties: {
            id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
            name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
            age: { dataType: { type: 'integer' }, rid: 'ri.p.age' },
          },
        }
      : undefined,
    isLoading: false,
  }),
  useOutgoingLinkTypes: () => ({ data: [], isLoading: false }),
}));

const server = setupServer();
beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

const ONTOLOGY = 'test';

beforeEach(() => {
  // Seed two saved object sets for the ontology so the page can pick them.
  const savedAB = [
    {
      id: 'sa-1',
      name: 'Set A',
      def: { type: 'base', objectType: 'Employee' },
      createdAt: new Date().toISOString(),
    },
    {
      id: 'sb-1',
      name: 'Set B',
      def: { type: 'base', objectType: 'Employee' },
      createdAt: new Date().toISOString(),
    },
  ];
  window.localStorage.setItem(localStorageKey(ONTOLOGY), JSON.stringify(savedAB));
});

afterEach(() => {
  window.localStorage.clear();
});

function renderPage(ontology = ONTOLOGY) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/objectsets/${ontology}/diff`]}>
        <Routes>
          <Route
            path="/objectsets/:ontology/diff"
            element={<ObjectSetDiffPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ObjectSetDiffPage', () => {
  it('renders the page title and saved-set selectors', () => {
    renderPage();
    expect(
      screen.getByRole('heading', { name: /object set diff/i }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/object set a/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/object set b/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /compute diff/i })).toBeDisabled();
  });

  it('shows an empty state when no saved sets exist', () => {
    window.localStorage.clear();
    renderPage();
    expect(screen.getByText(/no saved object sets/i)).toBeInTheDocument();
  });

  it('computes the diff and shows only-in-A, only-in-B, and changed sections', async () => {
    server.use(
      http.get(
        '/api/v2/ontologies/test/objectTypes/Employee',
        () =>
          HttpResponse.json({
            rid: 'ri.ot',
            apiName: 'Employee',
            displayName: 'Employee',
            primaryKey: 'id',
            status: 'ACTIVE',
            visibility: 'NORMAL',
            properties: {
              id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
              name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
              age: { dataType: { type: 'integer' }, rid: 'ri.p.age' },
            },
          }),
      ),
      http.post(
        '/api/v2/ontologies/test/objectSets/loadObjects',
        async ({ request }) => {
          const body = (await request.json()) as { objectSet: { type: string } };
          // Distinguish the two requests by stamping a marker on the saved
          // defs would normally be done via different objectSet.objectType,
          // but here both savedA/B share Employee. Instead branch on the
          // first call vs second by alternating responses with a counter.
          const isFirst = !sentFirst;
          sentFirst = true;
          void body;
          if (isFirst) {
            return HttpResponse.json({
              data: [
                { __rid: 'r1', __primaryKey: '1', __apiName: 'Employee', name: 'Alice', age: 30 },
                { __rid: 'r2', __primaryKey: '2', __apiName: 'Employee', name: 'Bob', age: 25 },
                { __rid: 'r3', __primaryKey: '3', __apiName: 'Employee', name: 'Carol', age: 40 },
              ],
              totalCount: '3',
            });
          }
          return HttpResponse.json({
            data: [
              { __rid: 'r2b', __primaryKey: '2', __apiName: 'Employee', name: 'Bob', age: 25 },
              { __rid: 'r3b', __primaryKey: '3', __apiName: 'Employee', name: 'Caroline', age: 41 },
              { __rid: 'r4b', __primaryKey: '4', __apiName: 'Employee', name: 'Dave', age: 33 },
            ],
            totalCount: '3',
          });
        },
      ),
    );
    let sentFirst = false;

    renderPage();
    fireEvent.change(screen.getByLabelText(/object set a/i), {
      target: { value: 'sa-1' },
    });
    fireEvent.change(screen.getByLabelText(/object set b/i), {
      target: { value: 'sb-1' },
    });
    fireEvent.click(screen.getByRole('button', { name: /compute diff/i }));

    const onlyA = await screen.findByTestId('diff-only-in-a');
    const onlyB = await screen.findByTestId('diff-only-in-b');
    const changed = await screen.findByTestId('diff-changed');

    // PK 1 (Alice) is only in A; PK 2/3 are in both so they should NOT appear here
    expect(within(onlyA).getByText('Alice')).toBeInTheDocument();
    expect(within(onlyA).queryByText('Bob')).toBeNull();
    expect(within(onlyA).queryByText('Carol')).toBeNull();
    // PK 4 (Dave) is only in B
    expect(within(onlyB).getByText('Dave')).toBeInTheDocument();
    expect(within(onlyB).queryByText('Bob')).toBeNull();
    // PK 3 differs (Carol vs Caroline / 40 vs 41) — surfaced in changed section
    expect(within(changed).getByText('Carol')).toBeInTheDocument();
    expect(within(changed).getByText('Caroline')).toBeInTheDocument();
    expect(within(changed).getByText('40')).toBeInTheDocument();
    expect(within(changed).getByText('41')).toBeInTheDocument();
  });

  it('shows an error when the metadata fetch fails', async () => {
    server.use(
      http.get(
        '/api/v2/ontologies/test/objectTypes/Employee',
        () =>
          HttpResponse.json(
            {
              errorCode: 'INVALID',
              errorName: 'BadRequest',
              errorInstanceId: 'abc',
              statusCode: 400,
            },
            { status: 400 },
          ),
      ),
    );

    renderPage();
    fireEvent.change(screen.getByLabelText(/object set a/i), {
      target: { value: 'sa-1' },
    });
    fireEvent.change(screen.getByLabelText(/object set b/i), {
      target: { value: 'sb-1' },
    });
    fireEvent.click(screen.getByRole('button', { name: /compute diff/i }));

    await waitFor(() => {
      expect(screen.getByText(/INVALID|error|failed|Bad/i)).toBeInTheDocument();
    });
  });
});
