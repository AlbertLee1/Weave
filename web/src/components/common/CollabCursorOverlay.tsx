import { useContext, useEffect, useState } from 'react';
import { CollabPresenceContext } from '../../lib/collabPresenceContext';
import type { PresencePeer } from '../../lib/collabPresence';

interface ProjectedCursor {
  clientID: number;
  name: string;
  color: string;
  /** absolute top within the surface element */
  top: number;
  /** absolute left within the surface element */
  left: number;
  /** width of the highlighted selection range, 0 for a collapsed caret */
  width: number;
  /** height of the field row */
  height: number;
  hasSelection: boolean;
}

const POLL_INTERVAL_MS = 750;

/**
 * Compute the surface-relative position of a caret/selection inside a field
 * marked with `data-collab-field`. Returns null if the field can't be located
 * inside the supplied surface (e.g. unmounted or in a different scroll
 * container).
 */
function projectCursor(
  surface: HTMLElement | null,
  peer: PresencePeer,
): ProjectedCursor | null {
  if (!surface) return null;
  const cursor = peer.cursor;
  if (!cursor || !cursor.field) return null;
  const field = surface.querySelector<HTMLElement>(
    `[data-collab-field="${cssEscape(cursor.field)}"]`,
  );
  if (!field) return null;
  const surfaceRect = surface.getBoundingClientRect();
  const fieldRect = field.getBoundingClientRect();
  const baseTop = fieldRect.top - surfaceRect.top + (surface.scrollTop ?? 0);
  const baseLeft = fieldRect.left - surfaceRect.left + (surface.scrollLeft ?? 0);
  const fieldWidth = fieldRect.width;
  // We can't measure exact glyph offset without a canvas; approximate caret
  // x via fraction of selectionStart over the field's text length. Empty
  // fields anchor on the left edge.
  const value = readFieldValue(field);
  const len = value.length;
  let leftFrac = 0;
  let widthFrac = 0;
  if (len > 0) {
    const start = clamp(cursor.selectionStart, 0, len);
    const end = clamp(cursor.selectionEnd, 0, len);
    leftFrac = start / len;
    widthFrac = Math.max(0, (end - start) / len);
  }
  return {
    clientID: peer.clientID,
    name: peer.user.name,
    color: peer.user.color,
    top: baseTop,
    left: baseLeft + fieldWidth * leftFrac,
    width: fieldWidth * widthFrac,
    height: fieldRect.height,
    hasSelection: cursor.selectionEnd > cursor.selectionStart,
  };
}

function readFieldValue(field: HTMLElement): string {
  if (field instanceof HTMLInputElement || field instanceof HTMLTextAreaElement) {
    return field.value;
  }
  return field.textContent ?? '';
}

function clamp(n: number, lo: number, hi: number): number {
  if (n < lo) return lo;
  if (n > hi) return hi;
  return n;
}

function cssEscape(value: string): string {
  // Escape characters that would break the attribute selector. ASCII-only
  // so jsdom and modern browsers behave identically.
  return value.replace(/(["\\])/g, '\\$1');
}

export interface CollabCursorOverlayProps {
  /**
   * Surface element to project peer cursors over. When omitted the overlay
   * reads the registered surface from `CollabPresenceContext`. The caller's
   * surface needs `position: relative` on its containing block for the
   * overlay to anchor correctly.
   */
  surface?: HTMLElement | null;
}

export function CollabCursorOverlay({ surface }: CollabCursorOverlayProps = {}) {
  const ctx = useContext(CollabPresenceContext);
  const peers = ctx.peers;
  const target = surface ?? ctx.surface;
  const [projected, setProjected] = useState<ProjectedCursor[]>([]);
  // Reset projection synchronously when the target disappears — fixes the
  // "stale peer cursors over a hidden surface" race without breaking the
  // react-hooks/set-state-in-effect rule (the comparison uses a render-phase
  // setState pattern instead of a setState-in-effect).
  const [prevTarget, setPrevTarget] = useState<HTMLElement | null>(target);
  if (prevTarget !== target) {
    setPrevTarget(target);
    if (!target && projected.length > 0) setProjected([]);
  }

  useEffect(() => {
    if (!target) return undefined;
    const recompute = () => {
      const next: ProjectedCursor[] = [];
      for (const peer of peers) {
        const projection = projectCursor(target, peer);
        if (projection) next.push(projection);
      }
      setProjected(next);
    };
    recompute();
    // Re-project periodically to track field resizes that don't emit
    // awareness updates. Peer count is tiny so the DOM walk is cheap.
    const handle = window.setInterval(recompute, POLL_INTERVAL_MS);
    return () => {
      window.clearInterval(handle);
    };
  }, [peers, target]);

  if (projected.length === 0) return null;

  return (
    <div
      data-testid="collab-cursor-overlay"
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 z-30"
    >
      {projected.map((p) => (
        <div
          key={p.clientID}
          data-testid={`collab-cursor-${p.clientID}`}
          data-peer-name={p.name}
          className="absolute"
          style={{
            top: p.top,
            left: p.left,
            height: p.height,
            width: p.hasSelection ? p.width : undefined,
          }}
        >
          <div
            className="h-full"
            style={{
              width: p.hasSelection ? '100%' : 2,
              backgroundColor: p.color,
              opacity: p.hasSelection ? 0.25 : 0.95,
            }}
          />
          <div
            className="absolute -top-4 left-0 px-1 py-px rounded-sm text-[9px] font-mono text-bg-primary whitespace-nowrap"
            style={{ backgroundColor: p.color }}
          >
            {p.name}
          </div>
        </div>
      ))}
    </div>
  );
}
