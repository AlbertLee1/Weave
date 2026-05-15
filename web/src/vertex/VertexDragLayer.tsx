// VTX-024 — Vertex node-drag layer.
//
// Mounted as a child of <SigmaContainer> so useSigma() resolves. The
// component subscribes to Sigma's downNode / mousemovebody / mouseup
// events; while a drag is in progress it rewrites the dragged node's
// x/y attributes on the graphology Graph and asks Sigma to repaint. On
// mouseup it calls `onDragEnd(nodeId, x, y)` so the page can persist
// the new coords via PATCH /layout and add the node to its pinned set.
//
// The pure helpers (formatLayoutPatchBody) and page-level mutation logic
// live elsewhere — this component is just the Sigma event ↔ React state
// bridge.

import { useEffect, useRef } from 'react';
import { useRegisterEvents, useSigma } from '@react-sigma/core';

export interface VertexDragLayerProps {
  /** Called once per completed drag with the final graph-space coords. */
  onDragEnd: (nodeId: string, x: number, y: number) => void;
}

interface SigmaNodeDownPayload {
  node: string;
  event: { original: MouseEvent };
}

interface SigmaMouseCoordsPayload {
  x: number;
  y: number;
  original: MouseEvent;
}

interface DragState {
  nodeId: string;
  /** Final graph-space coords the last mousemove computed — what mouseup commits. */
  x: number;
  y: number;
}

export function VertexDragLayer({ onDragEnd }: VertexDragLayerProps) {
  const sigma = useSigma();
  const registerEvents = useRegisterEvents();

  const dragRef = useRef<DragState | null>(null);
  const onDragEndRef = useRef(onDragEnd);
  useEffect(() => {
    onDragEndRef.current = onDragEnd;
  }, [onDragEnd]);

  useEffect(() => {
    registerEvents({
      downNode: (payload: SigmaNodeDownPayload) => {
        const id = payload.node;
        const graph = sigma.getGraph();
        if (!graph || !graph.hasNode(id)) return;
        const x = graph.getNodeAttribute(id, 'x') as unknown;
        const y = graph.getNodeAttribute(id, 'y') as unknown;
        dragRef.current = {
          nodeId: id,
          x: typeof x === 'number' ? x : 0,
          y: typeof y === 'number' ? y : 0,
        };
      },
      mousemovebody: (payload: SigmaMouseCoordsPayload) => {
        const drag = dragRef.current;
        if (!drag) return;
        const vp = pointFromMouseEvent(sigma, payload.original);
        const gp = sigma.viewportToGraph(vp);
        if (!Number.isFinite(gp.x) || !Number.isFinite(gp.y)) return;
        const graph = sigma.getGraph();
        if (!graph || !graph.hasNode(drag.nodeId)) return;
        graph.setNodeAttribute(drag.nodeId, 'x', gp.x);
        graph.setNodeAttribute(drag.nodeId, 'y', gp.y);
        dragRef.current = { nodeId: drag.nodeId, x: gp.x, y: gp.y };
        if (typeof sigma.refresh === 'function') sigma.refresh();
        // Suppress camera panning while a node is under the cursor.
        if (payload.original && typeof payload.original.preventDefault === 'function') {
          payload.original.preventDefault();
        }
      },
      mouseup: (payload: SigmaMouseCoordsPayload) => {
        const drag = dragRef.current;
        if (!drag) return;
        let { x, y } = drag;
        if (payload && payload.original) {
          const vp = pointFromMouseEvent(sigma, payload.original);
          const gp = sigma.viewportToGraph(vp);
          if (Number.isFinite(gp.x) && Number.isFinite(gp.y)) {
            x = gp.x;
            y = gp.y;
          }
        }
        const id = drag.nodeId;
        dragRef.current = null;
        onDragEndRef.current(id, x, y);
      },
    });
  }, [registerEvents, sigma]);

  return null;
}

function pointFromMouseEvent(
  sigma: ReturnType<typeof useSigma>,
  e: MouseEvent,
): { x: number; y: number } {
  const container = sigma.getContainer();
  if (!container) return { x: e.clientX, y: e.clientY };
  const rect = container.getBoundingClientRect();
  return { x: e.clientX - rect.left, y: e.clientY - rect.top };
}
