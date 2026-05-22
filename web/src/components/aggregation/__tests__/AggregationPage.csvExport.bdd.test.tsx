import { describe, it, expect, beforeAll, afterAll, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { AggregationPage } from '../AggregationPage';

const csvRows = [
  {
    group: {
      shipCountry: 'USA, East',
      segment: 'Quoted "Direct"',
      metadata: { tier: 'A' },
      emptyValue: null,
    },
    metrics: [
      { name: 'orderCount', value: 30 },
      { name: 'freightTotal', value: 1234.5 },
    ],
  },
  {
    group: {
      shipCountry: 'France',
      segment: 'Retail',
      metadata: { tier: 'B' },
      emptyValue: null,
    },
    metrics: [
      { name: 'orderCount', value: 12 },
      { name: 'freightTotal', value: 456.75 },
    ],
  },
];

const emptyRows: typeof csvRows = [];

let aggregateRows = csvRows;

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
        segment: { dataType: { type: 'string' }, rid: 'ri.p.order.segment' },
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
  aggregateRows = csvRows;
  server.resetHandlers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
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

async function executeAggregation() {
  renderPage();
  fireEvent.click(await screen.findByTestId('groupby-add'));
  fireEvent.change(await screen.findByTestId('groupby-0-field'), {
    target: { value: 'shipCountry' },
  });
  fireEvent.change(screen.getByTestId('metric-0-type'), {
    target: { value: 'sum' },
  });
  fireEvent.change(screen.getByTestId('metric-0-field'), {
    target: { value: 'freight' },
  });
  fireEvent.change(screen.getByTestId('metric-0-name'), {
    target: { value: 'freightTotal' },
  });
  fireEvent.click(screen.getByTestId('aggregation-execute'));
  await screen.findByTestId('aggregation-results');
}

describe('BDD: AggregationPage CSV export (P2B-001)', () => {
  it('Given aggregation buckets are visible, When Export CSV is clicked, Then a CSV file with group and metric columns is downloaded', async () => {
    const blobs: Blob[] = [];
    const createdUrls: string[] = [];
    vi.spyOn(URL, 'createObjectURL').mockImplementation((obj: Blob | MediaSource) => {
      if (obj instanceof Blob) blobs.push(obj);
      const url = `blob:aggregation-${createdUrls.length + 1}`;
      createdUrls.push(url);
      return url;
    });
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});

    const clickedDownloads: string[] = [];
    const originalCreateElement = document.createElement.bind(document);
    vi.spyOn(document, 'createElement').mockImplementation((tagName: string) => {
      const el = originalCreateElement(tagName);
      if (tagName.toLowerCase() === 'a') {
        const anchor = el as HTMLAnchorElement;
        anchor.click = () => {
          clickedDownloads.push(anchor.download);
        };
      }
      return el;
    });

    await executeAggregation();

    fireEvent.click(screen.getByRole('button', { name: /export csv/i }));

    await waitFor(() => {
      expect(blobs).toHaveLength(1);
      expect(clickedDownloads).toEqual(['northwind-order-aggregation.csv']);
    });
    expect(blobs[0]!.type).toBe('text/csv;charset=utf-8');
    await expect(blobs[0]!.text()).resolves.toBe(
      [
        'shipCountry,segment,metadata,emptyValue,orderCount,freightTotal',
        '"USA, East","Quoted ""Direct""","{""tier"":""A""}",,30,1234.5',
        'France,Retail,"{""tier"":""B""}",,12,456.75',
        '',
      ].join('\n'),
    );
  });

  it('Given aggregation returns no buckets, When the results panel renders, Then Export CSV is hidden', async () => {
    aggregateRows = emptyRows;

    await executeAggregation();

    expect(screen.queryByRole('button', { name: /export csv/i })).not.toBeInTheDocument();
  });
});
