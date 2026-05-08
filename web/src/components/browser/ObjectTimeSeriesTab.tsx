// US-457: TimeSeries tab on the object-detail panel. Renders one chart
// per property whose dataType.type === "timeseries", auto-fetching points
// from the existing /streamPoints endpoint via the same hook that powers
// the Quiver workbench. Falls back to an empty-state when an object has
// no timeseries-typed properties.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { MultiSeriesChart, type ChartSeries } from '../quiver/MultiSeriesChart';
import { useTimeSeriesPoints } from '../../hooks/useTimeSeries';
import type { TimeSeriesPoint } from '../../api/timeseries';
import { baseTypeOf } from '../../lib/geoParser';
import type { ObjectType } from '../../api/types';

const SERIES_PALETTE = [
  '#2563eb',
  '#9333ea',
  '#16a34a',
  '#dc2626',
  '#0891b2',
  '#ea580c',
  '#7c3aed',
  '#0284c7',
];

interface SeriesFetcherProps {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  property: string;
  onLoaded: (
    property: string,
    points: TimeSeriesPoint[],
    status: 'loading' | 'ready' | 'error',
  ) => void;
}

function SeriesFetcher({
  ontologyApiName,
  objectType,
  primaryKey,
  property,
  onLoaded,
}: SeriesFetcherProps) {
  const { data, isLoading, isError } = useTimeSeriesPoints({
    ontologyApiName,
    objectType,
    primaryKey,
    property,
  });
  const status = isLoading ? 'loading' : isError ? 'error' : 'ready';
  useEffect(() => {
    onLoaded(property, data ?? [], status);
  }, [property, data, status, onLoaded]);
  return null;
}

interface ObjectTimeSeriesTabProps {
  ontologyApiName: string;
  objectType: ObjectType;
  primaryKey: string;
}

interface PropState {
  points: TimeSeriesPoint[];
  status: 'loading' | 'ready' | 'error';
}

export function ObjectTimeSeriesTab({
  ontologyApiName,
  objectType,
  primaryKey,
}: ObjectTimeSeriesTabProps) {
  const tsProperties = useMemo(() => {
    if (!objectType.properties) return [] as string[];
    return Object.entries(objectType.properties)
      .filter(([, prop]) => baseTypeOf(prop.dataType) === 'timeseries')
      .map(([name]) => name)
      .sort();
  }, [objectType.properties]);

  const [byProperty, setByProperty] = useState<Record<string, PropState>>({});

  const handleLoaded = useCallback(
    (property: string, points: TimeSeriesPoint[], status: PropState['status']) => {
      setByProperty((prev) => {
        const cur = prev[property];
        if (cur && cur.status === status && cur.points === points) return prev;
        return { ...prev, [property]: { points, status } };
      });
    },
    [],
  );

  // Reset cached series state when the targeted object changes so a stale
  // chart from a previous selection does not flash on initial mount.
  useEffect(() => {
    setByProperty({});
  }, [primaryKey, objectType.apiName]);

  if (tsProperties.length === 0) {
    return (
      <div
        data-testid="ts-tab-empty"
        className="text-xs text-text-secondary px-1 py-4"
      >
        This object type has no timeseries properties.
      </div>
    );
  }

  return (
    <div className="space-y-4" data-testid="ts-tab">
      {tsProperties.map((property) => (
        <SeriesFetcher
          key={`fetch-${property}`}
          ontologyApiName={ontologyApiName}
          objectType={objectType.apiName}
          primaryKey={primaryKey}
          property={property}
          onLoaded={handleLoaded}
        />
      ))}
      {tsProperties.map((property, idx) => {
        const state = byProperty[property];
        const series: ChartSeries[] = state && state.status === 'ready'
          ? [
              {
                id: property,
                label: property,
                color: SERIES_PALETTE[idx % SERIES_PALETTE.length],
                points: state.points,
              },
            ]
          : [];
        return (
          <section
            key={property}
            data-testid={`ts-tab-property-${property}`}
            className="rounded-md border border-border p-3"
          >
            <header className="flex items-center justify-between mb-2">
              <h3 className="text-xs font-mono font-medium text-text-primary">
                {property}
              </h3>
              <span
                data-testid={`ts-tab-status-${property}`}
                className="text-[10px] uppercase tracking-wide text-text-secondary"
              >
                {state ? state.status : 'loading'}
              </span>
            </header>
            {state?.status === 'ready' && state.points.length === 0 && (
              <div
                data-testid={`ts-tab-nopoints-${property}`}
                className="text-xs text-text-secondary py-6 text-center"
              >
                No points recorded for this series yet.
              </div>
            )}
            {state?.status === 'error' && (
              <div
                data-testid={`ts-tab-error-${property}`}
                className="text-xs text-rose-700 py-6 text-center"
              >
                Failed to load timeseries data.
              </div>
            )}
            {state?.status === 'ready' && state.points.length > 0 && (
              <MultiSeriesChart series={series} height={200} />
            )}
          </section>
        );
      })}
    </div>
  );
}
