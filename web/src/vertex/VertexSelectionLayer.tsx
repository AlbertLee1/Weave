// VTX-020: Vertex canvas selection interactions (single click, ctrl/cmd
// multi-select, shift box-select).
//
// Mounted as a child of <SigmaContainer> so useSigma() resolves. The
// component subscribes to Sigma's clickNode / clickStage / downStage /
// mousemovebody / mouseup events; the active selection is hoisted into
// the page via `onSelectionChange` so the right-side sidebar + node
// highlight reducer can read the same state.
//
// Pure helpers (selectionState, boxSelect) hold all the math so this
// component just plumbs events → state changes.

import { useEffect, useRef, useState } from 'react';
import { useSigma, useRegisterEvents } from '@react-sigma/core';
import type { MouseCoords, SigmaEventPayload, SigmaNodeEventPayload } from 'sigma/types';

import {
  selectionClear,
  selectionSingle,
  selectionToggle,
  selectionReplace,
  type SelectionState,
} from '../features/vertex/selections/selectionState';
import { nodesInRect, rectFromCorners, type ViewportPoint, type ViewportRect } from '../features/vertex/selections/boxSelect';

export interface VertexSelectionLayerProps {
  selection: SelectionState;
  onSelectionChange: (next: SelectionState) => void;
}

interface DragState {
  start: ViewportPoint;
  current: ViewportPoint;
}

function isToggleClick(e: MouseEvent | TouchEvent): boolean {
  return e.ctrlKey || e.metaKey;
}

function clientCoordsOf(e: MouseEvent | TouchEvent): { x: number; y: number } {
  if ('clientX' in e) return { x: e.clientX, y: e.clientY };
  const t = e.touches[0] ?? e.changedTouches[0];
  return { x: t?.clientX ?? 0, y: t?.clientY ?? 0 };
}

function pointFromMouseEvent(sigma: ReturnType<typeof useSigma>, e: MouseEvent | TouchEvent): ViewportPoint {
  const { x: clientX, y: clientY } = clientCoordsOf(e);
  const container = sigma.getContainer();
  if (!container) return { x: clientX, y: clientY };
  const rect = container.getBoundingClientRect();
  return { x: clientX - rect.left, y: clientY - rect.top };
}

function viewportPositionsForNodes(sigma: ReturnType<typeof useSigma>): Map<string, ViewportPoint> {
  const graph = sigma.getGraph();
  const positions = new Map<string, ViewportPoint>();
  for (const id of graph.nodes()) {
    const gx = graph.getNodeAttribute(id, 'x') as unknown;
    const gy = graph.getNodeAttribute(id, 'y') as unknown;
    if (typeof gx !== 'number' || typeof gy !== 'number') continue;
    if (!Number.isFinite(gx) || !Number.isFinite(gy)) continue;
    const vp = sigma.graphToViewport({ x: gx, y: gy });
    if (!Number.isFinite(vp.x) || !Number.isFinite(vp.y)) continue;
    positions.set(id, vp);
  }
  return positions;
}

export function VertexSelectionLayer({ selection, onSelectionChange }: VertexSelectionLayerProps) {
  const sigma = useSigma();
  const registerEvents = useRegisterEvents();

  // Drag state lives in both refs (for the event handlers to read the
  // latest value without a re-subscribe per render) AND React state (for
  // the box overlay to repaint as the drag proceeds).
  const dragRef = useRef<DragState | null>(null);
  const [drag, setDrag] = useState<DragState | null>(null);
  // Keep the latest selection in a ref so the event handler closure can
  // read it without re-registering every time the prop changes.
  const selectionRef = useRef(selection);
  useEffect(() => {
    selectionRef.current = selection;
  }, [selection]);

  useEffect(() => {
    registerEvents({
      clickNode: (payload: SigmaNodeEventPayload) => {
        const e = payload.event.original;
        const rid = payload.node;
        if (isToggleClick(e)) {
          onSelectionChange(selectionToggle(selectionRef.current, rid));
          return;
        }
        // Shift-click on a node: extend the selection (don't replace).
        if (e.shiftKey) {
          onSelectionChange(selectionToggle(selectionRef.current, rid));
          return;
        }
        onSelectionChange(selectionSingle(selectionRef.current, rid));
      },
      clickStage: (payload: SigmaEventPayload) => {
        const e = payload.event.original;
        // Shift+stage click is part of the box-select gesture path; let
        // mouseup finalise. A plain (no-modifier) click on empty stage
        // clears the selection.
        if (e.shiftKey) return;
        if (selectionRef.current.size === 0) return;
        onSelectionChange(selectionClear());
      },
      downStage: (payload: SigmaEventPayload) => {
        const e = payload.event.original;
        if (!e.shiftKey) return;
        const p = pointFromMouseEvent(sigma, e);
        const next: DragState = { start: p, current: p };
        dragRef.current = next;
        setDrag(next);
      },
      mousemovebody: (coords: MouseCoords) => {
        if (dragRef.current === null) return;
        const p = pointFromMouseEvent(sigma, coords.original);
        const next: DragState = { start: dragRef.current.start, current: p };
        dragRef.current = next;
        setDrag(next);
      },
      mouseup: (coords: MouseCoords) => {
        if (dragRef.current === null) return;
        const p = pointFromMouseEvent(sigma, coords.original);
        const rect = rectFromCorners(dragRef.current.start, p);
        dragRef.current = null;
        setDrag(null);
        if (rect.width === 0 && rect.height === 0) return;
        const positions = viewportPositionsForNodes(sigma);
        const hits = nodesInRect(rect, positions);
        onSelectionChange(selectionReplace(selectionRef.current, hits));
      },
    });
  }, [registerEvents, sigma, onSelectionChange]);

  if (drag === null) return null;
  const rect: ViewportRect = rectFromCorners(drag.start, drag.current);
  return (
    <div
      data-testid="vertex-box-select-overlay"
      className="pointer-events-none absolute"
      style={{
        position: 'absolute',
        left: `${rect.x}px`,
        top: `${rect.y}px`,
        width: `${rect.width}px`,
        height: `${rect.height}px`,
        border: '1px dashed rgba(59,130,246,0.9)',
        background: 'rgba(59,130,246,0.12)',
      }}
    />
  );
}
