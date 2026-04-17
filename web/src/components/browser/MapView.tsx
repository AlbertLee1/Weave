import { useMemo } from 'react';
import {
  MapContainer,
  TileLayer,
  CircleMarker,
  GeoJSON,
  Popup,
} from 'react-leaflet';
import 'leaflet/dist/leaflet.css';
import type { ObjectType, WireObject } from '../../api/types';
import {
  baseTypeOf,
  parseGeopoint,
  parseGeoshape,
  type GeoJSONGeometry,
  type LatLng,
} from '../../lib/geoParser';

interface MapViewProps {
  objectType: ObjectType;
  data: WireObject[];
  onRowClick?: (row: WireObject) => void;
}

interface PointMarker {
  row: WireObject;
  property: string;
  latLng: LatLng;
}

interface ShapeMarker {
  row: WireObject;
  property: string;
  geometry: GeoJSONGeometry;
}

function collectGeoProperties(objectType: ObjectType): {
  pointProps: string[];
  shapeProps: string[];
} {
  const pointProps: string[] = [];
  const shapeProps: string[] = [];
  const props = objectType.properties ?? {};
  for (const [name, prop] of Object.entries(props)) {
    const base = baseTypeOf(prop.dataType);
    if (base === 'geopoint') pointProps.push(name);
    if (base === 'geoshape') shapeProps.push(name);
  }
  return { pointProps, shapeProps };
}

function extractTitle(row: WireObject, objectType: ObjectType): string {
  const titleProp = objectType.titleProperty ?? objectType.primaryKey;
  const val = row[titleProp];
  if (val === null || val === undefined) {
    return String(row.__primaryKey ?? '');
  }
  return String(val);
}

export function MapView({ objectType, data, onRowClick }: MapViewProps) {
  const { pointProps, shapeProps } = useMemo(
    () => collectGeoProperties(objectType),
    [objectType],
  );

  const markers = useMemo<PointMarker[]>(() => {
    const acc: PointMarker[] = [];
    for (const row of data) {
      for (const name of pointProps) {
        const latLng = parseGeopoint(row[name]);
        if (latLng) acc.push({ row, property: name, latLng });
      }
    }
    return acc;
  }, [data, pointProps]);

  const shapes = useMemo<ShapeMarker[]>(() => {
    const acc: ShapeMarker[] = [];
    for (const row of data) {
      for (const name of shapeProps) {
        const geom = parseGeoshape(row[name]);
        if (geom) acc.push({ row, property: name, geometry: geom });
      }
    }
    return acc;
  }, [data, shapeProps]);

  const hasGeoProperty = pointProps.length > 0 || shapeProps.length > 0;
  const hasRenderable = markers.length > 0 || shapes.length > 0;

  const defaultCenter = useMemo<LatLng>(() => {
    if (markers.length > 0) return markers[0].latLng;
    return [20, 0];
  }, [markers]);

  if (!hasGeoProperty) {
    return (
      <div
        data-testid="map-view-empty"
        className="flex flex-col items-center justify-center py-12 text-center border border-border rounded"
      >
        <p className="text-sm font-sans text-text-primary">
          No geospatial properties
        </p>
        <p className="text-xs font-mono text-text-secondary mt-1">
          Map view requires a Geopoint or Geoshape property on this object type.
        </p>
      </div>
    );
  }

  return (
    <div
      data-testid="map-view"
      className="relative w-full h-[520px] rounded overflow-hidden border border-border"
    >
      {!hasRenderable && (
        <div
          data-testid="map-view-no-data"
          className="absolute inset-0 z-[500] flex items-center justify-center bg-bg-primary/80 text-xs font-mono text-text-secondary pointer-events-none"
        >
          No geospatial data in current results
        </div>
      )}
      <MapContainer
        center={defaultCenter}
        zoom={markers.length > 0 ? 4 : 2}
        scrollWheelZoom
        className="w-full h-full"
      >
        <TileLayer
          attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
          url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
        />
        {markers.map((m, i) => (
          <CircleMarker
            key={`pt-${m.row.__rid ?? m.row.__primaryKey}-${m.property}-${i}`}
            center={m.latLng}
            radius={7}
            pathOptions={{
              color: '#22d3ee',
              fillColor: '#22d3ee',
              fillOpacity: 0.6,
              weight: 2,
            }}
          >
            <Popup>
              <MarkerPopup
                row={m.row}
                objectType={objectType}
                onOpen={onRowClick}
              />
            </Popup>
          </CircleMarker>
        ))}
        {shapes.map((s, i) => (
          <GeoJSON
            key={`sh-${s.row.__rid ?? s.row.__primaryKey}-${s.property}-${i}`}
            data={s.geometry}
            style={() => ({
              color: '#f59e0b',
              fillColor: '#f59e0b',
              fillOpacity: 0.25,
              weight: 2,
            })}
          >
            <Popup>
              <MarkerPopup
                row={s.row}
                objectType={objectType}
                onOpen={onRowClick}
              />
            </Popup>
          </GeoJSON>
        ))}
      </MapContainer>
    </div>
  );
}

interface MarkerPopupProps {
  row: WireObject;
  objectType: ObjectType;
  onOpen?: (row: WireObject) => void;
}

function MarkerPopup({ row, objectType, onOpen }: MarkerPopupProps) {
  const title = extractTitle(row, objectType);
  const pk = String(row.__primaryKey ?? '');
  return (
    <div className="text-xs font-sans space-y-1">
      <div className="font-medium text-text-primary">{title}</div>
      <div className="font-mono text-text-secondary break-all">{pk}</div>
      {onOpen && (
        <button
          type="button"
          className="text-accent-cyan hover:underline"
          onClick={() => onOpen(row)}
          data-testid={`map-popup-open-${pk}`}
        >
          Open details →
        </button>
      )}
    </div>
  );
}
