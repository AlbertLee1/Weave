import { describe, it, expect, vi, beforeAll, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectSetPage } from '../ObjectSetPage';
import {
  encodeDefinitionToParam,
  OBJECT_SET_URL_PARAM,
  parseDefinitionFromSearch,
} from '../../../lib/objectSetUrl';
import type { ObjectSetDefinition } from '../../../api/types';

// Mock the ontologies hook to avoid needing real API calls for object types
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
            name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
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

beforeEach(() => {
  // Each test starts on a clean URL; the page reads window.location.search
  // for the `?def=` param outside of MemoryRouter's in-memory history.
  window.history.replaceState({}, '', '/');
});

function renderPage(ontology = 'test') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/objectsets/${ontology}`]}>
        <Routes>
          <Route path="/objectsets/:ontology" element={<ObjectSetPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ObjectSetPage', () => {
  it('renders the page title and Execute button', () => {
    renderPage();
    expect(screen.getByRole('heading', { name: /object set/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /execute/i })).toBeInTheDocument();
  });

  it('shows empty state before execution', () => {
    renderPage();
    expect(screen.getByText(/No results yet/i)).toBeInTheDocument();
  });

  it('calls loadObjects API on Execute and shows results', async () => {
    server.use(
      http.post('/api/v2/ontologies/test/objectSets/loadObjects', () =>
        HttpResponse.json({
          data: [
            { __rid: 'ri.1', __primaryKey: '1', __apiName: 'Employee', name: 'Alice' },
          ],
          totalCount: '1',
        }),
      ),
    );

    renderPage();
    fireEvent.click(screen.getByRole('button', { name: /execute/i }));

    await waitFor(() => {
      expect(screen.getByText('1')).toBeInTheDocument();
    });
  });

  it('serialises the definition into the URL on Execute', async () => {
    server.use(
      http.post('/api/v2/ontologies/test/objectSets/loadObjects', () =>
        HttpResponse.json({ data: [], totalCount: '0' }),
      ),
      http.post('/api/v2/ontologies/test/objectSets/createTemporary', () =>
        HttpResponse.json({ objectSetRid: 'ri.objectset.weave.main.tempObjectSet.0001' }),
      ),
    );

    renderPage();
    fireEvent.click(screen.getByRole('button', { name: /execute/i }));

    await waitFor(() => {
      const params = new URLSearchParams(window.location.search);
      expect(params.get(OBJECT_SET_URL_PARAM)).toBeTruthy();
    });

    const restored = parseDefinitionFromSearch(window.location.search);
    expect(restored).toEqual({ type: 'base', objectType: 'Employee' });
  });

  it('restores the definition from `?def=` on mount', async () => {
    const seed: ObjectSetDefinition = {
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Department' },
      where: { type: 'eq', field: 'name', value: 'Sales' },
    };
    const param = encodeDefinitionToParam(seed);
    window.history.replaceState({}, '', `/?${OBJECT_SET_URL_PARAM}=${param}`);

    server.use(
      http.post('/api/v2/ontologies/test/objectSets/loadObjects', async ({ request }) => {
        const body = (await request.json()) as { objectSet: ObjectSetDefinition };
        // Confirm the page replays the restored definition (filter wrapping
        // a base on Department) — not the empty default.
        expect(body.objectSet).toEqual(seed);
        return HttpResponse.json({
          data: [
            { __rid: 'ri.d.1', __primaryKey: '1', __apiName: 'Department', name: 'Sales' },
          ],
          totalCount: '1',
        });
      }),
    );

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Sales')).toBeInTheDocument();
    });
  });

  it('shows error when API fails', async () => {
    server.use(
      http.post('/api/v2/ontologies/test/objectSets/loadObjects', () =>
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
    fireEvent.click(screen.getByRole('button', { name: /execute/i }));

    await waitFor(() => {
      expect(screen.getByText(/INVALID|error|failed/i)).toBeInTheDocument();
    });
  });
});
