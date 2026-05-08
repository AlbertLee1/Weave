// US-457: tests for the per-object TimeSeries tab. We mock the
// streamTimeSeriesPoints API directly so the hook's underlying fetch
// returns deterministic points; the chart itself is rendered through
// MultiSeriesChart but uplot's canvas init is no-op'd by our test
// setup (matchMedia polyfill + jsdom canvas guard inside the chart).

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';
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
        {
          name,
          dataType: dt,
        },
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

describe('ObjectTimeSeriesTab', () => {
  it('shows an empty-state when the object type has no timeseries properties', () => {
    const objectType = makeObjectType({
      name: { type: 'string' },
      count: { type: 'integer' },
    });

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    expect(screen.getByTestId('ts-tab-empty').textContent).toContain('no timeseries');
  });

  it('renders one section per timeseries property and labels them', () => {
    const objectType = makeObjectType({
      temperature: { type: 'timeseries' },
      pressure: { type: 'timeseries' },
      label: { type: 'string' },
    });

    const spy = vi
      .spyOn(timeseriesApi, 'streamTimeSeriesPoints')
      .mockResolvedValue([
        { time: '2026-05-08T00:00:00Z', value: 21.5 },
        { time: '2026-05-08T00:01:00Z', value: 22.1 },
      ]);

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    // Both properties render section headers immediately, even before data
    // resolves.
    expect(screen.getByTestId('ts-tab-property-temperature')).toBeTruthy();
    expect(screen.getByTestId('ts-tab-property-pressure')).toBeTruthy();
    expect(spy).toHaveBeenCalledTimes(2);
    expect(spy).toHaveBeenCalledWith(
      expect.objectContaining({
        ontologyApiName: 'default',
        objectType: 'Sensor',
        primaryKey: '42',
        property: 'temperature',
      }),
    );
  });

  it('flips the property status to ready once the fetch resolves with points', async () => {
    const objectType = makeObjectType({
      temperature: { type: 'timeseries' },
    });

    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([
      { time: '2026-05-08T00:00:00Z', value: 21.5 },
    ]);

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    await waitFor(() => {
      expect(screen.getByTestId('ts-tab-status-temperature').textContent).toBe('ready');
    });
  });

  it('shows a no-points message when the fetch resolves with an empty list', async () => {
    const objectType = makeObjectType({
      temperature: { type: 'timeseries' },
    });

    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    await waitFor(() => {
      expect(screen.getByTestId('ts-tab-nopoints-temperature')).toBeTruthy();
    });
  });

  it('surfaces fetch errors in the per-property panel', async () => {
    const objectType = makeObjectType({
      temperature: { type: 'timeseries' },
    });

    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockRejectedValue(
      new Error('500 Internal Server Error'),
    );

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    await waitFor(() => {
      expect(screen.getByTestId('ts-tab-error-temperature').textContent).toContain('Failed');
    });
  });

  it('also matches array-of-timeseries via baseTypeOf', () => {
    const objectType = makeObjectType({
      readings: { type: 'array' } as { type: string },
    });
    // Inject an array-typed property whose itemType is a timeseries; the
    // baseTypeOf utility unwraps array → itemType.type so this should still
    // surface as a timeseries property.
    (objectType.properties!.readings as unknown as { dataType: { type: string; itemType: { type: string } } }).dataType = {
      type: 'array',
      itemType: { type: 'timeseries' },
    };

    vi.spyOn(timeseriesApi, 'streamTimeSeriesPoints').mockResolvedValue([]);

    renderWithClient(
      <ObjectTimeSeriesTab
        ontologyApiName="default"
        objectType={objectType}
        primaryKey="42"
      />,
    );

    expect(screen.getByTestId('ts-tab-property-readings')).toBeTruthy();
  });
});
