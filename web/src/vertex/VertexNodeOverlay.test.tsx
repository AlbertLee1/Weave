import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';

import type { ExtendedLabel } from '../features/vertex/render/extendedLabels';
import type { VertexObjectSummary } from './VertexSelectionSidebar';

type MockTimeSeriesPoint = { time: string; value: unknown };

const timeSeriesHook = vi.hoisted(() => ({
  useTimeSeriesPoints: vi.fn<() => { data: MockTimeSeriesPoint[] }>(() => ({ data: [] })),
}));

// Module-scoped stubs the test cases mutate per case. The mocked
// @react-sigma/core exports return references to these stubs so each test
// can shape the Sigma surface the overlay sees (positions, viewport
// dimensions, hasNode) without booting WebGL.
type GraphStub = {
  nodes: Record<string, { x: number; y: number }>;
};
type SigmaStub = {
  graphToViewport: (p: { x: number; y: number }) => { x: number; y: number };
  getDimensions: () => { width: number; height: number };
  getGraph: () => {
    hasNode: (id: string) => boolean;
    getNodeAttribute: (id: string, key: string) => unknown;
  };
};

let sigmaStub: SigmaStub;
let lastHandlers: Record<string, (() => void) | undefined> | null;

vi.mock('@react-sigma/core', () => ({
  useSigma: () => sigmaStub,
  useRegisterEvents: () => (handlers: Record<string, () => void>) => {
    lastHandlers = handlers;
  },
}));

vi.mock('../hooks/useTimeSeries', () => ({
  useTimeSeriesPoints: timeSeriesHook.useTimeSeriesPoints,
}));

import { VertexNodeOverlay } from './VertexNodeOverlay';

function buildSigmaStub(opts: {
  positions: Record<string, { x: number; y: number }>;
  dimensions?: { width: number; height: number };
  graphToViewport?: (p: { x: number; y: number }) => { x: number; y: number };
}): { graph: GraphStub; sigma: SigmaStub } {
  const graph: GraphStub = { nodes: opts.positions };
  const sigma: SigmaStub = {
    graphToViewport:
      opts.graphToViewport ?? ((p) => ({ x: p.x, y: p.y })),
    getDimensions: () => opts.dimensions ?? { width: 800, height: 600 },
    getGraph: () => ({
      hasNode: (id: string) => Object.prototype.hasOwnProperty.call(graph.nodes, id),
      getNodeAttribute: (id: string, key: string) => {
        const node = graph.nodes[id];
        if (!node) return undefined;
        if (key === 'x') return node.x;
        if (key === 'y') return node.y;
        return undefined;
      },
    }),
  };
  return { graph, sigma };
}

beforeEach(() => {
  lastHandlers = null;
  timeSeriesHook.useTimeSeriesPoints.mockReset();
  timeSeriesHook.useTimeSeriesPoints.mockReturnValue({ data: [] });
});

const threeLabels: ExtendedLabel[] = [
  { key: 'property:onTimePct:0', kind: 'property', label: 'On-time %', value: '92' },
  { key: 'timeSeries:temp:1', kind: 'timeSeries', label: 'Temp °C' },
  { key: 'measure:ri.fn.avgDelay:2', kind: 'measure', label: 'Avg Delay' },
];

const jfkSummary: VertexObjectSummary = {
  rid: 'ri.airport.JFK',
  label: 'JFK',
  properties: {},
  ontologyApiName: 'main',
  objectType: 'Airport',
  primaryKey: 'JFK',
};

describe('VTX-019 VertexNodeOverlay', () => {
  it('Given_oneNodeWith3Labels_When_overlayRenders_Then_emitsCardWith3LabelRows', () => {
    const { sigma } = buildSigmaStub({
      positions: { 'ri.airport.JFK': { x: 100, y: 200 } },
    });
    sigmaStub = sigma;
    const labelsByRid = new Map<string, ExtendedLabel[]>([
      ['ri.airport.JFK', threeLabels],
    ]);

    render(<VertexNodeOverlay labelsByRid={labelsByRid} objectsByRid={new Map()} />);

    const card = screen.getByTestId('vertex-node-overlay-card-ri.airport.JFK');
    expect(card).toBeInTheDocument();
    // 3 label rows must be present, one per kind.
    expect(card.querySelectorAll('[data-testid^="vertex-extended-label-"]')).toHaveLength(3);
    expect(screen.getByTestId('vertex-extended-label-property')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-extended-label-timeSeries')).toBeInTheDocument();
    expect(screen.getByTestId('vertex-extended-label-measure')).toBeInTheDocument();
    // Card must be absolutely positioned so the overlay follows the
    // camera transform layer instead of flowing under the canvas.
    expect(card.style.position).toBe('absolute');
    // Initial paint plants the card at the viewport coords returned by
    // the (identity) graphToViewport stub (left = 100, top = 200).
    expect(card.style.left).toBe('100px');
    expect(card.style.top).toBe('200px');
  });

  it('Given_cameraMoves_When_afterRenderFires_Then_overlayRepositions', () => {
    const { graph, sigma } = buildSigmaStub({
      positions: { 'ri.airport.JFK': { x: 50, y: 50 } },
      graphToViewport: (p) => ({ x: p.x + 0, y: p.y + 0 }),
    });
    sigmaStub = sigma;

    const labelsByRid = new Map<string, ExtendedLabel[]>([
      ['ri.airport.JFK', threeLabels],
    ]);
    render(<VertexNodeOverlay labelsByRid={labelsByRid} objectsByRid={new Map()} />);

    // Sanity check: registered afterRender handler is captured.
    expect(lastHandlers).not.toBeNull();
    expect(typeof lastHandlers?.afterRender).toBe('function');

    // Simulate a camera pan: the *graph* position is unchanged but the
    // graphToViewport projection now adds a 200/300 offset (panning the
    // viewport puts the node at a new screen pixel).
    graph.nodes['ri.airport.JFK'] = { x: 50, y: 50 };
    sigmaStub = {
      ...sigma,
      graphToViewport: (p) => ({ x: p.x + 200, y: p.y + 300 }),
    };

    act(() => {
      lastHandlers!.afterRender!();
    });

    const card = screen.getByTestId('vertex-node-overlay-card-ri.airport.JFK');
    expect(card.style.left).toBe('250px');
    expect(card.style.top).toBe('350px');
  });

  it('Given_nodeOffscreen_When_overlayRenders_Then_cardIsHidden', () => {
    const { sigma } = buildSigmaStub({
      positions: {
        'ri.airport.onscreen': { x: 100, y: 100 },
        'ri.airport.offscreen': { x: 9999, y: 9999 },
      },
      dimensions: { width: 800, height: 600 },
    });
    sigmaStub = sigma;

    const labelsByRid = new Map<string, ExtendedLabel[]>([
      ['ri.airport.onscreen', threeLabels],
      ['ri.airport.offscreen', threeLabels],
    ]);
    render(<VertexNodeOverlay labelsByRid={labelsByRid} objectsByRid={new Map()} />);

    expect(screen.queryByTestId('vertex-node-overlay-card-ri.airport.onscreen')).toBeInTheDocument();
    expect(screen.queryByTestId('vertex-node-overlay-card-ri.airport.offscreen')).not.toBeInTheDocument();
  });

  it('Given_emptyLabelsMap_When_render_Then_overlayRootRendersWithoutCards', () => {
    const { sigma } = buildSigmaStub({ positions: {} });
    sigmaStub = sigma;
    render(<VertexNodeOverlay labelsByRid={new Map()} objectsByRid={new Map()} />);
    expect(screen.getByTestId('vertex-node-overlay-root')).toBeInTheDocument();
    expect(
      screen.getByTestId('vertex-node-overlay-root').querySelectorAll(
        '[data-testid^="vertex-node-overlay-card-"]',
      ),
    ).toHaveLength(0);
  });

  it('Given_labelsForUnknownNode_When_render_Then_skipsThatRidWithoutThrowing', () => {
    const { sigma } = buildSigmaStub({
      positions: { 'ri.known': { x: 0, y: 0 } },
    });
    sigmaStub = sigma;

    const labelsByRid = new Map<string, ExtendedLabel[]>([
      ['ri.known', threeLabels],
      ['ri.missing', threeLabels],
    ]);
    render(<VertexNodeOverlay labelsByRid={labelsByRid} objectsByRid={new Map()} />);

    expect(screen.getByTestId('vertex-node-overlay-card-ri.known')).toBeInTheDocument();
    expect(screen.queryByTestId('vertex-node-overlay-card-ri.missing')).not.toBeInTheDocument();
  });

  it('Given_visibleNodeWithTimeSeriesLabelAndMetadata_When_overlayRenders_Then_requestsStreamPointsAndDrawsSparkline', () => {
    timeSeriesHook.useTimeSeriesPoints.mockReturnValue({
      data: [
        { time: '2026-05-19T00:00:00Z', value: 10 },
        { time: '2026-05-19T00:01:00Z', value: 15 },
        { time: '2026-05-19T00:02:00Z', value: 20 },
      ],
    });
    const { sigma } = buildSigmaStub({
      positions: { 'ri.airport.JFK': { x: 100, y: 200 } },
    });
    sigmaStub = sigma;
    const labelsByRid = new Map<string, ExtendedLabel[]>([
      ['ri.airport.JFK', [{ key: 'timeSeries:temp:0', kind: 'timeSeries', label: 'Temp °C' }]],
    ]);
    const objectsByRid = new Map<string, VertexObjectSummary>([
      ['ri.airport.JFK', jfkSummary],
    ]);

    render(<VertexNodeOverlay labelsByRid={labelsByRid} objectsByRid={objectsByRid} />);

    expect(timeSeriesHook.useTimeSeriesPoints).toHaveBeenCalledWith(
      expect.objectContaining({
        ontologyApiName: 'main',
        objectType: 'Airport',
        primaryKey: 'JFK',
        property: 'temp',
      }),
    );
    const sparkline = screen.getByTestId('vertex-node-overlay-series-sparkline');
    expect(sparkline.tagName.toLowerCase()).toBe('svg');
    expect(sparkline.querySelector('path')?.getAttribute('d')).toBe(
      'M 2.00 22.00 L 60.00 12.00 L 118.00 2.00',
    );
  });

  it('Given_missingOrNonNumericStreamPoints_When_overlayRenders_Then_keepsPendingMarkerWithoutThrowing', () => {
    timeSeriesHook.useTimeSeriesPoints.mockReturnValue({
      data: [
        { time: '2026-05-19T00:00:00Z', value: 'cold' },
        { time: '2026-05-19T00:01:00Z', value: null },
      ],
    });
    const { sigma } = buildSigmaStub({
      positions: { 'ri.airport.JFK': { x: 100, y: 200 } },
    });
    sigmaStub = sigma;
    const labelsByRid = new Map<string, ExtendedLabel[]>([
      ['ri.airport.JFK', [{ key: 'timeSeries:temp:0', kind: 'timeSeries', label: 'Temp °C' }]],
    ]);

    render(
      <VertexNodeOverlay
        labelsByRid={labelsByRid}
        objectsByRid={new Map([['ri.airport.JFK', jfkSummary]])}
      />,
    );

    const marker = screen.getByTestId('vertex-node-overlay-series-sparkline');
    expect(marker.tagName.toLowerCase()).toBe('span');
    expect(marker).toHaveAttribute('aria-label', 'sparkline pending');
    expect(marker.textContent).toBe('▁▂▃▄▅');
  });

  it('Given_onlyOneOfManyTimeSeriesNodesIsVisible_When_overlayRenders_Then_requestsOnlyRenderedCards', () => {
    const { sigma } = buildSigmaStub({
      positions: {
        'ri.airport.visible': { x: 100, y: 200 },
        'ri.airport.offscreen': { x: 9999, y: 9999 },
      },
      dimensions: { width: 800, height: 600 },
    });
    sigmaStub = sigma;
    const timeSeriesLabel: ExtendedLabel = {
      key: 'timeSeries:temp:0',
      kind: 'timeSeries',
      label: 'Temp °C',
    };
    const labelsByRid = new Map<string, ExtendedLabel[]>([
      ['ri.airport.visible', [timeSeriesLabel]],
      ['ri.airport.offscreen', [timeSeriesLabel]],
    ]);
    const objectsByRid = new Map<string, VertexObjectSummary>([
      ['ri.airport.visible', { ...jfkSummary, rid: 'ri.airport.visible', primaryKey: 'visible' }],
      ['ri.airport.offscreen', { ...jfkSummary, rid: 'ri.airport.offscreen', primaryKey: 'offscreen' }],
    ]);

    render(<VertexNodeOverlay labelsByRid={labelsByRid} objectsByRid={objectsByRid} />);

    expect(screen.getByTestId('vertex-node-overlay-card-ri.airport.visible')).toBeInTheDocument();
    expect(screen.queryByTestId('vertex-node-overlay-card-ri.airport.offscreen')).not.toBeInTheDocument();
    expect(timeSeriesHook.useTimeSeriesPoints).toHaveBeenCalledTimes(1);
    expect(timeSeriesHook.useTimeSeriesPoints).toHaveBeenCalledWith(
      expect.objectContaining({ primaryKey: 'visible' }),
    );
  });
});
