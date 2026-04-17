import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MapView } from '../MapView';
import type { ObjectType, WireObject } from '../../../api/types';

// -----------------------------------------------------------------------------
// react-leaflet is mocked so we can verify props declaratively without needing
// a real canvas/DOM environment (leaflet requires browser APIs that jsdom does
// not provide).
// -----------------------------------------------------------------------------

type CircleMarkerProps = {
  center: [number, number];
  children?: ReactNode;
};
type GeoJSONMockProps = {
  data: unknown;
  children?: ReactNode;
};

const circleMarkerCalls: CircleMarkerProps[] = [];
const geoJSONCalls: GeoJSONMockProps[] = [];

vi.mock('react-leaflet', () => ({
  MapContainer: ({ children, center, zoom }: { children?: ReactNode; center: [number, number]; zoom: number }) => (
    <div
      data-testid="mock-map"
      data-center={JSON.stringify(center)}
      data-zoom={zoom}
    >
      {children}
    </div>
  ),
  TileLayer: ({ url }: { url: string }) => (
    <div data-testid="mock-tile-layer" data-url={url} />
  ),
  CircleMarker: (props: CircleMarkerProps) => {
    circleMarkerCalls.push(props);
    return (
      <div
        data-testid="mock-circle-marker"
        data-center={JSON.stringify(props.center)}
      >
        {props.children}
      </div>
    );
  },
  GeoJSON: (props: GeoJSONMockProps) => {
    geoJSONCalls.push(props);
    return (
      <div
        data-testid="mock-geojson"
        data-type={(props.data as { type?: string })?.type ?? ''}
      >
        {props.children}
      </div>
    );
  },
  Popup: ({ children }: { children?: ReactNode }) => (
    <div data-testid="mock-popup">{children}</div>
  ),
}));

vi.mock('leaflet/dist/leaflet.css', () => ({}));

// -----------------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------------

const employeeType: ObjectType = {
  rid: 'ri.ot.employee',
  apiName: 'Employee',
  displayName: 'Employee',
  primaryKey: 'id',
  titleProperty: 'name',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
    location: { dataType: { type: 'geopoint' }, rid: 'ri.p.loc' },
    region: { dataType: { type: 'geoshape' }, rid: 'ri.p.region' },
  },
};

const plainType: ObjectType = {
  rid: 'ri.ot.plain',
  apiName: 'Plain',
  displayName: 'Plain',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
  },
};

const rowAlice: WireObject = {
  __rid: 'ri.o.1',
  __primaryKey: '1',
  __apiName: 'Employee',
  id: '1',
  name: 'Alice',
  location: { type: 'Point', coordinates: [-74.006, 40.7128] },
  region: {
    type: 'Polygon',
    coordinates: [
      [
        [0, 0],
        [1, 0],
        [1, 1],
        [0, 1],
        [0, 0],
      ],
    ],
  },
};

const rowBob: WireObject = {
  __rid: 'ri.o.2',
  __primaryKey: '2',
  __apiName: 'Employee',
  id: '2',
  name: 'Bob',
  location: '34.0522,-118.2437',
};

const rowNoGeo: WireObject = {
  __rid: 'ri.o.3',
  __primaryKey: '3',
  __apiName: 'Employee',
  id: '3',
  name: 'Carol',
};

beforeEach(() => {
  circleMarkerCalls.length = 0;
  geoJSONCalls.length = 0;
});

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

describe('MapView', () => {
  it('renders empty state when no geo properties are declared', () => {
    render(<MapView objectType={plainType} data={[rowNoGeo]} />);
    expect(screen.getByTestId('map-view-empty')).toBeInTheDocument();
    expect(screen.queryByTestId('mock-map')).not.toBeInTheDocument();
  });

  it('renders an OSM tile layer and a marker for a parsed geopoint', () => {
    render(<MapView objectType={employeeType} data={[rowAlice]} />);
    expect(screen.getByTestId('mock-map')).toBeInTheDocument();
    const tile = screen.getByTestId('mock-tile-layer');
    expect(tile.getAttribute('data-url')).toContain(
      'tile.openstreetmap.org',
    );
    const markers = screen.getAllByTestId('mock-circle-marker');
    expect(markers).toHaveLength(1);
    expect(markers[0].getAttribute('data-center')).toBe(
      JSON.stringify([40.7128, -74.006]),
    );
  });

  it('parses "lat,lon" string geopoints', () => {
    render(<MapView objectType={employeeType} data={[rowBob]} />);
    const markers = screen.getAllByTestId('mock-circle-marker');
    expect(markers).toHaveLength(1);
    expect(markers[0].getAttribute('data-center')).toBe(
      JSON.stringify([34.0522, -118.2437]),
    );
  });

  it('renders a GeoJSON layer for geoshape properties', () => {
    render(<MapView objectType={employeeType} data={[rowAlice]} />);
    const shapes = screen.getAllByTestId('mock-geojson');
    expect(shapes).toHaveLength(1);
    expect(shapes[0].getAttribute('data-type')).toBe('Polygon');
  });

  it('skips rows with missing/unparseable geo values silently', () => {
    render(
      <MapView objectType={employeeType} data={[rowAlice, rowNoGeo]} />,
    );
    // Only rowAlice contributes one marker + one shape
    expect(screen.getAllByTestId('mock-circle-marker')).toHaveLength(1);
    expect(screen.getAllByTestId('mock-geojson')).toHaveLength(1);
  });

  it('shows a "No geospatial data" overlay when the page has no parseable geo', () => {
    render(
      <MapView objectType={employeeType} data={[rowNoGeo]} />,
    );
    expect(screen.getByTestId('map-view-no-data')).toBeInTheDocument();
  });

  it('popup exposes the title property and primary key, and invokes onRowClick', () => {
    const onRowClick = vi.fn();
    render(
      <MapView
        objectType={employeeType}
        data={[rowAlice]}
        onRowClick={onRowClick}
      />,
    );
    const popup = screen.getAllByTestId('mock-popup')[0];
    expect(within(popup).getByText('Alice')).toBeInTheDocument();
    expect(within(popup).getByText('1')).toBeInTheDocument();
    const openButtons = screen.getAllByTestId('map-popup-open-1');
    fireEvent.click(openButtons[0]);
    expect(onRowClick).toHaveBeenCalledWith(rowAlice);
  });
});
