// VTX-019: DOM overlay for Vertex node Extended Labels.
//
// Mounts inside <SigmaContainer> (so useSigma() resolves) and renders
// absolute-positioned cards above each node carrying the extended labels
// the projection in features/vertex/render/extendedLabels.ts produced.
//
// Camera tracking: subscribe to Sigma's `afterRender` event and bump a
// local tick so the overlay re-projects each node's graph coords to
// viewport pixels on every render frame. Sigma already throttles
// afterRender to the actual paint cycle, so this stays bounded.
//
// Virtualization: nodes whose viewport position falls outside the
// canvas dimensions are skipped — the overlay never plants DOM for an
// off-screen node, keeping the React tree size bounded even on a 5000-
// node graph where most labels are clipped.
//
// VTX-019 introduced the infrastructure; SELF-466 wires time-series
// labels to the same mini-sparkline behavior used by the sidebar.

import { useEffect, useMemo, useState } from 'react';
import { useSigma, useRegisterEvents } from '@react-sigma/core';

import type { ExtendedLabel } from '../features/vertex/render/extendedLabels';
import { useTimeSeriesPoints } from '../hooks/useTimeSeries';
import type { VertexObjectSummary } from './VertexSelectionSidebar';
import { VertexMiniSparkline } from './VertexMiniSparkline';

export interface VertexNodeOverlayProps {
  /** Map keyed by objectRid → labels emitted by extractExtendedLabels. */
  labelsByRid: Map<string, ExtendedLabel[]>;
  /** Map keyed by objectRid → metadata needed for OSS object-scoped calls. */
  objectsByRid: ReadonlyMap<string, VertexObjectSummary>;
}

interface CardPos {
  rid: string;
  labels: ExtendedLabel[];
  object: VertexObjectSummary | null;
  x: number;
  y: number;
}

export function VertexNodeOverlay({ labelsByRid, objectsByRid }: VertexNodeOverlayProps) {
  const sigma = useSigma();
  const registerEvents = useRegisterEvents();
  // tick is bumped by Sigma's afterRender — the body reads `sigma` /
  // `labelsByRid` synchronously so we don't need to stash the cards in
  // state; a render trigger is enough.
  const [, setTick] = useState(0);

  useEffect(() => {
    registerEvents({
      afterRender: () => setTick((t) => (t + 1) | 0),
    });
  }, [registerEvents]);

  const dims = sigma.getDimensions();
  const graph = sigma.getGraph();

  const cards: CardPos[] = [];
  for (const [rid, labels] of labelsByRid) {
    if (!graph.hasNode(rid)) continue;
    const gx = graph.getNodeAttribute(rid, 'x') as unknown;
    const gy = graph.getNodeAttribute(rid, 'y') as unknown;
    if (typeof gx !== 'number' || typeof gy !== 'number') continue;
    if (!Number.isFinite(gx) || !Number.isFinite(gy)) continue;
    const vp = sigma.graphToViewport({ x: gx, y: gy });
    if (!Number.isFinite(vp.x) || !Number.isFinite(vp.y)) continue;
    // Virtualization: skip cards whose anchor pixel is outside the
    // canvas. The card extends *upwards* from the anchor (CSS translate
    // -100%), so a 0..height inclusive check matches what's visible.
    if (vp.x < 0 || vp.x > dims.width) continue;
    if (vp.y < 0 || vp.y > dims.height) continue;
    cards.push({
      rid,
      labels,
      object: objectsByRid.get(rid) ?? null,
      x: vp.x,
      y: vp.y,
    });
  }

  return (
    <div
      data-testid="vertex-node-overlay-root"
      className="pointer-events-none absolute inset-0 overflow-hidden"
      style={{ position: 'absolute', inset: 0 }}
    >
      {cards.map((c) => (
        <ExtendedLabelCard key={c.rid} card={c} />
      ))}
    </div>
  );
}

function ExtendedLabelCard({ card }: { card: CardPos }) {
  return (
    <div
      data-testid={`vertex-node-overlay-card-${card.rid}`}
      className="pointer-events-auto absolute"
      style={{
        position: 'absolute',
        left: `${card.x}px`,
        top: `${card.y}px`,
        transform: 'translate(-50%, calc(-100% - 14px))',
      }}
    >
      <div className="min-w-[120px] rounded border border-zinc-700 bg-zinc-900/95 px-2 py-1 text-[10px] text-zinc-100 shadow">
        {card.labels.map((l) => (
          <ExtendedLabelRow key={l.key} label={l} object={card.object} />
        ))}
      </div>
    </div>
  );
}

function ExtendedLabelRow({
  label,
  object,
}: {
  label: ExtendedLabel;
  object: VertexObjectSummary | null;
}) {
  return (
    <div
      data-testid={`vertex-extended-label-${label.kind}`}
      className="flex items-baseline justify-between gap-2"
    >
      <span className="text-zinc-400">{label.label}</span>
      {label.kind === 'property' && (
        <span className="font-mono text-zinc-100">{label.value ?? '—'}</span>
      )}
      {label.kind === 'timeSeries' && (
        <OverlayTimeSeriesSparkline label={label} object={object} />
      )}
      {label.kind === 'measure' && (
        <span className="font-mono text-zinc-500" aria-label="measure pending">
          …
        </span>
      )}
    </div>
  );
}

function propertyFromTimeSeriesLabel(label: ExtendedLabel): string {
  const [, property] = label.key.split(':');
  return property && property.length > 0 ? property : label.label;
}

function OverlayTimeSeriesSparkline({
  label,
  object,
}: {
  label: ExtendedLabel;
  object: VertexObjectSummary | null;
}) {
  const property = propertyFromTimeSeriesLabel(label);
  const ontologyApiName = object?.ontologyApiName ?? '';
  const objectType = object?.objectType ?? '';
  const primaryKey = object?.primaryKey ?? '';
  const enabled = Boolean(ontologyApiName && objectType && primaryKey && property);
  const query = useTimeSeriesPoints({
    ontologyApiName,
    objectType,
    primaryKey,
    property,
    enabled,
  });
  const values = useMemo<number[]>(() => {
    const data = query.data;
    if (!Array.isArray(data)) return [];
    const out: number[] = [];
    for (const pt of data) {
      const v = (pt as { value: unknown }).value;
      if (typeof v === 'number' && Number.isFinite(v)) out.push(v);
    }
    return out;
  }, [query.data]);

  if (values.length >= 2) {
    return (
      <VertexMiniSparkline
        values={values}
        testId="vertex-node-overlay-series-sparkline"
        className="h-4 w-16"
      />
    );
  }

  return (
    <span
      data-testid="vertex-node-overlay-series-sparkline"
      className="font-mono text-zinc-500"
      aria-label="sparkline pending"
    >
      ▁▂▃▄▅
    </span>
  );
}
