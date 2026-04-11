import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectSetPage } from '../ObjectSetPage';

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
