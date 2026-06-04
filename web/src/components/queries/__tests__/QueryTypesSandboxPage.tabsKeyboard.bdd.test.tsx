import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryTypesSandboxPage } from '../QueryTypesSandboxPage';
import * as ontologiesApi from '../../../api/ontologies';
import type { QueryType } from '../../../api/types';

// BDD: the result-view tablist (Table / JSON) inside QueryTypesSandboxPage
// must honour the WAI-ARIA tabs keyboard contract — ArrowLeft/Right (with
// Down/Up mirrors) move + activate the adjacent tab with wrap-around,
// Home/End jump to the ends, and roving tabindex keeps a single tab in the
// natural tab order. Mirrors the AggregationPage / MetricsPage fixes.

const queryType: QueryType = {
  rid: 'ri.ontology.main.querytype.echo',
  apiName: 'echoQuery',
  displayName: 'Echo Query',
  description: 'Returns its input',
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

// Selects the query type and runs it so the result-view tablist renders.
async function renderWithResult() {
  vi.spyOn(ontologiesApi, 'listQueryTypes').mockResolvedValue([queryType]);
  vi.spyOn(ontologiesApi, 'executeQueryType').mockResolvedValue({
    value: [{ id: 1, name: 'Alice' }],
  });
  renderPage();

  const selectBtn = await screen.findByTestId('query-type-select-echoQuery');
  fireEvent.click(selectBtn);
  fireEvent.click(await screen.findByTestId('query-type-execute-button'));
  await screen.findByTestId('query-result-panel');
}

describe('BDD: QueryTypesSandboxPage result-view tablist keyboard navigation (WAI-ARIA tabs)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('Given the Table tab is focused, When ArrowRight is pressed, Then focus and selection move to JSON and wrap back to Table', async () => {
    const user = userEvent.setup();
    await renderWithResult();

    const tableTab = screen.getByTestId('query-result-tab-table');
    const jsonTab = screen.getByTestId('query-result-tab-json');

    tableTab.focus();
    expect(tableTab).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(jsonTab).toHaveFocus();
    expect(jsonTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('query-result-json')).toBeInTheDocument();

    // Wrap-around from the last tab back to the first.
    await user.keyboard('{ArrowRight}');
    expect(tableTab).toHaveFocus();
    expect(tableTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('query-result-table')).toBeInTheDocument();
  });

  it('Given the Table tab is focused, When ArrowLeft is pressed, Then focus and selection wrap to the last tab (JSON)', async () => {
    const user = userEvent.setup();
    await renderWithResult();

    const tableTab = screen.getByTestId('query-result-tab-table');
    const jsonTab = screen.getByTestId('query-result-tab-json');

    tableTab.focus();

    await user.keyboard('{ArrowLeft}');
    expect(jsonTab).toHaveFocus();
    expect(jsonTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('query-result-json')).toBeInTheDocument();
  });

  it('Given any tab is focused, When Home/End are pressed, Then focus and selection jump to the first/last tab', async () => {
    const user = userEvent.setup();
    await renderWithResult();

    const tableTab = screen.getByTestId('query-result-tab-table');
    const jsonTab = screen.getByTestId('query-result-tab-json');

    tableTab.focus();

    await user.keyboard('{End}');
    expect(jsonTab).toHaveFocus();
    expect(jsonTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{Home}');
    expect(tableTab).toHaveFocus();
    expect(tableTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the Down/Up mirror keys, When pressed, Then they behave like ArrowRight/ArrowLeft', async () => {
    const user = userEvent.setup();
    await renderWithResult();

    const tableTab = screen.getByTestId('query-result-tab-table');
    const jsonTab = screen.getByTestId('query-result-tab-json');

    tableTab.focus();

    await user.keyboard('{ArrowDown}');
    expect(jsonTab).toHaveFocus();
    expect(jsonTab).toHaveAttribute('aria-selected', 'true');

    await user.keyboard('{ArrowUp}');
    expect(tableTab).toHaveFocus();
    expect(tableTab).toHaveAttribute('aria-selected', 'true');
  });

  it('Given the tablist follows the roving tabindex pattern, Then only the selected tab is in the tab order, and mouse click still works', async () => {
    await renderWithResult();

    const tableTab = screen.getByTestId('query-result-tab-table');
    const jsonTab = screen.getByTestId('query-result-tab-json');

    // Table is the default selection.
    expect(tableTab).toHaveAttribute('tabindex', '0');
    expect(jsonTab).toHaveAttribute('tabindex', '-1');

    // Mouse click still works and updates roving tabindex + selection.
    fireEvent.click(jsonTab);
    expect(jsonTab).toHaveAttribute('aria-selected', 'true');
    expect(jsonTab).toHaveAttribute('tabindex', '0');
    expect(tableTab).toHaveAttribute('tabindex', '-1');
    expect(screen.getByTestId('query-result-json')).toBeInTheDocument();
  });
});
