import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
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

let aggregateRows = groupedRows;

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
      data: aggregateRows,
      accuracy: 'ACCURATE',
    }),
  ),
);

beforeAll(() => server.listen());
afterEach(() => {
  aggregateRows = groupedRows;
  server.resetHandlers();
});
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

describe('BDD: AggregationPage chart type switcher (P2B-002)', () => {
  it('Given grouped aggregation buckets, When the operator switches chart type, Then the chart renderer updates without losing the result table', async () => {
    await executeGroupedAggregation();

    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'bar');
    expect(screen.getByRole('tab', { name: /bar/i })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByTestId('aggregation-bucket-tree')).toHaveTextContent('USA');

    fireEvent.click(screen.getByRole('tab', { name: /line/i }));

    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'line');
    expect(screen.getByRole('tab', { name: /line/i })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByTestId('aggregation-chart-line')).toBeInTheDocument();
    expect(screen.getByTestId('aggregation-bucket-tree')).toHaveTextContent('France');

    fireEvent.click(screen.getByRole('tab', { name: /pie/i }));

    expect(screen.getByTestId('aggregation-chart')).toHaveAttribute('data-chart-type', 'pie');
    expect(screen.getByRole('tab', { name: /pie/i })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByTestId('aggregation-chart-pie')).toBeInTheDocument();
    expect(screen.getByTestId('aggregation-bucket-tree')).toHaveTextContent('Brazil');
  });

  it('Given no grouped buckets exist, When results render, Then chart controls remain hidden', async () => {
    aggregateRows = [];

    await executeGroupedAggregation();

    expect(screen.queryByTestId('aggregation-chart-type-tabs')).not.toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: /bar/i })).not.toBeInTheDocument();
    expect(screen.queryByTestId('aggregation-chart')).not.toBeInTheDocument();
  });
});
