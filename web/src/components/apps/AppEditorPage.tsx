import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  createApp,
  getApp,
  listApps,
  updateApp,
  type App,
  type AppEvent,
  type AppVariable,
  type AppVariableType,
} from '../../api/apps';
import {
  COMPONENT_TYPES,
  INSTANCE_DND_MIME,
  MAX_COLUMNS,
  PALETTE_DND_MIME,
  VARIABLE_NAME_PATTERN,
  VARIABLE_TYPES,
  decodeLayout,
  distributeWidths,
  instancesToLayout,
  makeEvent,
  makeInstance,
  makeVariable,
  substituteVariables,
  type ComponentInstance,
  type ComponentType,
} from './layout';
import {
  dispatchEvent,
  initialVariableState,
  type VariableState,
} from './runtime';

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
  const [variables, setVariables] = useState<AppVariable[]>([]);
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
  const [mode, setMode] = useState<'edit' | 'preview'>('edit');
  const [runtimeState, setRuntimeState] = useState<VariableState>({});
  const [runtimeMessage, setRuntimeMessage] = useState<string | null>(null);

  // Load existing app on mount when rid is supplied.
  useEffect(() => {
    if (!rid) return;
    let cancelled = false;
    getApp(rid)
      .then((row) => {
        if (cancelled) return;
        setName(row.name);
        setSavedRid(row.rid);
        const decoded = decodeLayout(row.layoutJson);
        setInstances(decoded.instances);
        setVariables(decoded.variables);
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

  const setOnClick = useCallback(
    (id: string, event: AppEvent | null) => {
      setInstances((prev) =>
        prev.map((i) => {
          if (i.id !== id) return i;
          const events = { ...i.events };
          if (event === null) delete events.onClick;
          else events.onClick = event;
          return { ...i, events };
        }),
      );
    },
    [],
  );

  const addVariable = useCallback(() => {
    setVariables((prev) => {
      let n = prev.length + 1;
      let candidate = `var${n}`;
      const taken = new Set(prev.map((v) => v.name));
      while (taken.has(candidate)) {
        n += 1;
        candidate = `var${n}`;
      }
      return [...prev, makeVariable(candidate, 'string', '')];
    });
  }, []);

  const updateVariable = useCallback(
    (index: number, patch: Partial<AppVariable>) => {
      setVariables((prev) =>
        prev.map((v, i) => (i === index ? { ...v, ...patch } : v)),
      );
    },
    [],
  );

  const removeVariable = useCallback((index: number) => {
    setVariables((prev) => prev.filter((_, i) => i !== index));
  }, []);

  const enterPreview = useCallback(() => {
    setRuntimeState(initialVariableState(variables));
    setRuntimeMessage(null);
    setMode('preview');
  }, [variables]);

  const exitPreview = useCallback(() => {
    setMode('edit');
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
    const layoutJson = instancesToLayout(instances, variables);
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
  }, [instances, name, onSaved, savedRid, variables]);

  const canAddMore = instances.length < MAX_COLUMNS;
  const isPreview = mode === 'preview';

  const handleRuntimeEvent = useCallback(
    async (event: AppEvent) => {
      try {
        await dispatchEvent(event, {
          variables,
          state: runtimeState,
          setState: setRuntimeState,
          navigate: (to) => {
            setRuntimeMessage(`navigate → ${to}`);
          },
          runAction: (actionType, params) => {
            const summary =
              Object.keys(params).length > 0
                ? `runAction ${actionType}(${JSON.stringify(params)})`
                : `runAction ${actionType}`;
            setRuntimeMessage(summary);
          },
        });
      } catch (err) {
        setRuntimeMessage(
          err instanceof Error ? `error: ${err.message}` : 'event failed',
        );
      }
    },
    [variables, runtimeState],
  );

  return (
    <div data-testid="app-editor-page" data-mode={mode} className="min-h-full p-6">
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
            data-testid="app-mode-toggle"
            onClick={() => (isPreview ? exitPreview() : enterPreview())}
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
          >
            {isPreview ? 'Back to Edit' : 'Preview'}
          </button>
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

      {isPreview ? (
        <RuntimeView
          instances={instances}
          variables={variables}
          state={runtimeState}
          onEvent={handleRuntimeEvent}
          message={runtimeMessage}
        />
      ) : (
        <div className="grid grid-cols-12 gap-4">
          <div className="col-span-3 flex flex-col gap-4">
            <ComponentPalette
              onDragStart={handlePaletteDragStart}
              onAdd={addInstance}
              canAddMore={canAddMore}
            />
            <VariablesPanel
              variables={variables}
              onAdd={addVariable}
              onUpdate={updateVariable}
              onRemove={removeVariable}
            />
          </div>

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

          <PropertyPanel
            selected={selected}
            variables={variables}
            onPatch={patchProps}
            onSetOnClick={setOnClick}
          />
        </div>
      )}
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
  variables: AppVariable[];
  onPatch: (id: string, patch: Record<string, unknown>) => void;
  onSetOnClick: (id: string, event: AppEvent | null) => void;
}

function PropertyPanel({
  selected,
  variables,
  onPatch,
  onSetOnClick,
}: PropertyPanelProps) {
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
      <EventsEditor
        instance={selected}
        variables={variables}
        onSetOnClick={(ev) => onSetOnClick(selected.id, ev)}
      />
    </aside>
  );
}

interface VariablesPanelProps {
  variables: AppVariable[];
  onAdd: () => void;
  onUpdate: (index: number, patch: Partial<AppVariable>) => void;
  onRemove: (index: number) => void;
}

function VariablesPanel({
  variables,
  onAdd,
  onUpdate,
  onRemove,
}: VariablesPanelProps) {
  return (
    <aside
      data-testid="app-variables-panel"
      data-variable-count={variables.length}
      className="border border-border rounded bg-bg-secondary/40 p-3"
    >
      <div className="flex items-center justify-between mb-2">
        <h2 className="text-sm font-mono font-medium text-text-secondary">
          Variables
        </h2>
        <button
          type="button"
          data-testid="app-variables-add"
          onClick={onAdd}
          className="text-xs px-2 py-0.5 rounded border border-border bg-bg-primary text-text-primary hover:border-accent-primary"
        >
          + Add
        </button>
      </div>
      {variables.length === 0 ? (
        <p
          data-testid="app-variables-empty"
          className="text-xs text-text-secondary italic"
        >
          No variables. Variables hold runtime state read by components and
          written by events.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {variables.map((v, idx) => {
            const nameValid = VARIABLE_NAME_PATTERN.test(v.name);
            const dupCount = variables.filter((x) => x.name === v.name).length;
            const isDuplicate = dupCount > 1;
            return (
              <li
                key={idx}
                data-testid="app-variable-row"
                data-variable-name={v.name}
                data-variable-type={v.type}
                data-variable-invalid={
                  !nameValid || isDuplicate ? 'true' : 'false'
                }
                className="flex flex-col gap-1 border border-border rounded p-2 bg-bg-primary"
              >
                <div className="flex items-center gap-1">
                  <input
                    type="text"
                    aria-label={`Variable name ${idx + 1}`}
                    data-testid={`app-variable-name-${idx}`}
                    value={v.name}
                    onChange={(e) => onUpdate(idx, { name: e.target.value })}
                    placeholder="varName"
                    className="flex-1 px-2 py-0.5 rounded border border-border bg-bg-secondary text-xs text-text-primary font-mono"
                  />
                  <button
                    type="button"
                    data-testid={`app-variable-remove-${idx}`}
                    aria-label={`Remove ${v.name}`}
                    onClick={() => onRemove(idx)}
                    className="text-text-secondary hover:text-accent-error text-xs px-1"
                  >
                    ×
                  </button>
                </div>
                <div className="flex items-center gap-1">
                  <select
                    aria-label={`Variable type ${idx + 1}`}
                    data-testid={`app-variable-type-${idx}`}
                    value={v.type}
                    onChange={(e) =>
                      onUpdate(idx, {
                        type: e.target.value as AppVariableType,
                      })
                    }
                    className="px-1 py-0.5 rounded border border-border bg-bg-secondary text-xs text-text-primary"
                  >
                    {VARIABLE_TYPES.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </select>
                  <input
                    type="text"
                    aria-label={`Variable default ${idx + 1}`}
                    data-testid={`app-variable-default-${idx}`}
                    value={v.default}
                    onChange={(e) =>
                      onUpdate(idx, { default: e.target.value })
                    }
                    placeholder="default"
                    className="flex-1 px-2 py-0.5 rounded border border-border bg-bg-secondary text-xs text-text-primary font-mono"
                  />
                </div>
                {!nameValid && v.name !== '' && (
                  <p
                    data-testid={`app-variable-error-${idx}`}
                    className="text-[10px] text-accent-error"
                  >
                    Names must match /[A-Za-z_][A-Za-z0-9_]*/.
                  </p>
                )}
                {isDuplicate && (
                  <p
                    data-testid={`app-variable-dup-${idx}`}
                    className="text-[10px] text-accent-error"
                  >
                    Duplicate variable name.
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </aside>
  );
}

interface EventsEditorProps {
  instance: ComponentInstance;
  variables: AppVariable[];
  onSetOnClick: (event: AppEvent | null) => void;
}

function EventsEditor({ instance, variables, onSetOnClick }: EventsEditorProps) {
  const onClick = instance.events?.onClick;
  const kind: AppEvent['kind'] | 'none' = onClick?.kind ?? 'none';

  const updateOnClick = useCallback(
    (next: AppEvent | null) => onSetOnClick(next),
    [onSetOnClick],
  );

  return (
    <div
      data-testid="app-events-editor"
      data-onclick-kind={kind}
      className="mt-3 pt-3 border-t border-border flex flex-col gap-2"
    >
      <h3 className="text-xs font-mono font-medium text-text-secondary">
        onClick
      </h3>
      <select
        aria-label="onClick handler kind"
        data-testid="app-event-onclick-kind"
        value={kind}
        onChange={(e) => {
          const next = e.target.value as AppEvent['kind'] | 'none';
          if (next === 'none') {
            updateOnClick(null);
          } else {
            updateOnClick(makeEvent(next));
          }
        }}
        className="px-1 py-0.5 rounded border border-border bg-bg-primary text-xs text-text-primary"
      >
        <option value="none">No handler</option>
        <option value="setVariable">Set Variable</option>
        <option value="runAction">Run Action</option>
        <option value="navigate">Navigate</option>
      </select>
      {onClick?.kind === 'setVariable' && (
        <div className="flex flex-col gap-1">
          <select
            aria-label="setVariable target"
            data-testid="app-event-onclick-setvariable-name"
            value={onClick.name}
            onChange={(e) =>
              updateOnClick({ ...onClick, name: e.target.value })
            }
            className="px-1 py-0.5 rounded border border-border bg-bg-primary text-xs text-text-primary"
          >
            <option value="">— select variable —</option>
            {variables.map((v) => (
              <option key={v.name} value={v.name}>
                {v.name} : {v.type}
              </option>
            ))}
          </select>
          <input
            type="text"
            aria-label="setVariable value"
            data-testid="app-event-onclick-setvariable-value"
            value={onClick.value}
            onChange={(e) =>
              updateOnClick({ ...onClick, value: e.target.value })
            }
            placeholder="value (use {{var}} for refs)"
            className="px-2 py-0.5 rounded border border-border bg-bg-primary text-xs text-text-primary font-mono"
          />
        </div>
      )}
      {onClick?.kind === 'runAction' && (
        <input
          type="text"
          aria-label="runAction ActionType"
          data-testid="app-event-onclick-runaction-actiontype"
          value={onClick.actionType}
          onChange={(e) =>
            updateOnClick({ ...onClick, actionType: e.target.value })
          }
          placeholder="action API name"
          className="px-2 py-0.5 rounded border border-border bg-bg-primary text-xs text-text-primary font-mono"
        />
      )}
      {onClick?.kind === 'navigate' && (
        <input
          type="text"
          aria-label="navigate target"
          data-testid="app-event-onclick-navigate-to"
          value={onClick.to}
          onChange={(e) => updateOnClick({ ...onClick, to: e.target.value })}
          placeholder="/path/{{var}}"
          className="px-2 py-0.5 rounded border border-border bg-bg-primary text-xs text-text-primary font-mono"
        />
      )}
    </div>
  );
}

interface RuntimeViewProps {
  instances: ComponentInstance[];
  variables: AppVariable[];
  state: VariableState;
  onEvent: (event: AppEvent) => void;
  message: string | null;
}

function RuntimeView({
  instances,
  variables,
  state,
  onEvent,
  message,
}: RuntimeViewProps) {
  const stringState = useMemo(() => {
    const out: Record<string, string | number | boolean> = {};
    for (const v of variables) {
      out[v.name] = state[v.name] ?? '';
    }
    return out;
  }, [variables, state]);

  const widths = useMemo(() => distributeWidths(instances.length), [instances]);

  return (
    <div data-testid="app-runtime-view" className="flex flex-col gap-3">
      <div
        data-testid="app-runtime-state"
        className="rounded border border-border bg-bg-secondary/40 p-2 text-xs font-mono text-text-secondary flex flex-wrap gap-3"
      >
        {variables.length === 0 && <span>No variables.</span>}
        {variables.map((v) => (
          <span
            key={v.name}
            data-testid={`app-runtime-var-${v.name}`}
            data-variable-value={String(state[v.name] ?? '')}
          >
            {v.name}
            <span className="text-text-tertiary">: {v.type}</span>
            <span className="ml-1 text-accent-primary">
              = {String(state[v.name] ?? '')}
            </span>
          </span>
        ))}
      </div>
      <div
        data-testid="app-runtime-canvas"
        className="min-h-[300px] border border-border rounded bg-bg-secondary/40 p-3"
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(12, minmax(0, 1fr))',
          gap: 8,
        }}
      >
        {instances.length === 0 && (
          <p
            style={{ gridColumn: '1 / -1' }}
            className="text-xs text-text-secondary italic text-center"
          >
            Empty app. Add components in Edit mode.
          </p>
        )}
        {instances.map((inst, idx) => (
          <RuntimeInstance
            key={inst.id}
            instance={inst}
            width={widths[idx]}
            state={stringState}
            onEvent={onEvent}
          />
        ))}
      </div>
      {message && (
        <p
          data-testid="app-runtime-message"
          className="text-xs font-mono text-text-secondary border border-border rounded px-2 py-1"
        >
          {message}
        </p>
      )}
    </div>
  );
}

function RuntimeInstance({
  instance,
  width,
  state,
  onEvent,
}: {
  instance: ComponentInstance;
  width: number;
  state: Record<string, string | number | boolean>;
  onEvent: (event: AppEvent) => void;
}) {
  const onClick = instance.events?.onClick;
  const handleClick = useCallback(() => {
    if (onClick) onEvent(onClick);
  }, [onClick, onEvent]);

  return (
    <div
      data-testid="app-runtime-instance"
      data-component-type={instance.componentType}
      style={{ gridColumn: `span ${width}` }}
      className="border border-border rounded bg-bg-primary p-3 text-text-primary"
    >
      <RuntimeComponent
        instance={instance}
        state={state}
        onClick={onClick ? handleClick : undefined}
      />
    </div>
  );
}

function RuntimeComponent({
  instance,
  state,
  onClick,
}: {
  instance: ComponentInstance;
  state: Record<string, string | number | boolean>;
  onClick?: () => void;
}) {
  const meta = COMPONENT_TYPES.find((c) => c.type === instance.componentType);
  switch (instance.componentType) {
    case 'text':
      return (
        <div
          data-testid="app-runtime-text"
          className="text-sm whitespace-pre-wrap"
        >
          {substituteVariables(
            String(instance.props.content ?? ''),
            state,
          )}
        </div>
      );
    case 'button':
      return (
        <button
          type="button"
          data-testid="app-runtime-button"
          onClick={onClick}
          className="px-3 py-1.5 rounded border border-border bg-accent-primary/20 text-sm text-text-primary hover:border-accent-primary"
        >
          {substituteVariables(
            String(instance.props.label ?? meta?.label ?? 'Button'),
            state,
          )}
        </button>
      );
    case 'table':
      return (
        <div className="text-xs text-text-secondary">
          <div className="font-mono">Table</div>
          <div className="truncate">
            {substituteVariables(
              String(instance.props.objectSet ?? '— bind ObjectSet —'),
              state,
            )}
          </div>
        </div>
      );
    case 'form':
      return (
        <div className="text-xs text-text-secondary">
          <div className="font-mono">Form</div>
          <div className="truncate">
            {substituteVariables(
              String(instance.props.actionType ?? '— bind ActionType —'),
              state,
            )}
          </div>
        </div>
      );
    case 'chart':
      return (
        <div className="text-xs text-text-secondary">
          <div className="font-mono">Chart</div>
          <div className="truncate">
            {substituteVariables(
              String(instance.props.title ?? 'Untitled chart'),
              state,
            )}
          </div>
        </div>
      );
    case 'objectCard':
      return (
        <div className="text-xs text-text-secondary">
          <div className="font-mono">Object Card</div>
          <div className="truncate">
            {substituteVariables(
              String(instance.props.objectType ?? '— bind ObjectType —'),
              state,
            )}
          </div>
        </div>
      );
  }
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

