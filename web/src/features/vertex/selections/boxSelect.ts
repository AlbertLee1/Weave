// VTX-020: pure box-select math for shift-drag region selection on the
// Vertex canvas.
//
// `rectFromCorners` normalises a drag's start + end (which may go any
// direction) into a canonical {x, y, width, height} viewport rect.
//
// `nodesInRect` walks a viewport-coords position map and returns the rids
// whose anchor pixel falls inside (inclusive of the edges). Both helpers
// stay pure JS so they can be Vitest-tested in isolation; the React
// layer plugs them into Sigma's `sigma.graphToViewport` projection.

export interface ViewportPoint {
  x: number;
  y: number;
}

export interface ViewportRect {
  /** Top-left X in viewport pixels. */
  x: number;
  /** Top-left Y in viewport pixels. */
  y: number;
  /** Box width in viewport pixels (always ≥ 0). */
  width: number;
  /** Box height in viewport pixels (always ≥ 0). */
  height: number;
}

export function rectFromCorners(a: ViewportPoint, b: ViewportPoint): ViewportRect {
  const x = Math.min(a.x, b.x);
  const y = Math.min(a.y, b.y);
  const width = Math.abs(b.x - a.x);
  const height = Math.abs(b.y - a.y);
  return { x, y, width, height };
}

export function nodesInRect(
  rect: ViewportRect,
  positions: ReadonlyMap<string, ViewportPoint>,
): string[] {
  if (rect.width === 0 || rect.height === 0) return [];
  const minX = rect.x;
  const maxX = rect.x + rect.width;
  const minY = rect.y;
  const maxY = rect.y + rect.height;
  const out: string[] = [];
  for (const [rid, p] of positions) {
    if (p.x < minX || p.x > maxX) continue;
    if (p.y < minY || p.y > maxY) continue;
    out.push(rid);
  }
  return out;
}
