import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectSetBuilder } from '../ObjectSetBuilder';
import { ObjectSetPage } from '../ObjectSetPage';
import { localStorageKey } from '../../../lib/objectSetBuilder';
import type { ObjectSetDefinition, WhereClause } from '../../../api/types';

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectTypes: () => ({
    data: [
      { apiName: 'Employee', displayName: 'Employee' },
      { apiName: 'Department', displayName: 'Department' },
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
            status: { dataType: { type: 'string' }, rid: 'ri.p.status' },
            country: { dataType: { type: 'string' }, rid: 'ri.p.country' },
          },
        }
      : undefined,
    isLoading: false,
  }),
  useOutgoingLinkTypes: () => ({ data: [], isLoading: false }),
}));

const server = setupServer();

beforeAll(() => server.listen());
afterEach(() => {
  server.resetHandlers();
  window.localStorage.clear();
  window.history.replaceState({}, '', '/');
});
afterAll(() => server.close());

function renderObjectSetPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/objectsets/test']}>
        <Routes>
          <Route path="/objectsets/:ontology" element={<ObjectSetPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('BDD: logical Where groups in ObjectSet composer (SELF-441)', () => {
  it('Given the user adds an and filter group with two child clauses, When the definition updates, Then the wire WhereClause contains an and value array', () => {
    const onChange = vi.fn();
    const value: ObjectSetDefinition = {
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Employee' },
      where: { type: 'eq', field: '', value: '' },
    };

    render(
      <ObjectSetBuilder
        objectTypes={['Employee']}
        value={value}
        onChange={onChange}
      />,
    );

    fireEvent.change(screen.getByRole('combobox', { name: /where type/i }), {
      target: { value: 'and' },
    });

    expect(onChange).toHaveBeenCalledWith({
      ...value,
      where: {
        type: 'and',
        value: [
          { type: 'eq', field: '', value: '' },
          { type: 'eq', field: '', value: '' },
        ],
      },
    });
  });

  it('Given the user adds a not filter around an eq clause, When ObjectSet executes, Then the backend receives the supported not shape', async () => {
    let capturedBody: { objectSet: ObjectSetDefinition } | null = null;
    server.use(
      http.post('/api/v2/ontologies/test/objectSets/loadObjects', async ({ request }) => {
        capturedBody = (await request.json()) as { objectSet: ObjectSetDefinition };
        return HttpResponse.json({ data: [], totalCount: '0' });
      }),
      http.post('/api/v2/ontologies/test/objectSets/createTemporary', () =>
        HttpResponse.json({ objectSetRid: 'ri.objectset.weave.main.tempObjectSet.0001' }),
      ),
    );

    renderObjectSetPage();

    fireEvent.change(screen.getAllByRole('combobox', { name: /objectset type/i })[0], {
      target: { value: 'filter' },
    });
    fireEvent.change(screen.getByRole('combobox', { name: /where type/i }), {
      target: { value: 'not' },
    });
    fireEvent.change(screen.getByLabelText(/where field/i), {
      target: { value: 'status' },
    });
    fireEvent.change(screen.getByLabelText(/where value/i), {
      target: { value: 'archived' },
    });

    fireEvent.click(screen.getByRole('button', { name: /execute/i }));

    await waitFor(() => {
      expect(capturedBody?.objectSet).toEqual({
        type: 'filter',
        objectSet: { type: 'base', objectType: 'Employee' },
        where: {
          type: 'not',
          value: { type: 'eq', field: 'status', value: 'archived' },
        },
      });
    });
  });

  it('Given a saved definition with nested logical clauses is loaded, When it is edited and saved again, Then the logical filter round-trips without losing children', async () => {
    const nestedWhere: WhereClause = {
      type: 'or',
      value: [
        { type: 'eq', field: 'status', value: 'active' },
        {
          type: 'not',
          value: { type: 'eq', field: 'country', value: 'US' },
        },
      ],
    };
    const savedDef: ObjectSetDefinition = {
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Employee' },
      where: nestedWhere,
    };
    window.localStorage.setItem(
      localStorageKey('test'),
      JSON.stringify([
        {
          id: 's1',
          name: 'Nested saved',
          def: savedDef,
          createdAt: '2026-05-19T00:00:00.000Z',
          versions: [
            {
              versionId: 'v1',
              def: savedDef,
              createdAt: '2026-05-19T00:00:00.000Z',
            },
          ],
          activeVersionId: 'v1',
        },
      ]),
    );

    renderObjectSetPage();

    fireEvent.click(screen.getByRole('button', { name: 'Nested saved' }));

    await waitFor(() => {
      expect(screen.getAllByRole('combobox', { name: /where type/i })[0]).toHaveValue('or');
    });
    fireEvent.change(screen.getByDisplayValue('active'), {
      target: { value: 'archived' },
    });
    fireEvent.click(screen.getByRole('button', { name: /save as/i }));
    fireEvent.change(screen.getByLabelText(/save name/i), {
      target: { value: 'Nested copy' },
    });
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() => {
      const saved = JSON.parse(window.localStorage.getItem(localStorageKey('test')) ?? '[]') as Array<{
        name: string;
        def: ObjectSetDefinition;
      }>;
      const copy = saved.find((item) => item.name === 'Nested copy');
      expect(copy?.def).toEqual({
        type: 'filter',
        objectSet: { type: 'base', objectType: 'Employee' },
        where: {
          type: 'or',
          value: [
            { type: 'eq', field: 'status', value: 'archived' },
            {
              type: 'not',
              value: { type: 'eq', field: 'country', value: 'US' },
            },
          ],
        },
      });
    });
  });
});
