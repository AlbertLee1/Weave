import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { QuiverTransformPanel } from '../QuiverTransformPanel';
import type { SeriesSpec } from '../QuiverWorkbenchView';

// BDD — resample agg dropdown must only offer backend-supported aggregations.
//
// The backend (pkg/timeseries/transform.go) accepts exactly
// avg|mean|sum|min|max|count for a `resample` transform and returns a 4xx
// for anything else. Previously the panel surfaced `first`/`last`, which
// the backend rejects, so selecting either produced a guaranteed failed
// POST. This contract pins the dropdown to the supported set.
//
// Scenario: Given the resample transform panel, Then the agg dropdown only
// offers avg/sum/min/max/count and never the unsupported first/last.

const SERIES: SeriesSpec[] = [
  {
    id: 'Host/h1.cpu',
    label: 'host cpu',
    ontologyApiName: 'demo',
    objectType: 'Host',
    primaryKey: 'h1',
    property: 'cpu',
    color: '#00ffff',
  },
];

function renderPanel() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <QuiverTransformPanel ontologyApiName="demo" seriesList={SERIES} />
    </QueryClientProvider>,
  );
}

// Backend-supported resample aggregations (transform.go switch accepts
// avg|mean|sum|min|max|count; the panel exposes the canonical avg alias).
const SUPPORTED_AGGS = ['avg', 'sum', 'min', 'max', 'count'];
const UNSUPPORTED_AGGS = ['first', 'last'];

describe('BDD — QuiverTransformPanel resample agg options', () => {
  it('Given the resample panel, Then the agg dropdown offers only backend-supported aggregations', () => {
    renderPanel();

    const panel = screen.getByTestId('quiver-transform-panel');

    // Op defaults to `resample`, so the agg select is present.
    const aggSelect = within(panel).getByTestId(
      'transform-agg-select',
    ) as HTMLSelectElement;

    const optionValues = Array.from(aggSelect.options).map((o) => o.value);

    // Then: exactly the backend-supported aggregations are offered, in order.
    expect(optionValues).toEqual(SUPPORTED_AGGS);
  });

  it('Given the resample panel, Then the agg dropdown never offers the unsupported first/last', () => {
    renderPanel();

    const aggSelect = within(
      screen.getByTestId('quiver-transform-panel'),
    ).getByTestId('transform-agg-select') as HTMLSelectElement;

    const optionValues = Array.from(aggSelect.options).map((o) => o.value);

    for (const bad of UNSUPPORTED_AGGS) {
      expect(optionValues).not.toContain(bad);
    }
  });
});
