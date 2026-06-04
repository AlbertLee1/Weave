import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createElement } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectDetail } from '../ObjectDetail';
import type { ObjectType, WireObject } from '../../../api/types';

// React-diff-viewer-continued is a heavy import pulled by ObjectDiffPanel via
// ObjectDetail; stub it so jsdom can render without the worker bundle.
vi.mock('react-diff-viewer-continued', () => ({
  __esModule: true,
  default: () => null,
  DiffMethod: { LINES: 'diffLines' },
}));

// Mock the markdown editor too (same pattern as the sibling ObjectDetail test).
vi.mock('@uiw/react-md-editor', () => {
  const Editor = () => null;
  (Editor as unknown as { Markdown: () => null }).Markdown = () => null;
  return { default: Editor };
});

const server = setupServer();

beforeEach(() => {
  server.listen({ onUnhandledRequest: 'bypass' });
});

afterEach(() => {
  server.resetHandlers();
  server.close();
  vi.clearAllMocks();
});

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  return ({ children }: { children: React.ReactNode }) =>
    createElement(
      MemoryRouter,
      null,
      createElement(QueryClientProvider, { client }, children),
    );
}

// Mounts the supporting queries ObjectDetail fans out on mount so they resolve
// cleanly (empty actionTypes/properties, no outgoing link types).
function mountSupportingQueries() {
  server.use(
    http.get('/api/v2/ontologies/:ontology/actionTypes', () =>
      HttpResponse.json({ data: [] }),
    ),
    http.get('/api/v2/ontologies/:ontology/properties', () =>
      HttpResponse.json({ data: [] }),
    ),
    http.get(
      '/api/v2/ontologies/:ontology/objectTypes/:objectType/outgoingLinkTypes',
      () => HttpResponse.json({ data: [] }),
    ),
  );
}

const wireObject: WireObject = {
  __rid: 'ri.o.v1',
  __apiName: 'Vehicle',
  __primaryKey: 'v1',
  vehicleId: 'v1',
};

const vehicleWithTrack: ObjectType = {
  rid: 'ri.ontology.main.object-type.vehicle',
  apiName: 'Vehicle',
  displayName: 'Vehicle',
  primaryKey: 'vehicleId',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    vehicleId: { dataType: { type: 'string' }, rid: 'ri.p.vehicleId' },
    // GeotemporalSeriesProperty — base type `geotimeseries` mirrors the
    // backend index mapping (pkg/oss/handlers_geotemporal_test.go).
    track: { dataType: { type: 'geotimeseries' }, rid: 'ri.p.track' },
  },
};

const vehicleNoTrack: ObjectType = {
  rid: 'ri.ontology.main.object-type.vehicle',
  apiName: 'Vehicle',
  displayName: 'Vehicle',
  primaryKey: 'vehicleId',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    vehicleId: { dataType: { type: 'string' }, rid: 'ri.p.vehicleId' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
  },
};

describe('BDD: ObjectDetail geotemporal series', () => {
  it('Given an object with a geotimeseries property, When the historic values endpoint returns a series, Then the geotemporal section renders the (time, position) readings', async () => {
    mountSupportingQueries();

    let captured: { method: string; path: string } | null = null;
    server.use(
      http.post(
        '/api/v2/ontologies/:ontology/objects/:objectType/:primaryKey/geotemporalSeries/:property/streamHistoricValues',
        ({ request }) => {
          const url = new URL(request.url);
          captured = { method: request.method, path: url.pathname };
          return HttpResponse.json([
            {
              time: '2026-04-01T00:00:00Z',
              position: { type: 'Point', coordinates: [-122.41, 37.77] },
            },
            {
              time: '2026-04-02T00:00:00Z',
              position: { type: 'Point', coordinates: [-122.42, 37.78] },
            },
          ]);
        },
      ),
    );

    render(
      <ObjectDetail
        object={wireObject}
        objectType={vehicleWithTrack}
        open
        onClose={vi.fn()}
        ontologyApiName="main"
      />,
      { wrapper: makeWrapper() },
    );

    // Then — the geotemporal section renders with the property's readings.
    await waitFor(() =>
      expect(
        screen.getByTestId('object-detail-geotemporal'),
      ).toBeInTheDocument(),
    );
    const section = await screen.findByTestId('geotemporal-series-track');
    // Both timestamps surface as rows.
    expect(section).toHaveTextContent('2026-04-01T00:00:00Z');
    expect(section).toHaveTextContent('2026-04-02T00:00:00Z');
    // Coordinates from the GeoJSON Point are shown (lat / lng both present).
    expect(section).toHaveTextContent('-122.41');
    expect(section).toHaveTextContent('37.77');

    // And the request targeted the streamHistoricValues endpoint for the
    // geotimeseries property via POST.
    expect(captured).not.toBeNull();
    const req = captured as unknown as { method: string; path: string };
    expect(req.method).toBe('POST');
    expect(req.path).toBe(
      '/api/v2/ontologies/main/objects/Vehicle/v1/geotemporalSeries/track/streamHistoricValues',
    );
  });

  it('Given an object type with no geotimeseries property, Then no geotemporal section renders', async () => {
    mountSupportingQueries();

    render(
      <ObjectDetail
        object={wireObject}
        objectType={vehicleNoTrack}
        open
        onClose={vi.fn()}
        ontologyApiName="main"
      />,
      { wrapper: makeWrapper() },
    );

    // The Properties tab content settles; with no geotimeseries property the
    // section must stay absent.
    await waitFor(() =>
      expect(
        screen.getByTestId('object-detail-properties'),
      ).toBeInTheDocument(),
    );
    expect(
      screen.queryByTestId('object-detail-geotemporal'),
    ).not.toBeInTheDocument();
  });
});
