// VTX-024 — minimal right-click menu for nodes. Surfaces a single
// "Unpin" item for nodes that are user-pinned so the manual-drag flow
// can be reversed. The full multi-item context menu (Search Around,
// Open in Object Explorer, Pin/Unpin, Hide, Copy RID, …) lands in
// VTX-026 — this file is intentionally scoped to the BDD line that
// "Given 用户右键 → Unpin Then pinned=false".

import { useEffect, useRef, useState } from 'react';
import { useRegisterEvents, useSigma } from '@react-sigma/core';

export interface VertexNodeContextMenuProps {
  pinnedNodeIds: ReadonlySet<string>;
  onUnpin: (nodeId: string) => void;
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

export function VertexNodeContextMenu({
  pinnedNodeIds,
  onUnpin,
}: VertexNodeContextMenuProps) {
  const sigma = useSigma();
  const registerEvents = useRegisterEvents();

  const [menu, setMenu] = useState<MenuState | null>(null);

  useEffect(() => {
    // Only register rightClickNode — VertexSelectionLayer owns
    // clickNode/clickStage and a second subscriber would clobber its
    // handlers in jsdom-style merge-by-assignment mocks. Dismissal is
    // handled by the document-level mousedown listener below + by the
    // Unpin button's onClick.
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

  // Close on any outside mousedown. Bound at document level so a click on
  // the Sigma canvas (which doesn't fire DOM events on the menu DOM) also
  // dismisses the popup.
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
  const isPinned = pinnedNodeIds.has(menu.nodeId);
  if (!isPinned) {
    // Nothing to offer on a non-pinned node in VTX-024 (full menu lands in VTX-026).
    return null;
  }
  return (
    <div
      ref={menuElRef}
      data-testid="vertex-node-context-menu"
      data-node={menu.nodeId}
      className="absolute z-30 rounded border border-zinc-700 bg-zinc-950 text-xs shadow-lg"
      style={{ left: `${menu.x}px`, top: `${menu.y}px`, minWidth: '120px' }}
      role="menu"
    >
      <button
        type="button"
        data-testid="vertex-node-context-menu-unpin"
        role="menuitem"
        onClick={() => {
          const id = menu.nodeId;
          setMenu(null);
          onUnpin(id);
        }}
        className="block w-full px-3 py-1 text-left text-zinc-100 hover:bg-zinc-800"
      >
        Unpin
      </button>
    </div>
  );
}
