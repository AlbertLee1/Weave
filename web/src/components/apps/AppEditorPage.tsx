import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  createApp,
  getApp,
  listApps,
  updateApp,
  type App,
} from '../../api/apps';
import {
  COMPONENT_TYPES,
  INSTANCE_DND_MIME,
  MAX_COLUMNS,
  PALETTE_DND_MIME,
  distributeWidths,
  instancesFromLayout,
  instancesToLayout,
  makeInstance,
  type ComponentInstance,
  type ComponentType,
} from './layout';

// US-392: App Component Palette + Canvas + Property Panel.
//
// PRD asked for react-dnd; the lib isn't installed and the codebase
// already standardised on native HTML5 DnD for the analogous Dashboard
// editor (see DashboardEditorPage learning #209 in progress.txt) — we
// follow that precedent rather than pull in a 30+kB dep used only here.
//
// Layout encoding
// ---------------
// pkg/apps/layout.go::ValidateLayout requires a single root node with
// row→col→… recursion and a row's direct cols summing to ≤12. The
// editor's canvas is therefore modelled as a single horizontal row of
// 1..MAX_COLUMNS components, each rendered as a col with an
// auto-distributed width (sum always 12). Future stories (US-393+) will
// extend the canvas to multi-row stacks.
//
// State model
// -----------
// `instances` is a flat array of `{id, componentType, props}` records.
// Drag from the palette → drop onto the canvas → instance appended.
// Clicking an instance selects it; the right-hand property panel binds
// edits onto its props bag.

export interface AppEditorPageProps {
  // Optional rid: when provided, the editor loads that App's live row on
  // mount and routes Save through PUT. When absent, the editor starts
  // with a blank canvas and routes Save through POST → onSaved.
  rid?: string;
  onSaved?: (rid: string) => void;
}

export function AppEditorPage({ rid, onSaved }: AppEditorPageProps = {}) {
  const [instances, setInstances] = useState<ComponentInstance[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [name, setName] = useState('Untitled App');
  const [savedRid, setSavedRid] = useState<string | null>(rid ?? null);
  const [savedApps, setSavedApps] = useState<App[]>([]);
  const [saveStatus, setSaveStatus] = useState<
    'idle' | 'saving' | 'saved' | 'error'
  >('idle');
  const [saveError, setSaveError] = useState<string | null>(null);
  const dragOverCanvas = useRef(false);
  const [canvasIsDragging, setCanvasIsDragging] = useState(false);

  // Load existing app on mount when rid is supplied.
  useEffect(() => {
    if (!rid) return;
    let cancelled = false;
    getApp(rid)
      .then((row) => {
        if (cancelled) return;
        setName(row.name);
        setSavedRid(row.rid);
        setInstances(instancesFromLayout(row.layoutJson));
      })
      .catch(() => {
        // Fall through to blank editor — degraded mode (no PG store)
        // returns 500 AppsUnavailable; the wider page chrome surfaces
        // a toast in production.
      });
    return () => {
      cancelled = true;
    };
  }, [rid]);

  // Eagerly fetch the caller's saved apps so the Load picker is
  // populated on first render (degraded mode → empty list).
  useEffect(() => {
    let cancelled = false;
    listApps()
      .then((resp) => {
        if (cancelled) return;
        setSavedApps(resp.apps ?? []);
      })
      .catch(() => {
        if (cancelled) return;
        setSavedApps([]);
      });
    return () => {
      cancelled = true;
    };
  }, [savedRid]);

  const selected = useMemo(
    () => instances.find((i) => i.id === selectedId) ?? null,
    [instances, selectedId],
  );

  const widths = useMemo(() => distributeWidths(instances.length), [instances]);

  const addInstance = useCallback(
    (type: ComponentType) => {
      if (instances.length >= MAX_COLUMNS) return;
      const inst = makeInstance(type);
      setInstances((prev) => [...prev, inst]);
      setSelectedId(inst.id);
    },
    [instances.length],
  );

  const removeInstance = useCallback((id: string) => {
    setInstances((prev) => prev.filter((i) => i.id !== id));
    setSelectedId((cur) => (cur === id ? null : cur));
  }, []);

  const moveInstance = useCallback((sourceId: string, targetIndex: number) => {
    setInstances((prev) => {
      const idx = prev.findIndex((i) => i.id === sourceId);
      if (idx === -1) return prev;
      const next = prev.slice();
      const [moved] = next.splice(idx, 1);
      const insertAt = Math.min(Math.max(targetIndex, 0), next.length);
      next.splice(insertAt, 0, moved);
      return next;
    });
  }, []);

  const patchProps = useCallback((id: string, patch: Record<string, unknown>) => {
    setInstances((prev) =>
      prev.map((i) =>
        i.id === id ? { ...i, props: { ...i.props, ...patch } } : i,
      ),
    );
  }, []);

  const handlePaletteDragStart = useCallback(
    (e: React.DragEvent, type: ComponentType) => {
      e.dataTransfer.setData(PALETTE_DND_MIME, type);
      e.dataTransfer.effectAllowed = 'copy';
    },
    [],
  );

  const handleInstanceDragStart = useCallback(
    (e: React.DragEvent, id: string) => {
      e.dataTransfer.setData(INSTANCE_DND_MIME, id);
      e.dataTransfer.effectAllowed = 'move';
    },
    [],
  );

  const handleCanvasDragOver = useCallback((e: React.DragEvent) => {
    const types = Array.from(e.dataTransfer.types);
    if (
      types.includes(PALETTE_DND_MIME) ||
      types.includes(INSTANCE_DND_MIME)
    ) {
      e.preventDefault();
      e.dataTransfer.dropEffect = types.includes(INSTANCE_DND_MIME)
        ? 'move'
        : 'copy';
      if (!dragOverCanvas.current) {
        dragOverCanvas.current = true;
        setCanvasIsDragging(true);
      }
    }
  }, []);

  const handleCanvasDragLeave = useCallback(() => {
    dragOverCanvas.current = false;
    setCanvasIsDragging(false);
  }, []);

  const computeDropIndex = useCallback(
    (e: React.DragEvent, container: HTMLElement) => {
      const rect = container.getBoundingClientRect();
      const relX = e.clientX - rect.left;
      const ratio = rect.width === 0 ? 0 : relX / rect.width;
      return Math.min(
        instances.length,
        Math.max(0, Math.round(ratio * Math.max(instances.length, 1))),
      );
    },
    [instances.length],
  );

  const handleCanvasDrop = useCallback(
    (e: React.DragEvent) => {
      const paletteType = e.dataTransfer.getData(PALETTE_DND_MIME);
      const movedId = e.dataTransfer.getData(INSTANCE_DND_MIME);
      e.preventDefault();
      dragOverCanvas.current = false;
      setCanvasIsDragging(false);
      if (paletteType) {
        if (instances.length >= MAX_COLUMNS) return;
        addInstance(paletteType as ComponentType);
        return;
      }
      if (movedId) {
        const dropIdx = computeDropIndex(
          e,
          e.currentTarget as HTMLElement,
        );
        moveInstance(movedId, dropIdx);
      }
    },
    [addInstance, computeDropIndex, instances.length, moveInstance],
  );

  const handleSave = useCallback(async () => {
    setSaveStatus('saving');
    setSaveError(null);
    const layoutJson = instancesToLayout(instances);
    try {
      if (savedRid) {
        await updateApp({ rid: savedRid, name, layoutJson });
      } else {
        const created = await createApp({ name, layoutJson });
        setSavedRid(created.rid);
        onSaved?.(created.rid);
      }
      setSaveStatus('saved');
    } catch (err) {
      setSaveStatus('error');
      setSaveError(
        err instanceof Error ? err.message : 'Save failed',
      );
    }
  }, [instances, name, onSaved, savedRid]);

  const canAddMore = instances.length < MAX_COLUMNS;

  return (
    <div data-testid="app-editor-page" className="min-h-full p-6">
      <header className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
            App Editor
          </h1>
          <p className="text-sm text-text-secondary mt-1">
            Drag components from the palette onto the canvas. Click a component
            to edit its properties.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <input
            type="text"
            data-testid="app-name-input"
            aria-label="App name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="px-2 py-1 rounded border border-border bg-bg-secondary text-sm text-text-primary"
          />
          <button
            type="button"
            data-testid="app-save"
            onClick={() => {
              void handleSave();
            }}
            className="px-3 py-1.5 rounded border border-border bg-accent-primary/20 text-sm text-text-primary hover:border-accent-primary"
          >
            {savedRid ? 'Save' : 'Save New'}
          </button>
          {saveStatus !== 'idle' && (
            <span
              data-testid="app-save-status"
              data-save-status={saveStatus}
              className="text-xs font-mono text-text-secondary"
            >
              {saveStatus === 'saving' && 'Saving…'}
              {saveStatus === 'saved' && 'Saved'}
              {saveStatus === 'error' && (saveError ?? 'Save failed')}
            </span>
          )}
          {savedApps.length > 0 && (
            <select
              data-testid="app-load-select"
              aria-label="Load saved app"
              value={savedRid ?? ''}
              onChange={(e) => {
                const next = e.target.value;
                if (!next) return;
                onSaved?.(next);
              }}
              className="px-2 py-1 rounded border border-border bg-bg-secondary text-xs text-text-primary"
            >
              <option value="">Load saved…</option>
              {savedApps.map((a) => (
                <option key={a.rid} value={a.rid}>
                  {a.name}
                </option>
              ))}
            </select>
          )}
        </div>
      </header>

      <div className="grid grid-cols-12 gap-4">
        <ComponentPalette
          onDragStart={handlePaletteDragStart}
          onAdd={addInstance}
          canAddMore={canAddMore}
        />

        <div className="col-span-7">
          <Canvas
            instances={instances}
            widths={widths}
            selectedId={selectedId}
            isDropTarget={canvasIsDragging}
            onSelect={setSelectedId}
            onRemove={removeInstance}
            onInstanceDragStart={handleInstanceDragStart}
            onDragOver={handleCanvasDragOver}
            onDragLeave={handleCanvasDragLeave}
            onDrop={handleCanvasDrop}
          />
        </div>

        <PropertyPanel selected={selected} onPatch={patchProps} />
      </div>
    </div>
  );
}

interface ComponentPaletteProps {
  onDragStart: (e: React.DragEvent, type: ComponentType) => void;
  onAdd: (type: ComponentType) => void;
  canAddMore: boolean;
}

function ComponentPalette({
  onDragStart,
  onAdd,
  canAddMore,
}: ComponentPaletteProps) {
  return (
    <aside
      data-testid="app-palette"
      className="col-span-3 border border-border rounded bg-bg-secondary/40 p-3"
    >
      <h2 className="text-sm font-mono font-medium text-text-secondary mb-2">
        Components
      </h2>
      <ul className="flex flex-col gap-2">
        {COMPONENT_TYPES.map((meta) => (
          <li key={meta.type}>
            <button
              type="button"
              draggable
              data-testid={`app-palette-item-${meta.type}`}
              data-component-type={meta.type}
              disabled={!canAddMore}
              onDragStart={(e) => onDragStart(e, meta.type)}
              onClick={() => {
                if (canAddMore) onAdd(meta.type);
              }}
              className="w-full text-left px-3 py-2 rounded border border-border bg-bg-primary text-text-primary hover:border-accent-primary disabled:opacity-50 disabled:cursor-not-allowed cursor-grab active:cursor-grabbing"
            >
              <div className="text-sm font-medium">{meta.label}</div>
              <div className="text-xs text-text-secondary">
                {meta.description}
              </div>
            </button>
          </li>
        ))}
      </ul>
      {!canAddMore && (
        <p
          data-testid="app-palette-full"
          className="text-xs text-text-secondary mt-3 italic"
        >
          Canvas is full ({MAX_COLUMNS} components max).
        </p>
      )}
    </aside>
  );
}

interface CanvasProps {
  instances: ComponentInstance[];
  widths: number[];
  selectedId: string | null;
  isDropTarget: boolean;
  onSelect: (id: string) => void;
  onRemove: (id: string) => void;
  onInstanceDragStart: (e: React.DragEvent, id: string) => void;
  onDragOver: (e: React.DragEvent) => void;
  onDragLeave: () => void;
  onDrop: (e: React.DragEvent) => void;
}

function Canvas({
  instances,
  widths,
  selectedId,
  isDropTarget,
  onSelect,
  onRemove,
  onInstanceDragStart,
  onDragOver,
  onDragLeave,
  onDrop,
}: CanvasProps) {
  return (
    <div
      data-testid="app-canvas"
      data-drop-target={isDropTarget ? 'true' : 'false'}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
      className={`min-h-[300px] border-2 rounded p-3 transition-colors ${
        isDropTarget
          ? 'border-accent-primary bg-accent-primary/10'
          : 'border-dashed border-border bg-bg-secondary/40'
      }`}
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(12, minmax(0, 1fr))',
        gap: 8,
      }}
    >
      {instances.length === 0 ? (
        <div
          data-testid="app-canvas-empty"
          style={{ gridColumn: '1 / -1' }}
          className="flex flex-col items-center justify-center text-center text-text-secondary py-12"
        >
          <p className="text-sm">No components yet.</p>
          <p className="text-xs font-mono mt-1">
            Drag a component from the palette to start building.
          </p>
        </div>
      ) : (
        instances.map((inst, idx) => (
          <CanvasInstance
            key={inst.id}
            instance={inst}
            width={widths[idx]}
            isSelected={selectedId === inst.id}
            onSelect={() => onSelect(inst.id)}
            onRemove={() => onRemove(inst.id)}
            onDragStart={(e) => onInstanceDragStart(e, inst.id)}
          />
        ))
      )}
    </div>
  );
}

interface CanvasInstanceProps {
  instance: ComponentInstance;
  width: number;
  isSelected: boolean;
  onSelect: () => void;
  onRemove: () => void;
  onDragStart: (e: React.DragEvent) => void;
}

function CanvasInstance({
  instance,
  width,
  isSelected,
  onSelect,
  onRemove,
  onDragStart,
}: CanvasInstanceProps) {
  const meta = COMPONENT_TYPES.find((c) => c.type === instance.componentType);
  return (
    <div
      data-testid="app-canvas-instance"
      data-instance-id={instance.id}
      data-component-type={instance.componentType}
      data-instance-width={width}
      data-selected={isSelected ? 'true' : 'false'}
      draggable
      onDragStart={onDragStart}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect();
        }
      }}
      role="button"
      tabIndex={0}
      style={{ gridColumn: `span ${width}` }}
      className={`relative border rounded bg-bg-primary text-text-primary p-3 cursor-pointer overflow-hidden ${
        isSelected
          ? 'border-accent-primary ring-1 ring-accent-primary'
          : 'border-border hover:border-accent-primary/60'
      }`}
    >
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs font-mono text-text-secondary">
          {meta?.label ?? instance.componentType}
        </span>
        <button
          type="button"
          data-testid="app-canvas-instance-remove"
          aria-label={`Remove ${meta?.label ?? instance.componentType}`}
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
          className="text-text-secondary hover:text-accent-error text-xs px-1"
        >
          ×
        </button>
      </div>
      <CanvasInstancePreview instance={instance} />
    </div>
  );
}

function CanvasInstancePreview({
  instance,
}: {
  instance: ComponentInstance;
}) {
  switch (instance.componentType) {
    case 'table':
      return (
        <div
          data-testid="app-preview-table"
          className="text-xs text-text-secondary"
        >
          <div className="font-mono">Table</div>
          <div className="truncate">
            {String(instance.props.objectSet ?? '— bind ObjectSet —')}
          </div>
        </div>
      );
    case 'form':
      return (
        <div
          data-testid="app-preview-form"
          className="text-xs text-text-secondary"
        >
          <div className="font-mono">Form</div>
          <div className="truncate">
            {String(instance.props.actionType ?? '— bind ActionType —')}
          </div>
        </div>
      );
    case 'chart':
      return (
        <div
          data-testid="app-preview-chart"
          data-chart-type={String(instance.props.chartType ?? 'bar')}
          className="text-xs text-text-secondary"
        >
          <div className="font-mono">Chart</div>
          <div className="truncate">
            {String(instance.props.title ?? 'Untitled chart')}
          </div>
        </div>
      );
    case 'button':
      return (
        <div
          data-testid="app-preview-button"
          className="text-xs text-text-secondary"
        >
          <div className="font-mono">Button</div>
          <div className="truncate">
            {String(instance.props.label ?? 'Run')}
          </div>
        </div>
      );
    case 'objectCard':
      return (
        <div
          data-testid="app-preview-objectCard"
          className="text-xs text-text-secondary"
        >
          <div className="font-mono">Object Card</div>
          <div className="truncate">
            {String(instance.props.objectType ?? '— bind ObjectType —')}
          </div>
        </div>
      );
    case 'text':
      return (
        <div
          data-testid="app-preview-text"
          className="text-xs text-text-secondary"
        >
          <div className="font-mono">Text</div>
          <div className="truncate">
            {String(instance.props.content ?? '— enter content —')}
          </div>
        </div>
      );
  }
}

interface PropertyPanelProps {
  selected: ComponentInstance | null;
  onPatch: (id: string, patch: Record<string, unknown>) => void;
}

function PropertyPanel({ selected, onPatch }: PropertyPanelProps) {
  if (!selected) {
    return (
      <aside
        data-testid="app-property-panel"
        data-empty="true"
        className="col-span-2 border border-border rounded bg-bg-secondary/40 p-3"
      >
        <h2 className="text-sm font-mono font-medium text-text-secondary mb-2">
          Properties
        </h2>
        <p className="text-xs text-text-secondary">
          Select a component to edit its properties.
        </p>
      </aside>
    );
  }

  const meta = COMPONENT_TYPES.find((c) => c.type === selected.componentType);

  return (
    <aside
      data-testid="app-property-panel"
      data-component-type={selected.componentType}
      data-instance-id={selected.id}
      className="col-span-2 border border-border rounded bg-bg-secondary/40 p-3"
    >
      <h2 className="text-sm font-mono font-medium text-text-secondary mb-2">
        {meta?.label ?? selected.componentType}
      </h2>
      <ComponentPropertyFields
        instance={selected}
        onPatch={(patch) => onPatch(selected.id, patch)}
      />
    </aside>
  );
}

interface ComponentPropertyFieldsProps {
  instance: ComponentInstance;
  onPatch: (patch: Record<string, unknown>) => void;
}

function ComponentPropertyFields({
  instance,
  onPatch,
}: ComponentPropertyFieldsProps) {
  switch (instance.componentType) {
    case 'table':
      return (
        <div className="flex flex-col gap-2">
          <PropField
            label="ObjectSet RID"
            testId="prop-table-objectSet"
            value={String(instance.props.objectSet ?? '')}
            onChange={(v) => onPatch({ objectSet: v })}
            placeholder="ri.objectSet…"
          />
          <PropField
            label="Columns (comma-separated)"
            testId="prop-table-columns"
            value={
              Array.isArray(instance.props.columns)
                ? (instance.props.columns as string[]).join(', ')
                : ''
            }
            onChange={(v) =>
              onPatch({
                columns: v
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
              })
            }
            placeholder="name, status"
          />
        </div>
      );
    case 'form':
      return (
        <PropField
          label="ActionType API name"
          testId="prop-form-actionType"
          value={String(instance.props.actionType ?? '')}
          onChange={(v) => onPatch({ actionType: v })}
          placeholder="createCustomer"
        />
      );
    case 'chart':
      return (
        <div className="flex flex-col gap-2">
          <label className="flex flex-col gap-1 text-xs text-text-secondary">
            Chart Type
            <select
              data-testid="prop-chart-type"
              value={String(instance.props.chartType ?? 'bar')}
              onChange={(e) => onPatch({ chartType: e.target.value })}
              className="px-2 py-1 rounded border border-border bg-bg-primary text-sm text-text-primary"
            >
              <option value="bar">Bar</option>
              <option value="line">Line</option>
              <option value="pie">Pie</option>
            </select>
          </label>
          <PropField
            label="Title"
            testId="prop-chart-title"
            value={String(instance.props.title ?? '')}
            onChange={(v) => onPatch({ title: v })}
          />
        </div>
      );
    case 'button':
      return (
        <div className="flex flex-col gap-2">
          <PropField
            label="Label"
            testId="prop-button-label"
            value={String(instance.props.label ?? '')}
            onChange={(v) => onPatch({ label: v })}
          />
          <PropField
            label="ActionType"
            testId="prop-button-actionType"
            value={String(instance.props.actionType ?? '')}
            onChange={(v) => onPatch({ actionType: v })}
            placeholder="approveOrder"
          />
        </div>
      );
    case 'objectCard':
      return (
        <div className="flex flex-col gap-2">
          <PropField
            label="ObjectType API name"
            testId="prop-objectCard-objectType"
            value={String(instance.props.objectType ?? '')}
            onChange={(v) => onPatch({ objectType: v })}
          />
          <PropField
            label="Object PK"
            testId="prop-objectCard-objectId"
            value={String(instance.props.objectId ?? '')}
            onChange={(v) => onPatch({ objectId: v })}
          />
        </div>
      );
    case 'text':
      return (
        <label className="flex flex-col gap-1 text-xs text-text-secondary">
          Content
          <textarea
            data-testid="prop-text-content"
            value={String(instance.props.content ?? '')}
            onChange={(e) => onPatch({ content: e.target.value })}
            rows={6}
            className="px-2 py-1 rounded border border-border bg-bg-primary text-sm text-text-primary"
          />
        </label>
      );
  }
}

interface PropFieldProps {
  label: string;
  testId: string;
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
}

function PropField({
  label,
  testId,
  value,
  onChange,
  placeholder,
}: PropFieldProps) {
  return (
    <label className="flex flex-col gap-1 text-xs text-text-secondary">
      {label}
      <input
        type="text"
        data-testid={testId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="px-2 py-1 rounded border border-border bg-bg-primary text-sm text-text-primary"
      />
    </label>
  );
}

