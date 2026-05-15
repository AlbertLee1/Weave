// LayersDragPanel — drag a Layer chip onto the graph canvas to auto-load
// the first N objects of that type via OSS search (VTX-104).
//
// PRD names @dnd-kit/core as the underlying library but the public
// contract that VTX-104 cares about is "drop triggers a load of 50
// objects, not how the drag is wired". We use the native HTML5
// dragstart / dragover / drop events here — they cover the BDD shape,
// keep web/ bundle size flat, and let the unit test fire events without
// pulling in a heavy DnD harness. Swapping to @dnd-kit later is a
// purely additive change on this surface.

import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import '../i18n';

export interface LayerSpec {
  objectType: string;
  label: string;
}

export interface SearchFn {
  (objectType: string, pageSize: number): Promise<Array<{ id: string; properties: Record<string, unknown> }>>;
}

export interface LayersDragPanelProps {
  layers: LayerSpec[];
  search: SearchFn;
  onObjectsLoaded: (objectType: string, objects: Array<{ id: string }>) => void;
}

const DRAG_MIME = 'application/x-weave-vertex-layer';

export function LayersDragPanel({ layers, search, onObjectsLoaded }: LayersDragPanelProps) {
  const { t } = useTranslation();
  const [pending, setPending] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [hover, setHover] = useState(false);

  function onDragStart(e: React.DragEvent<HTMLDivElement>, layer: LayerSpec) {
    e.dataTransfer.setData(DRAG_MIME, layer.objectType);
    e.dataTransfer.effectAllowed = 'copy';
  }

  function onDragOver(e: React.DragEvent<HTMLDivElement>) {
    if (e.dataTransfer.types.includes(DRAG_MIME)) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
      setHover(true);
    }
  }

  function onDragLeave() {
    setHover(false);
  }

  async function onDrop(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault();
    setHover(false);
    const objectType = e.dataTransfer.getData(DRAG_MIME);
    if (!objectType) return;
    setPending(objectType);
    setError(null);
    try {
      const objects = await search(objectType, 50);
      onObjectsLoaded(objectType, objects);
    } catch (err) {
      setError(String(err));
    } finally {
      setPending(null);
    }
  }

  return (
    <div className="flex h-full gap-3">
      <aside data-testid="vertex-layers-panel" className="w-48 space-y-2 border-r p-2">
        <div className="text-xs font-semibold uppercase text-zinc-500">{t('vertex.layers.title')}</div>
        {layers.map((layer) => (
          <div
            key={layer.objectType}
            data-testid={`vertex-layer-${layer.objectType}`}
            draggable
            onDragStart={(e) => onDragStart(e, layer)}
            className="cursor-grab rounded border bg-white px-2 py-1 text-sm shadow-sm hover:bg-zinc-50 dark:bg-zinc-800 dark:hover:bg-zinc-700"
          >
            {layer.label}
          </div>
        ))}
      </aside>
      <div
        data-testid="vertex-graph-canvas"
        onDragOver={onDragOver}
        onDragLeave={onDragLeave}
        onDrop={onDrop}
        className={
          'flex flex-1 items-center justify-center rounded border-2 border-dashed text-sm transition ' +
          (hover ? 'border-blue-500 bg-blue-50/40' : 'border-zinc-300')
        }
      >
        {pending && (
          <span data-testid="vertex-canvas-loading">
            {t('vertex.layers.loadingPlaceholder', { objectType: pending })}
          </span>
        )}
        {!pending && error && <span data-testid="vertex-canvas-error" className="text-red-600">{error}</span>}
        {!pending && !error && <span className="text-zinc-500">{t('vertex.layers.dropHere')}</span>}
      </div>
    </div>
  );
}
