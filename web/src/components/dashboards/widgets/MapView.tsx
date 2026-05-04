import { lazy, Suspense, useMemo } from 'react';
import type { MapWidget } from './types';
import { parseGeoJSON } from './geojson';

// US-428: real leaflet rendering for the map widget. The leaflet runtime
// touches `window` at module load (ResizeObserver, image URL helpers), so
// we lazy-import the inner renderer — server-side or jsdom test runs
// without those globals fall through to the static fallback below.
//
// The legacy data-map-* attributes are preserved on the outer surface so
// US-328's tests keep passing without modification; the live leaflet
// container is mounted inside.

const LeafletInner = lazy(() => import('./MapViewLeaflet'));

function isBrowser(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    typeof document !== 'undefined' &&
    typeof document.createElement === 'function'
  );
}

export function MapView({ widget }: { widget: MapWidget }) {
  const parsed = useMemo(() => parseGeoJSON(widget.geojson), [widget.geojson]);
  const browser = isBrowser();
  return (
    <div
      data-testid="dashboard-widget-map"
      data-map-lat={widget.latitude}
      data-map-lng={widget.longitude}
      data-map-zoom={widget.zoom}
      data-map-has-geojson={parsed.ok ? 'true' : 'false'}
      className="w-full h-full relative bg-bg-secondary/40"
    >
      {browser ? (
        <Suspense fallback={<MapFallback widget={widget} />}>
          <LeafletInner
            latitude={widget.latitude}
            longitude={widget.longitude}
            zoom={widget.zoom}
            geojson={parsed.ok ? parsed.raw : null}
          />
        </Suspense>
      ) : (
        <MapFallback widget={widget} />
      )}
    </div>
  );
}

function MapFallback({ widget }: { widget: MapWidget }) {
  return (
    <div className="absolute inset-0 flex items-center justify-center text-xs font-mono text-text-secondary">
      <span className="absolute top-1 left-1">
        {widget.latitude.toFixed(4)}, {widget.longitude.toFixed(4)} · z
        {widget.zoom}
      </span>
      <span className="text-base text-accent-primary">📍</span>
    </div>
  );
}
