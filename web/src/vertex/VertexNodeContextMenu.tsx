// VTX-026 — node right-click context menu.
//
// Surfaces five items per BDD §2.5: Search Around, Open in Object
// Explorer, Pin/Unpin (toggles based on `pinnedNodeIds`), Hide, Copy
// RID. The menu always renders on `rightClickNode` (unlike VTX-024
// which only opened for already-pinned nodes); dismissal is via a
// document-level mousedown listener so a click on the Sigma canvas
// also closes the popup. The menu intentionally registers ONLY
// `rightClickNode` because VertexSelectionLayer already owns
// clickNode/clickStage — a second subscriber would clobber its
// handlers under the test mock's merge-by-assignment behaviour.

import { useEffect, useRef, useState } from 'react';
import { useRegisterEvents, useSigma } from '@react-sigma/core';

import type { VertexObjectSummary } from './VertexSelectionSidebar';

export interface VertexNodeContextMenuProps {
  pinnedNodeIds: ReadonlySet<string>;
  objectsByRid: ReadonlyMap<string, VertexObjectSummary>;
  onPin: (nodeId: string, x: number, y: number) => void;
  onUnpin: (nodeId: string) => void;
  onHide: (nodeId: string) => void;
  /** Optional callback for Search Around (real flow lands in VTX-069). */
  onSearchAround?: (nodeId: string) => void;
}

interface SigmaNodePayload {
  node: string;
  event: { original: MouseEvent };
}

interface MenuState {
  nodeId: string;
  /** Viewport coords for the popup's top-left. */
  x: number;
  y: number;
}

async function copyToClipboard(text: string): Promise<void> {
  try {
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
    }
  } catch {
    // Clipboard may be unavailable (insecure context, denied permission, etc.).
    // The action is best-effort; silently dropping the failure mirrors how the
    // rest of the workspace handles opportunistic side effects (drag PATCH).
  }
}

function buildExplorerUrl(rid: string, ontologyApiName?: string): string {
  const params = new URLSearchParams({ objectRid: rid });
  const ont = ontologyApiName ?? '';
  return `/explorer/${encodeURIComponent(ont)}?${params.toString()}`;
}

export function VertexNodeContextMenu({
  pinnedNodeIds,
  objectsByRid,
  onPin,
  onUnpin,
  onHide,
  onSearchAround,
}: VertexNodeContextMenuProps) {
  const sigma = useSigma();
  const registerEvents = useRegisterEvents();

  const [menu, setMenu] = useState<MenuState | null>(null);

  useEffect(() => {
    registerEvents({
      rightClickNode: (payload: SigmaNodePayload) => {
        const e = payload.event.original;
        if (typeof e.preventDefault === 'function') e.preventDefault();
        const container = sigma.getContainer();
        const rect = container ? container.getBoundingClientRect() : { left: 0, top: 0 };
        setMenu({
          nodeId: payload.node,
          x: e.clientX - rect.left,
          y: e.clientY - rect.top,
        });
      },
    });
  }, [registerEvents, sigma]);

  // Close on any outside mousedown.
  const menuElRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!menu) return;
    const onDocDown = (e: MouseEvent) => {
      if (menuElRef.current && menuElRef.current.contains(e.target as Node)) return;
      setMenu(null);
    };
    document.addEventListener('mousedown', onDocDown);
    return () => document.removeEventListener('mousedown', onDocDown);
  }, [menu]);

  if (!menu) return null;
  const nodeId = menu.nodeId;
  const isPinned = pinnedNodeIds.has(nodeId);
  const summary = objectsByRid.get(nodeId);
  const explorerUrl = buildExplorerUrl(nodeId, summary?.ontologyApiName);

  const dismiss = () => setMenu(null);

  const handleSearchAround = () => {
    dismiss();
    onSearchAround?.(nodeId);
  };

  const handleOpenExplorer = () => {
    dismiss();
    if (typeof window !== 'undefined' && typeof window.open === 'function') {
      window.open(explorerUrl, '_blank', 'noopener');
    }
  };

  const handlePinToggle = () => {
    dismiss();
    if (isPinned) {
      onUnpin(nodeId);
      return;
    }
    // Pin at the node's current rendered coordinates. Read from the
    // graphology Graph so the pin sticks the node *where the user sees it*.
    const graph = sigma.getGraph();
    let x = 0;
    let y = 0;
    if (graph && typeof graph.hasNode === 'function' && graph.hasNode(nodeId)) {
      const gx = graph.getNodeAttribute(nodeId, 'x');
      const gy = graph.getNodeAttribute(nodeId, 'y');
      if (typeof gx === 'number' && Number.isFinite(gx)) x = gx;
      if (typeof gy === 'number' && Number.isFinite(gy)) y = gy;
    }
    onPin(nodeId, x, y);
  };

  const handleHide = () => {
    dismiss();
    onHide(nodeId);
  };

  const handleCopyRid = () => {
    dismiss();
    void copyToClipboard(nodeId);
  };

  const itemClass =
    'block w-full px-3 py-1 text-left text-zinc-100 hover:bg-zinc-800';

  return (
    <div
      ref={menuElRef}
      data-testid="vertex-node-context-menu"
      data-node={nodeId}
      className="absolute z-30 rounded border border-zinc-700 bg-zinc-950 text-xs shadow-lg"
      style={{ left: `${menu.x}px`, top: `${menu.y}px`, minWidth: '180px' }}
      role="menu"
    >
      <button
        type="button"
        data-testid="vertex-node-context-menu-search-around"
        role="menuitem"
        onClick={handleSearchAround}
        className={itemClass}
      >
        Search Around
      </button>
      <button
        type="button"
        data-testid="vertex-node-context-menu-open-explorer"
        role="menuitem"
        onClick={handleOpenExplorer}
        className={itemClass}
      >
        Open in Object Explorer
      </button>
      {isPinned ? (
        <button
          type="button"
          data-testid="vertex-node-context-menu-unpin"
          role="menuitem"
          onClick={handlePinToggle}
          className={itemClass}
        >
          Unpin
        </button>
      ) : (
        <button
          type="button"
          data-testid="vertex-node-context-menu-pin"
          role="menuitem"
          onClick={handlePinToggle}
          className={itemClass}
        >
          Pin
        </button>
      )}
      <button
        type="button"
        data-testid="vertex-node-context-menu-hide"
        role="menuitem"
        onClick={handleHide}
        className={itemClass}
      >
        Hide
      </button>
      <button
        type="button"
        data-testid="vertex-node-context-menu-copy-rid"
        role="menuitem"
        onClick={handleCopyRid}
        className={itemClass}
      >
        Copy RID
      </button>
    </div>
  );
}
