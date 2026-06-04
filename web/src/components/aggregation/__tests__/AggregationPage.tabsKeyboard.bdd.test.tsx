import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

const groupedRows = [
  {
    group: { shipCountry: 'USA' },
    metrics: [{ name: 'count', value: 30 }],
  },
  {
    group: { shipCountry: 'France' },
    metrics: [{ name: 'count', value: 12 }],
  },
  {
    group: { shipCountry: 'Brazil' },
    metrics: [{ name: 'count', value: 5 }],
  },
];

const server = setupServer(
  http.get('/api/v2/ontologies/:ontology/objectTypes/:objectType', ({ params }) =>
    HttpResponse.json({
      rid: 'ri.ontology.main.object-type.order',
      apiName: String(params.objectType ?? 'order'),
      displayName: 'Order',
      primaryKey: 'orderID',
      status: 'ACTIVE',
      visibility: 'PROMINENT',
      properties: {
        shipCountry: { dataType: { type: 'string' }, rid: 'ri.p.order.country' },
        freight: { dataType: { type: 'double' }, rid: 'ri.p.order.freight' },
      },
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', () =>
    HttpResponse.json({
      data: groupedRows,
      accuracy: 'ACCURATE',
    }),
  ),
);

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/aggregation/northwind/order']}>
        <Routes>
          <Route path="/aggregation/:ontology/:objectType" element={<AggregationPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function executeGroupedAggregation() {
  renderPage();
  fireEvent.click(await screen.findByTestId('groupby-add'));
  fireEvent.change(await screen.findByTestId('groupby-0-field'), {
    target: { value: 'shipCountry' },
  });
  fireEvent.change(screen.getByTestId('metric-0-name'), {
    target: { value: 'count' },
  });
  fireEvent.click(screen.getByTestId('aggregation-execute'));
  await screen.findByTestId('aggregation-results');
}

describe('BDD: AggregationPage chart-type tablist keyboard navigation (WAI-ARIA tabs)', () => {
  it('Given the bar tab is focused, When ArrowRight is pressed, Then focus and selection move to line, then pie, and wrap back to bar', async () => {
    const user = userEvent.setup();
    await executeGroupedAggregation();

    const barTab = screen.getByRole('tab', { name: /bar/i });
    const lineTab = screen.getByRole('tab', { name: /line/i });
    const pieTab = screen.getByRole('tab', { name: /pie/i });

    barTab.focus();
    expect(barTab).toHaveFocus();

    await user.keyboard('{ArrowRight}');
    expect(lineTab).toHaveFocus();
    expect(lineTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'line');

    await user.keyboard('{ArrowRight}');
    expect(pieTab).toHaveFocus();
    expect(pieTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'pie');

    // Wrap-around from the last tab back to the first.
    await user.keyboard('{ArrowRight}');
    expect(barTab).toHaveFocus();
    expect(barTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'bar');
  });

  it('Given the bar tab is focused, When ArrowLeft is pressed, Then focus and selection wrap to the last tab (pie) and move backwards', async () => {
    const user = userEvent.setup();
    await executeGroupedAggregation();

    const barTab = screen.getByRole('tab', { name: /bar/i });
    const lineTab = screen.getByRole('tab', { name: /line/i });
    const pieTab = screen.getByRole('tab', { name: /pie/i });

    barTab.focus();

    // Wrap-around from the first tab back to the last.
    await user.keyboard('{ArrowLeft}');
    expect(pieTab).toHaveFocus();
    expect(pieTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'pie');

    await user.keyboard('{ArrowLeft}');
    expect(lineTab).toHaveFocus();
    expect(lineTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'line');
  });

  it('Given any tab is focused, When Home/End are pressed, Then focus and selection jump to the first/last tab', async () => {
    const user = userEvent.setup();
    await executeGroupedAggregation();

    const barTab = screen.getByRole('tab', { name: /bar/i });
    const pieTab = screen.getByRole('tab', { name: /pie/i });

    barTab.focus();

    await user.keyboard('{End}');
    expect(pieTab).toHaveFocus();
    expect(pieTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'pie');

    await user.keyboard('{Home}');
    expect(barTab).toHaveFocus();
    expect(barTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'bar');
  });

  it('Given the tablist follows the roving tabindex pattern, Then only the selected tab is in the tab order', async () => {
    await executeGroupedAggregation();

    const barTab = screen.getByRole('tab', { name: /bar/i });
    const lineTab = screen.getByRole('tab', { name: /line/i });
    const pieTab = screen.getByRole('tab', { name: /pie/i });

    // bar is the default selection.
    expect(barTab).toHaveAttribute('tabindex', '0');
    expect(lineTab).toHaveAttribute('tabindex', '-1');
    expect(pieTab).toHaveAttribute('tabindex', '-1');

    // Mouse click still works and updates roving tabindex.
    fireEvent.click(lineTab);
    expect(lineTab).toHaveAttribute('aria-selected', 'true');
    expect(lineTab).toHaveAttribute('tabindex', '0');
    expect(barTab).toHaveAttribute('tabindex', '-1');
    expect(pieTab).toHaveAttribute('tabindex', '-1');
    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'line');
  });
});
