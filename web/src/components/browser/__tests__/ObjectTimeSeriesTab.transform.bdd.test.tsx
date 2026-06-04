// BDD: time-series transform chain in the object-detail TimeSeries tab.
//
// Scenario (Given/When/Then):
//   Given an object whose type has a timeseries property and a tab that
//         renders its raw points,
//   When  the user configures a `resample` (downsample) transform with an
//         interval + aggregation and applies it,
//   Then  the tab issues a POST to the chain-transform endpoint with a body
//         whose `source` resolves THIS object/property and whose
//         `transforms` carries the exact backend wire shape
//         ({op:"resample", params:{interval, agg}}), and the transformed
//         series replaces the raw render.
//
// We spy on the api wrapper (transformTimeSeries) the same way the existing
// ObjectTimeSeriesTab.test.tsx spies on streamTimeSeriesPoints, so the
// assertion pins the request shape that crosses the wire to the backend.

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectTimeSeriesTab } from '../ObjectTimeSeriesTab';
import * as timeseriesApi from '../../../api/timeseries';
import type { ObjectType } from '../../../api/types';

function makeObjectType(
  properties: Record<string, { type: string }>,
): ObjectType {
  return {
    rid: 'ri.ontology.main.object-type.uuid',
    apiName: 'Sensor',
    displayName: 'Sensor',
    primaryKey: 'id',
    status: 'ACTIVE',
    visibility: 'NORMAL',
    properties: Object.fromEntries(
      Object.entries(properties).map(([name, dt]) => [
        name,
        { name, dataType: dt },
      ]),
    ),
  } as unknown as ObjectType;
}

function renderWithClient(ui: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity, gcTime: Infinity },
    },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('BDD: ObjectTimeSeriesTab transform chain', () => {
  it('applies a resample transform and posts the backend wire shape', async () => {
    const objectType = makeObjectType({ temperature: { type: 'timeseries' } });

    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([
      { time: '2026-05-08T00:00:00Z', value: 21.5 },
      { time: '2026-05-08T00:30:00Z', value: 22.5 },
      { time: '2026-05-08T01:00:00Z', value: 24.0 },
    ]);

    const transformSpy = vi
      .spyOn(timeseriesApi, 'transformTimeSeries')
      .mockResolvedValue({
        points: [
          { time: '2026-05-08T00:00:00Z', value: 22.0 },
          { time: '2026-05-08T01:00:00Z', value: 24.0 },
        ],
      });

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    // The raw series must render first (status flips to ready).
    await waitFor(() => {
      expect(
        screen.getByTestId('ts-tab-status-temperature').textContent,
      ).toBe('ready');
    });

    // Given: a transform builder is available for this property.
    const opSelect = screen.getByTestId('ts-transform-op-temperature');
    // When: choose resample, set interval + aggregation, then apply.
    fireEvent.change(opSelect, { target: { value: 'resample' } });

    fireEvent.change(screen.getByTestId('ts-transform-interval-temperature'), {
      target: { value: '1h' },
    });
    fireEvent.change(screen.getByTestId('ts-transform-agg-temperature'), {
      target: { value: 'avg' },
    });
    fireEvent.click(screen.getByTestId('ts-transform-apply-temperature'));

    // Then: the chain-transform endpoint is hit with the exact wire shape.
    await waitFor(() => {
      expect(transformSpy).toHaveBeenCalledTimes(1);
    });

    expect(transformSpy).toHaveBeenCalledWith(
      'default',
      expect.objectContaining({
        source: {
          objectType: 'Sensor',
          primaryKey: '42',
          property: 'temperature',
        },
        transforms: [
          { op: 'resample', params: { interval: '1h', agg: 'avg' } },
        ],
      }),
    );

    // And: the transformed-series indicator is surfaced to the user.
    await waitFor(() => {
      expect(
        screen.getByTestId('ts-transform-active-temperature'),
      ).toBeTruthy();
    });
  });

  it('supports a parameterless transform (diff) without interval inputs', async () => {
    const objectType = makeObjectType({ temperature: { type: 'timeseries' } });

    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([
      { time: '2026-05-08T00:00:00Z', value: 10 },
      { time: '2026-05-08T00:01:00Z', value: 12 },
    ]);

    const transformSpy = vi
      .spyOn(timeseriesApi, 'transformTimeSeries')
      .mockResolvedValue({
        points: [{ time: '2026-05-08T00:01:00Z', value: 2 }],
      });

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    await waitFor(() => {
      expect(
        screen.getByTestId('ts-tab-status-temperature').textContent,
      ).toBe('ready');
    });

    fireEvent.change(screen.getByTestId('ts-transform-op-temperature'), {
      target: { value: 'diff' },
    });
    fireEvent.click(screen.getByTestId('ts-transform-apply-temperature'));

    await waitFor(() => {
      expect(transformSpy).toHaveBeenCalledTimes(1);
    });
    expect(transformSpy).toHaveBeenCalledWith(
      'default',
      expect.objectContaining({
        source: {
          objectType: 'Sensor',
          primaryKey: '42',
          property: 'temperature',
        },
        transforms: [{ op: 'diff' }],
      }),
    );
  });

  it('clears the transform and returns to the raw series', async () => {
    const objectType = makeObjectType({ temperature: { type: 'timeseries' } });

    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([
      { time: '2026-05-08T00:00:00Z', value: 21.5 },
    ]);
    vi.spyOn(timeseriesApi, 'transformTimeSeries').mockResolvedValue({
      points: [{ time: '2026-05-08T00:00:00Z', value: 99 }],
    });

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    await waitFor(() => {
      expect(
        screen.getByTestId('ts-tab-status-temperature').textContent,
      ).toBe('ready');
    });

    fireEvent.change(screen.getByTestId('ts-transform-op-temperature'), {
      target: { value: 'diff' },
    });
    fireEvent.click(screen.getByTestId('ts-transform-apply-temperature'));

    await waitFor(() => {
      expect(
        screen.getByTestId('ts-transform-active-temperature'),
      ).toBeTruthy();
    });

    fireEvent.click(screen.getByTestId('ts-transform-clear-temperature'));

    await waitFor(() => {
      expect(
        screen.queryByTestId('ts-transform-active-temperature'),
      ).toBeNull();
    });
  });
});
