import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryTypesSandboxPage } from '../QueryTypesSandboxPage';
import * as ontologiesApi from '../../../api/ontologies';
import type { QueryType } from '../../../api/types';

// BDD: function-backed QueryTypes carry a `functionRid` pointing at the
// Function that backs them. The wire shape ships it (pkg/oms/models.go
// FunctionRID -> wire["functionRid"]); the sandbox metadata pane must
// surface it so operators can see which Function executes a query.

const FUNCTION_RID = 'ri.function.main.function.top-customers';

const fnBackedQueryType: QueryType = {
  rid: 'ri.ontology.main.querytype.fn',
  apiName: 'fnBackedQuery',
  displayName: 'Function Backed Query',
  description: 'Backed by an embedded Function',
  status: 'ACTIVE',
  functionRid: FUNCTION_RID,
  parameters: [],
  output: {},
  query: {},
};

const plainQueryType: QueryType = {
  rid: 'ri.ontology.main.querytype.plain',
  apiName: 'plainQuery',
  displayName: 'Plain Query',
  description: 'No backing Function',
  status: 'ACTIVE',
  parameters: [],
  output: {},
  query: {},
};

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/queries/northwind']}>
        <Routes>
          <Route
            path="/queries/:ontology"
            element={<QueryTypesSandboxPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('QueryTypesSandboxPage functionRid display (BDD)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('Given a QueryType with a functionRid, When selected, Then the sandbox shows the functionRid', async () => {
    vi.spyOn(ontologiesApi, 'listQueryTypes').mockResolvedValue([
      fnBackedQueryType,
    ]);
    renderPage();

    const selectBtn = await screen.findByTestId(
      'query-type-select-fnBackedQuery',
    );
    fireEvent.click(selectBtn);

    const detail = await screen.findByTestId('query-type-detail');
    const fnEl = within(detail).getByTestId('query-type-function-rid');
    expect(fnEl).toHaveTextContent(FUNCTION_RID);
  });

  it('Given a QueryType without a functionRid, When selected, Then the functionRid row is not rendered', async () => {
    vi.spyOn(ontologiesApi, 'listQueryTypes').mockResolvedValue([
      plainQueryType,
    ]);
    renderPage();

    const selectBtn = await screen.findByTestId(
      'query-type-select-plainQuery',
    );
    fireEvent.click(selectBtn);

    await screen.findByTestId('query-type-detail');
    expect(screen.queryByTestId('query-type-function-rid')).toBeNull();
  });
});
