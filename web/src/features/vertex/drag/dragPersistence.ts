// VTX-024 — drag-persistence helpers used by VertexWorkspacePage. Pure JS
// (no React, no Sigma) so the math + payload-shaping is Vitest-friendly and
// reusable from Playwright fixtures.
//
// Two pieces:
//   - pinnedPositionsFromPayload: extract the user-pinned subset of
//     payload.positions so the page can seed `pinnedPositions` state on
//     first load. Used by the layout helpers (via the pinnedPositions
//     option) and by the right-click "Unpin" menu (to know what to show).
//   - formatLayoutPatchBody: shape the request body for PATCH
//     /api/vertex/v1/graphs/{rid}/layout — one node, x/y coords, pinned
//     boolean. The backend merges into existing payload.positions by key,
//     so callers may send a single entry without clobbering siblings.

export interface LayoutPoint {
  x: number;
  y: number;
}

interface PositionLike {
  x?: unknown;
  y?: unknown;
  pinned?: unknown;
}

interface PayloadLike {
  positions?: Record<string, PositionLike>;
}

/**
 * Extract the user-pinned subset of `payload.positions`. Entries are kept
 * only when:
 *   - the value is an object with numeric, finite `x` + `y`, AND
 *   - `pinned === true`.
 *
 * Missing or non-object payloads / positions yield an empty map.
 */
export function pinnedPositionsFromPayload(
  payload: unknown,
): Map<string, LayoutPoint> {
  const out = new Map<string, LayoutPoint>();
  if (!payload || typeof payload !== 'object') return out;
  const positions = (payload as PayloadLike).positions;
  if (!positions || typeof positions !== 'object') return out;
  for (const [id, raw] of Object.entries(positions)) {
    if (!raw || typeof raw !== 'object') continue;
    if ((raw as PositionLike).pinned !== true) continue;
    const x = (raw as PositionLike).x;
    const y = (raw as PositionLike).y;
    if (typeof x !== 'number' || typeof y !== 'number') continue;
    if (!Number.isFinite(x) || !Number.isFinite(y)) continue;
    out.set(id, { x, y });
  }
  return out;
}

export interface LayoutPatchBody {
  positions: Record<string, { x: number; y: number; pinned: boolean }>;
}

/**
 * Shape the PATCH /layout request body for a single node update. The
 * backend merges by key into payload.positions, so callers can send one
 * entry per drag / unpin without disturbing siblings.
 */
export function formatLayoutPatchBody(
  nodeId: string,
  x: number,
  y: number,
  pinned: boolean,
): LayoutPatchBody {
  return { positions: { [nodeId]: { x, y, pinned } } };
}
