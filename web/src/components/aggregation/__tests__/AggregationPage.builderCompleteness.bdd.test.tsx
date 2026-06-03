import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

let capturedAggregateBody: unknown = null;

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
        orderID: { dataType: { type: 'string' }, rid: 'ri.p.order.id' },
        shipCountry: { dataType: { type: 'string' }, rid: 'ri.p.order.country' },
        freight: { dataType: { type: 'double' }, rid: 'ri.p.order.freight' },
        orderDate: { dataType: { type: 'timestamp' }, rid: 'ri.p.order.date' },
        location: { dataType: { type: 'geopoint' }, rid: 'ri.p.order.loc' },
      },
    }),
  ),
  http.post('/api/v2/ontologies/:ontology/objects/:objectType/aggregate', async ({ request }) => {
    capturedAggregateBody = await request.json();
    return HttpResponse.json({
      data: [{ metrics: [{ name: 'm', value: 1 }] }],
      accuracy: 'ACCURATE',
    });
  }),
);

beforeAll(() => server.listen());
afterEach(() => {
  capturedAggregateBody = null;
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

function bodyAs() {
  return capturedAggregateBody as {
    aggregation?: Array<Record<string, unknown>>;
    groupBy?: Array<Record<string, unknown>>;
  };
}

describe('BDD: Aggregation builder completeness — A1 metric types', () => {
  it('Given the metric type select, Then it offers all 11 backend metric types', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    const typeSelect = screen.getByTestId('metric-0-type') as HTMLSelectElement;
    const options = Array.from(typeSelect.options).map((o) => o.value);
    expect(options).toEqual([
      'count',
      'min',
      'max',
      'sum',
      'avg',
      'approximateDistinct',
      'exactDistinct',
      'standardDeviation',
      'variance',
      'approximatePercentile',
      'collectList',
    ]);
  });

  it('Given an approximatePercentile metric with a percentile, When Execute, Then the request carries percentile as a number', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.change(screen.getByTestId('metric-0-type'), {
      target: { value: 'approximatePercentile' },
    });
    fireEvent.change(screen.getByTestId('metric-0-field'), {
      target: { value: 'freight' },
    });
    fireEvent.change(screen.getByTestId('metric-0-percentile'), {
      target: { value: '95' },
    });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    expect(bodyAs().aggregation?.[0]).toEqual({
      type: 'approximatePercentile',
      field: 'freight',
      percentile: 95,
    });
  });

  it('Given a percentile is set then the metric type is switched away, When Execute, Then the stale percentile key is dropped', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.change(screen.getByTestId('metric-0-type'), {
      target: { value: 'approximatePercentile' },
    });
    fireEvent.change(screen.getByTestId('metric-0-field'), { target: { value: 'freight' } });
    fireEvent.change(screen.getByTestId('metric-0-percentile'), { target: { value: '95' } });
    // Switch to collectList — percentile must not leak onto the new metric.
    fireEvent.change(screen.getByTestId('metric-0-type'), { target: { value: 'collectList' } });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    const metric = bodyAs().aggregation?.[0] ?? {};
    expect(Object.prototype.hasOwnProperty.call(metric, 'percentile')).toBe(false);
    expect(metric.type).toBe('collectList');
  });

  it('Given a collectList metric with maxItems, When Execute, Then the request carries maxItems as a number', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.change(screen.getByTestId('metric-0-type'), {
      target: { value: 'collectList' },
    });
    fireEvent.change(screen.getByTestId('metric-0-field'), {
      target: { value: 'shipCountry' },
    });
    fireEvent.change(screen.getByTestId('metric-0-maxItems'), {
      target: { value: '25' },
    });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    expect(bodyAs().aggregation?.[0]).toEqual({
      type: 'collectList',
      field: 'shipCountry',
      maxItems: 25,
    });
  });
});

describe('BDD: Aggregation builder completeness — A2 groupBy types', () => {
  it('Given the groupBy type select, Then it offers all 7 backend groupBy types', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    const typeSelect = screen.getByTestId('groupby-0-type') as HTMLSelectElement;
    const options = Array.from(typeSelect.options).map((o) => o.value);
    expect(options).toEqual([
      'exact',
      'fixedWidth',
      'ranges',
      'duration',
      'topValues',
      'geohash',
    ]);
  });

  it('Given the duration period select, Then it only offers periods the backend parseDuration accepts', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'duration' } });
    const periodSelect = screen.getByTestId('groupby-0-duration') as HTMLSelectElement;
    const options = Array.from(periodSelect.options).map((o) => o.value);
    // P3M / PT1H are unsupported by the backend and must not be offered.
    expect(options).toEqual(['P1D', 'P1W', 'P1M', 'P1Y']);
  });

  it('Given a duration groupBy with a period, When Execute, Then groupBy carries duration string', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), { target: { value: 'orderDate' } });
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'duration' } });
    fireEvent.change(screen.getByTestId('groupby-0-duration'), { target: { value: 'P1M' } });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    expect(bodyAs().groupBy?.[0]).toEqual({
      field: 'orderDate',
      type: 'duration',
      duration: 'P1M',
    });
  });

  it('Given a topValues groupBy with maxGroupCount, When Execute, Then groupBy carries maxGroupCount as a number', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), { target: { value: 'shipCountry' } });
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'topValues' } });
    fireEvent.change(screen.getByTestId('groupby-0-maxGroupCount'), { target: { value: '7' } });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    expect(bodyAs().groupBy?.[0]).toEqual({
      field: 'shipCountry',
      type: 'topValues',
      maxGroupCount: 7,
    });
  });

  it('Given a geohash groupBy with precision, When Execute, Then groupBy carries precision as a number (json key precision)', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), { target: { value: 'location' } });
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'geohash' } });
    fireEvent.change(screen.getByTestId('groupby-0-geohashPrecision'), { target: { value: '5' } });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    expect(bodyAs().groupBy?.[0]).toEqual({
      field: 'location',
      type: 'geohash',
      precision: 5,
    });
  });
});

describe('BDD: Aggregation builder completeness — A3 ranges editor + guard', () => {
  it('Given a ranges groupBy with no rows, Then Execute is disabled (silent-wrong-result guard)', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), { target: { value: 'freight' } });
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'ranges' } });

    expect(screen.getByTestId('aggregation-execute')).toBeDisabled();
  });

  it('Given a ranges groupBy with a row, When Execute, Then groupBy carries the typed ranges array', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), { target: { value: 'freight' } });
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'ranges' } });

    fireEvent.click(screen.getByTestId('groupby-0-range-add'));
    fireEvent.change(screen.getByTestId('groupby-0-range-0-name'), { target: { value: 'low' } });
    fireEvent.change(screen.getByTestId('groupby-0-range-0-start'), { target: { value: '0' } });
    fireEvent.change(screen.getByTestId('groupby-0-range-0-end'), { target: { value: '100' } });

    expect(screen.getByTestId('aggregation-execute')).not.toBeDisabled();
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    expect(bodyAs().groupBy?.[0]).toEqual({
      field: 'freight',
      type: 'ranges',
      ranges: [{ name: 'low', startValue: 0, endValue: 100 }],
    });
  });

  it('Given a ranges row is removed leaving zero rows, Then Execute is disabled again', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), { target: { value: 'freight' } });
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'ranges' } });
    fireEvent.click(screen.getByTestId('groupby-0-range-add'));
    expect(screen.getByTestId('aggregation-execute')).not.toBeDisabled();
    fireEvent.click(screen.getByTestId('groupby-0-range-0-remove'));
    expect(screen.getByTestId('aggregation-execute')).toBeDisabled();
  });
});

describe('BDD: Aggregation builder completeness — type switch strips stale keys', () => {
  it('Given ranges rows are entered then type is switched to exact, When Execute, Then no stale ranges array is sent', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), { target: { value: 'freight' } });
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'ranges' } });
    fireEvent.click(screen.getByTestId('groupby-0-range-add'));
    fireEvent.change(screen.getByTestId('groupby-0-range-0-start'), { target: { value: '0' } });
    // Switch back to exact — the ranges array must not leak.
    fireEvent.change(screen.getByTestId('groupby-0-type'), { target: { value: 'exact' } });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    const clause = bodyAs().groupBy?.[0] ?? {};
    expect(Object.prototype.hasOwnProperty.call(clause, 'ranges')).toBe(false);
    expect(clause).toEqual({ field: 'freight', type: 'exact' });
  });
});

describe('BDD: Aggregation builder completeness — A4 maxGroupCount on exact', () => {
  it('Given an exact groupBy with maxGroupCount, When Execute, Then groupBy carries maxGroupCount as a number', async () => {
    renderPage();
    await screen.findByTestId('aggregation-execute');
    fireEvent.click(screen.getByTestId('groupby-add'));
    fireEvent.change(screen.getByTestId('groupby-0-field'), { target: { value: 'shipCountry' } });
    // type stays 'exact'
    fireEvent.change(screen.getByTestId('groupby-0-maxGroupCount'), { target: { value: '250' } });
    fireEvent.click(screen.getByTestId('aggregation-execute'));

    await waitFor(() => expect(capturedAggregateBody).not.toBeNull());
    expect(bodyAs().groupBy?.[0]).toEqual({
      field: 'shipCountry',
      type: 'exact',
      maxGroupCount: 250,
    });
  });
});
