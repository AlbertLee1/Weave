import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  createApp,
  getApp,
  listAppVersions,
  listApps,
  publishApp,
  rollbackApp,
  unpublishApp,
  updateApp,
  type App,
  type AppEvent,
  type AppVariable,
  type AppVariableType,
  type AppVersion,
} from '../../api/apps';
import { applyAction } from '../../api/actions';
import type { ActionResults } from '../../api/types';
import { useOntologyStore } from '../../stores/ontologyStore';
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
import {
  TABLE_FILTER_OPS,
  useAppObjectSet,
  type TableSortDirection,
} from './useAppObjectSet';
import { APP_TEMPLATES, type AppTemplate } from './templates';

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
  // US-397: viewport toggle in preview mode. 'desktop' lets the SPA's
  // own layout breakpoints decide; 'mobile' forces a narrow mobile-frame
  // container so authors can sanity-check single-column rendering
  // without resizing their browser. Edit-mode chrome is also responsive
  // via Tailwind sm/md/lg classes (declared on the relevant grid
  // wrappers), independent of this toggle.
  const [viewport, setViewport] = useState<'desktop' | 'mobile'>('desktop');
  // US-398: version-rollback drawer. `versions` is the newest-first list
  // returned by GET /api/v2/apps/{rid}/versions; `versionsOpen` controls
  // the side-drawer visibility; `versionsStatus` is the fetch lifecycle
  // (idle when no rid is bound, loading on first fetch, ready once
  // populated, error on a failed fetch). `rollbackBusy` carries the
  // version currently being rolled back so the corresponding button can
  // show a busy state while the request is in flight.
  const [versions, setVersions] = useState<AppVersion[]>([]);
  const [versionsOpen, setVersionsOpen] = useState(false);
  const [versionsStatus, setVersionsStatus] = useState<
    'idle' | 'loading' | 'ready' | 'error'
  >('idle');
  const [versionsError, setVersionsError] = useState<string | null>(null);
  const [rollbackBusy, setRollbackBusy] = useState<number | null>(null);
  const [rollbackError, setRollbackError] = useState<string | null>(null);

  // App publish lifecycle. `published` mirrors the App's current publish
  // pin (publishedVersion/publishedAt/publishedBy) and drives the
  // published-status badge + which control (Publish vs Unpublish) shows.
  // It's seeded from the loaded App row and updated optimistically off
  // the publishApp/unpublishApp responses so the badge flips without a
  // second getApp round-trip. `publishBusy` carries the in-flight verb
  // for the busy label; `publishError` surfaces a failed call inline.
  const [published, setPublished] = useState<{
    version: number;
    at?: string;
    by?: string;
  } | null>(null);
  const [publishBusy, setPublishBusy] = useState<
    'publish' | 'unpublish' | null
  >(null);
  const [publishError, setPublishError] = useState<string | null>(null);

  // US-399: built-in template picker. Auto-shown in new-App mode (no
  // rid prop) so authors land on a "pick a scaffold or start blank"
  // chooser instead of an empty canvas. Once dismissed (via "Start
  // blank") or a template is applied, the panel hides; the header
  // exposes a "Templates" toggle for re-opening it pre-Save.
  const [templatePickerVisible, setTemplatePickerVisible] = useState<boolean>(
    !rid,
  );

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
        setPublished(
          row.publishedVersion != null
            ? {
                version: row.publishedVersion,
                at: row.publishedAt,
                by: row.publishedBy,
              }
            : null,
        );
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

  const applyTemplate = useCallback((tpl: AppTemplate) => {
    const decoded = decodeLayout(tpl.layoutJson);
    setInstances(decoded.instances);
    setVariables(decoded.variables);
    setName(tpl.defaultAppName);
    setSelectedId(null);
    setTemplatePickerVisible(false);
  }, []);

  const dismissTemplatePicker = useCallback(() => {
    setTemplatePickerVisible(false);
  }, []);

  const openTemplatePicker = useCallback(() => {
    setTemplatePickerVisible(true);
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

  // US-398: load history rows on demand so the panel stays inert until
  // an author actually opens the drawer. `savedRid` may flip between
  // null (unsaved draft) and a real rid (after first Save), so this
  // also runs when savedRid changes while the drawer is open.
  const loadVersions = useCallback(async () => {
    if (!savedRid) {
      setVersions([]);
      setVersionsStatus('idle');
      return;
    }
    setVersionsStatus('loading');
    setVersionsError(null);
    try {
      const resp = await listAppVersions(savedRid);
      setVersions(resp.versions ?? []);
      setVersionsStatus('ready');
    } catch (err) {
      setVersionsError(err instanceof Error ? err.message : 'Load failed');
      setVersionsStatus('error');
    }
  }, [savedRid]);

  const openVersions = useCallback(() => {
    setVersionsOpen(true);
    void loadVersions();
  }, [loadVersions]);

  const closeVersions = useCallback(() => {
    setVersionsOpen(false);
    setRollbackError(null);
  }, []);

  const handleRollback = useCallback(
    async (version: number) => {
      if (!savedRid) return;
      setRollbackBusy(version);
      setRollbackError(null);
      try {
        const row = await rollbackApp(savedRid, version);
        // Decode the post-rollback layout back into editor state so the
        // canvas reflects the restored snapshot immediately — no second
        // network round-trip and no stale draft state. Bumping the
        // history list keeps the drawer in sync with the new live row.
        setName(row.name);
        const decoded = decodeLayout(row.layoutJson);
        setInstances(decoded.instances);
        setVariables(decoded.variables);
        setSelectedId(null);
        setSaveStatus('saved');
        setSaveError(null);
        await loadVersions();
      } catch (err) {
        setRollbackError(
          err instanceof Error ? err.message : 'Rollback failed',
        );
      } finally {
        setRollbackBusy(null);
      }
    },
    [savedRid, loadVersions],
  );

  // Publish pins the App's current version as the read-only published
  // snapshot. Owner-only on the server; a 403 surfaces as an inline
  // error. The returned PublishedAppView carries the freshly-pinned
  // version/at/by so the badge updates without a second getApp.
  const handlePublish = useCallback(async () => {
    if (!savedRid) return;
    setPublishBusy('publish');
    setPublishError(null);
    try {
      const view = await publishApp(savedRid);
      setPublished({
        version: view.publishedVersion,
        at: view.publishedAt,
        by: view.publishedBy,
      });
    } catch (err) {
      setPublishError(
        err instanceof Error ? err.message : 'Publish failed',
      );
    } finally {
      setPublishBusy(null);
    }
  }, [savedRid]);

  // Unpublish clears the publish pin. Owner-only; on success the badge
  // and the Unpublish control disappear, returning to the publishable
  // state.
  const handleUnpublish = useCallback(async () => {
    if (!savedRid) return;
    setPublishBusy('unpublish');
    setPublishError(null);
    try {
      await unpublishApp(savedRid);
      setPublished(null);
    } catch (err) {
      setPublishError(
        err instanceof Error ? err.message : 'Unpublish failed',
      );
    } finally {
      setPublishBusy(null);
    }
  }, [savedRid]);

  const canAddMore = instances.length < MAX_COLUMNS;
  const isPreview = mode === 'preview';

  const selectedOntology = useOntologyStore((s) => s.selectedOntology);

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
          // US-394: in preview mode runAction goes through the live
          // /api/v2/ontologies/{ontology}/actions/{action}/apply
          // endpoint and surfaces its edit counts (or error) in the
          // existing runtime message strip — that's the "toast" the AC
          // calls for. The ontology comes from the global ontology
          // store; preview from a page that hasn't selected one shows a
          // clear hint instead of POSTing to a `null` ontology.
          runAction: async (actionType, params) => {
            if (!actionType) {
              setRuntimeMessage('runAction: no ActionType configured');
              return;
            }
            if (!selectedOntology) {
              setRuntimeMessage(
                `runAction ${actionType} → no ontology selected`,
              );
              return;
            }
            try {
              const resp = await applyAction(selectedOntology, actionType, {
                parameters: params,
              });
              setRuntimeMessage(formatRunActionResult(actionType, resp.edits));
            } catch (err) {
              setRuntimeMessage(
                err instanceof Error
                  ? `runAction ${actionType} → error: ${err.message}`
                  : `runAction ${actionType} → error`,
              );
            }
          },
        });
      } catch (err) {
        setRuntimeMessage(
          err instanceof Error ? `error: ${err.message}` : 'event failed',
        );
      }
    },
    [variables, runtimeState, selectedOntology],
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
          {!savedRid && !templatePickerVisible && !isPreview && (
            <button
              type="button"
              data-testid="app-templates-toggle"
              onClick={openTemplatePicker}
              className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
            >
              Templates
            </button>
          )}
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
          {savedRid && (
            <button
              type="button"
              data-testid="app-versions-toggle"
              onClick={() => (versionsOpen ? closeVersions() : openVersions())}
              aria-expanded={versionsOpen}
              className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
            >
              {versionsOpen ? 'Hide Versions' : 'Versions'}
            </button>
          )}
          {savedRid && published === null && (
            <button
              type="button"
              data-testid="app-publish"
              onClick={() => {
                void handlePublish();
              }}
              disabled={publishBusy !== null}
              className="px-3 py-1.5 rounded border border-border bg-accent-primary/20 text-sm text-text-primary hover:border-accent-primary disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {publishBusy === 'publish' ? 'Publishing…' : 'Publish'}
            </button>
          )}
          {savedRid && published !== null && (
            <>
              <span
                data-testid="app-published-badge"
                data-version={String(published.version)}
                title={
                  published.by ? `Published by ${published.by}` : undefined
                }
                className="inline-flex items-center gap-1 px-2 py-1 rounded border border-accent-success/40 bg-accent-success/15 text-xs font-mono text-accent-success"
              >
                <span aria-hidden="true">●</span>
                Published v{published.version}
                {published.at ? ` · ${published.at}` : ''}
              </span>
              <button
                type="button"
                data-testid="app-unpublish"
                onClick={() => {
                  void handleUnpublish();
                }}
                disabled={publishBusy !== null}
                className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {publishBusy === 'unpublish' ? 'Unpublishing…' : 'Unpublish'}
              </button>
            </>
          )}
          {publishError && (
            <span
              data-testid="app-publish-error"
              className="text-xs font-mono text-accent-error"
            >
              {publishError}
            </span>
          )}
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

      {versionsOpen && (
        <VersionsPanel
          versions={versions}
          status={versionsStatus}
          loadError={versionsError}
          rollbackError={rollbackError}
          rollbackBusy={rollbackBusy}
          onRollback={(v) => {
            void handleRollback(v);
          }}
          onClose={closeVersions}
        />
      )}

      {!isPreview && !savedRid && templatePickerVisible && (
        <TemplatePickerPanel
          templates={APP_TEMPLATES}
          onSelect={applyTemplate}
          onDismiss={dismissTemplatePicker}
        />
      )}

      {isPreview ? (
        <RuntimeView
          instances={instances}
          variables={variables}
          state={runtimeState}
          onEvent={handleRuntimeEvent}
          message={runtimeMessage}
          viewport={viewport}
          onViewportChange={setViewport}
        />
      ) : (
        // US-397: Edit-mode chrome stacks vertically on phones (<lg) and
        // returns to its 3-7-2 sidebar layout on lg+. Sub-columns inside
        // are gated on lg: prefixes so the inner col-spans only kick in
        // once the outer grid splits.
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-4">
          <div className="lg:col-span-3 flex flex-col gap-4">
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

          <div className="lg:col-span-7">
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
      className="lg:col-span-3 border border-border rounded bg-bg-secondary/40 p-3"
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
        className="lg:col-span-2 border border-border rounded bg-bg-secondary/40 p-3"
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
      className="lg:col-span-2 border border-border rounded bg-bg-secondary/40 p-3"
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
        <RunActionEditor event={onClick} onChange={updateOnClick} />
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

interface RunActionEditorProps {
  event: Extract<AppEvent, { kind: 'runAction' }>;
  onChange: (next: AppEvent) => void;
}

// US-394: per-Button parameter mapping. Each row is a (paramName, value)
// pair where the value supports `{{var}}` template substitution against
// the runtime variable state. The wire shape on the event is
// `params: Record<string, string>`; we materialise it as an ordered
// array client-side so the row order is stable across edits and so an
// empty-key row doesn't collide with another empty-key row.
function RunActionEditor({ event, onChange }: RunActionEditorProps) {
  const paramEntries = useMemo(
    () => Object.entries(event.params ?? {}),
    [event.params],
  );

  const writeEntries = useCallback(
    (next: Array<[string, string]>) => {
      const out: Record<string, string> = {};
      for (const [k, v] of next) out[k] = v;
      onChange({ ...event, params: out });
    },
    [event, onChange],
  );

  const setKey = useCallback(
    (idx: number, key: string) => {
      const next = paramEntries.map<[string, string]>(([k, v], i) =>
        i === idx ? [key, v] : [k, v],
      );
      writeEntries(next);
    },
    [paramEntries, writeEntries],
  );

  const setValue = useCallback(
    (idx: number, value: string) => {
      const next = paramEntries.map<[string, string]>(([k, v], i) =>
        i === idx ? [k, value] : [k, v],
      );
      writeEntries(next);
    },
    [paramEntries, writeEntries],
  );

  const addParam = useCallback(() => {
    let candidate = '';
    let n = paramEntries.length;
    const taken = new Set(paramEntries.map(([k]) => k));
    while (taken.has(candidate)) {
      n += 1;
      candidate = `param${n}`;
    }
    writeEntries([...paramEntries, [candidate, '']]);
  }, [paramEntries, writeEntries]);

  const removeParam = useCallback(
    (idx: number) => {
      const next = paramEntries.filter((_, i) => i !== idx);
      writeEntries(next);
    },
    [paramEntries, writeEntries],
  );

  return (
    <div className="flex flex-col gap-1">
      <input
        type="text"
        aria-label="runAction ActionType"
        data-testid="app-event-onclick-runaction-actiontype"
        value={event.actionType}
        onChange={(e) => onChange({ ...event, actionType: e.target.value })}
        placeholder="action API name"
        className="px-2 py-0.5 rounded border border-border bg-bg-primary text-xs text-text-primary font-mono"
      />
      <div className="flex items-center justify-between mt-1">
        <span className="text-[10px] uppercase tracking-wide text-text-secondary">
          Parameter Mapping
        </span>
        <button
          type="button"
          data-testid="app-event-onclick-runaction-add-param"
          onClick={addParam}
          className="text-[10px] px-1.5 py-0.5 rounded border border-border bg-bg-primary text-text-primary hover:border-accent-primary"
        >
          + Add
        </button>
      </div>
      {paramEntries.length === 0 ? (
        <p
          data-testid="app-event-onclick-runaction-params-empty"
          className="text-[10px] text-text-secondary italic"
        >
          No params. Map ActionType inputs to variables via {'{{varName}}'}.
        </p>
      ) : (
        <ul className="flex flex-col gap-1">
          {paramEntries.map(([key, value], idx) => (
            <li
              key={idx}
              data-testid={`app-event-onclick-runaction-param-row-${idx}`}
              className="flex items-center gap-1"
            >
              <input
                type="text"
                aria-label={`Parameter name ${idx + 1}`}
                data-testid={`app-event-onclick-runaction-param-key-${idx}`}
                value={key}
                onChange={(e) => setKey(idx, e.target.value)}
                placeholder="paramName"
                className="flex-1 px-1.5 py-0.5 rounded border border-border bg-bg-primary text-[11px] text-text-primary font-mono"
              />
              <input
                type="text"
                aria-label={`Parameter value ${idx + 1}`}
                data-testid={`app-event-onclick-runaction-param-value-${idx}`}
                value={value}
                onChange={(e) => setValue(idx, e.target.value)}
                placeholder="{{var}} or literal"
                className="flex-1 px-1.5 py-0.5 rounded border border-border bg-bg-primary text-[11px] text-text-primary font-mono"
              />
              <button
                type="button"
                data-testid={`app-event-onclick-runaction-param-remove-${idx}`}
                aria-label={`Remove parameter ${idx + 1}`}
                onClick={() => removeParam(idx)}
                className="text-text-secondary hover:text-accent-error text-xs px-1"
              >
                ×
              </button>
            </li>
          ))}
        </ul>
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
  viewport: 'desktop' | 'mobile';
  onViewportChange: (next: 'desktop' | 'mobile') => void;
}

// US-397: Preview-mode runtime view with viewport toggle.
//
// Two surfaces are responsive:
//
// 1. The runtime canvas itself uses Tailwind's `sm:` breakpoint
//    (640px) — below it the inline grid collapses to a single
//    column so each component fills the row, above it the 12-col
//    grid returns and authored widths apply. The data-attributes
//    (`data-cols`) flip in lockstep so tests / E2E can assert the
//    breakpoint behaviour without measuring CSS.
//
// 2. The viewport toggle is an explicit override that sets
//    `data-viewport=mobile` on the frame, wraps the canvas in a
//    375px-wide container, and forces the single-column grid even
//    on a desktop browser. This lets authors sanity-check phone
//    rendering without resizing the window.
function RuntimeView({
  instances,
  variables,
  state,
  onEvent,
  message,
  viewport,
  onViewportChange,
}: RuntimeViewProps) {
  const stringState = useMemo(() => {
    const out: Record<string, string | number | boolean> = {};
    for (const v of variables) {
      out[v.name] = state[v.name] ?? '';
    }
    return out;
  }, [variables, state]);

  const widths = useMemo(() => distributeWidths(instances.length), [instances]);
  const isMobileFrame = viewport === 'mobile';
  // Forced mobile drops the 12-col tracks for a single column; default
  // ('desktop') uses Tailwind responsive classes so the SPA's natural
  // breakpoint behaviour wins.
  const canvasGridClass = isMobileFrame
    ? 'grid grid-cols-1 gap-2'
    : 'grid grid-cols-1 sm:grid-cols-12 gap-2';

  return (
    <div
      data-testid="app-runtime-view"
      data-viewport={viewport}
      className="flex flex-col gap-3"
    >
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
        data-testid="app-runtime-viewport-toolbar"
        className="flex items-center gap-2 text-xs text-text-secondary"
      >
        <span className="font-mono">Viewport:</span>
        <div className="inline-flex rounded border border-border overflow-hidden">
          <button
            type="button"
            data-testid="app-viewport-desktop"
            data-active={viewport === 'desktop' ? 'true' : 'false'}
            onClick={() => onViewportChange('desktop')}
            className={`px-2 py-0.5 text-xs ${
              viewport === 'desktop'
                ? 'bg-accent-primary/20 text-text-primary'
                : 'bg-bg-secondary text-text-secondary hover:text-text-primary'
            }`}
          >
            Desktop
          </button>
          <button
            type="button"
            data-testid="app-viewport-mobile"
            data-active={viewport === 'mobile' ? 'true' : 'false'}
            onClick={() => onViewportChange('mobile')}
            className={`px-2 py-0.5 text-xs border-l border-border ${
              viewport === 'mobile'
                ? 'bg-accent-primary/20 text-text-primary'
                : 'bg-bg-secondary text-text-secondary hover:text-text-primary'
            }`}
          >
            Mobile
          </button>
        </div>
      </div>
      <div
        data-testid="app-runtime-frame"
        data-frame-mode={viewport}
        className={
          isMobileFrame
            ? 'mx-auto w-full max-w-[375px] border border-border rounded shadow-md bg-bg-primary'
            : 'w-full'
        }
      >
        <div
          data-testid="app-runtime-canvas"
          data-cols={isMobileFrame ? '1' : '12'}
          className={`min-h-[300px] border border-border rounded bg-bg-secondary/40 p-3 ${canvasGridClass}`}
        >
          {instances.length === 0 && (
            <p
              className={`text-xs text-text-secondary italic text-center ${
                isMobileFrame ? '' : 'sm:col-span-12'
              }`}
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
              forceSingleColumn={isMobileFrame}
            />
          ))}
        </div>
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
  forceSingleColumn,
}: {
  instance: ComponentInstance;
  width: number;
  state: Record<string, string | number | boolean>;
  onEvent: (event: AppEvent) => void;
  forceSingleColumn?: boolean;
}) {
  const onClick = instance.events?.onClick;
  const handleClick = useCallback(() => {
    if (onClick) onEvent(onClick);
  }, [onClick, onEvent]);

  // US-397: in mobile-frame mode the parent grid only has one column,
  // so authored widths collapse to span-1 (the full row). In desktop
  // mode the parent uses `grid-cols-1 sm:grid-cols-12` — `gridColumn:
  // span N` is clamped by the browser to the available track count, so
  // <sm everything still occupies the single column and ≥sm authored
  // widths apply.
  const style = forceSingleColumn
    ? undefined
    : { gridColumn: `span ${width}` };

  return (
    <div
      data-testid="app-runtime-instance"
      data-component-type={instance.componentType}
      data-instance-width={width}
      data-mobile-frame={forceSingleColumn ? 'true' : 'false'}
      style={style}
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
      // US-395: Table runtime binds an ObjectSet RID / ObjectType to a
      // live data fetch with pagination + sorting + filter.
      return <RuntimeTable instance={instance} state={state} />;
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

interface RuntimeTableProps {
  instance: ComponentInstance;
  state: Record<string, string | number | boolean>;
}

// US-395: live ObjectSet binding for the Table component. The hook
// resolves objectSet (RID or base ObjectType API name) + columns +
// pageSize/sort/filter against the runtime variable state, and exposes
// pageIndex / hasNextPage / goNext / goPrev plus a sortOverride state
// that flips when the user clicks a column header.
function RuntimeTable({ instance, state }: RuntimeTableProps) {
  const ontology = useOntologyStore((s) => s.selectedOntology);
  const [sortOverride, setSortOverride] = useState<{
    field: string;
    direction: TableSortDirection;
  } | null>(null);

  const view = useAppObjectSet({
    ontologyApiName: ontology,
    props: instance.props,
    state,
    sortOverride,
  });

  const columns = view.resolved.columns ?? [];
  const sortField =
    sortOverride?.field ?? view.resolved.orderByField ?? null;
  const sortDirection =
    sortOverride?.direction ?? view.resolved.orderByDirection ?? 'asc';

  const onHeaderClick = useCallback(
    (col: string) => {
      setSortOverride((prev) => {
        if (prev?.field === col) {
          // Clicking the same column toggles direction.
          return {
            field: col,
            direction: prev.direction === 'asc' ? 'desc' : 'asc',
          };
        }
        return { field: col, direction: 'asc' };
      });
    },
    [],
  );

  if (!ontology) {
    return (
      <div
        data-testid="app-runtime-table"
        data-table-state="no-ontology"
        className="text-xs text-text-secondary"
      >
        Bind an ontology before previewing the Table.
      </div>
    );
  }

  if (!view.resolved.objectSet || columns.length === 0) {
    return (
      <div
        data-testid="app-runtime-table"
        data-table-state="unbound"
        className="text-xs text-text-secondary"
      >
        Configure the Table with an ObjectSet and at least one column.
      </div>
    );
  }

  return (
    <div
      data-testid="app-runtime-table"
      data-table-state={view.loading ? 'loading' : view.error ? 'error' : 'ok'}
      data-page-index={view.pageIndex}
      data-page-size={view.pageSize}
      className="flex flex-col gap-2"
    >
      <div className="overflow-auto border border-border rounded">
        <table className="w-full text-xs text-left">
          <thead className="bg-bg-secondary/60 text-text-secondary">
            <tr>
              {columns.map((col) => {
                const isSorted = sortField === col;
                return (
                  <th
                    key={col}
                    data-testid={`app-runtime-table-header-${col}`}
                    data-sort-active={isSorted ? 'true' : 'false'}
                    data-sort-direction={isSorted ? sortDirection : ''}
                    onClick={() => onHeaderClick(col)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        onHeaderClick(col);
                      }
                    }}
                    role="button"
                    tabIndex={0}
                    className="px-2 py-1 cursor-pointer select-none hover:text-text-primary border-b border-border font-mono"
                  >
                    {col}
                    {isSorted && (
                      <span className="ml-1 text-accent-primary">
                        {sortDirection === 'asc' ? '▲' : '▼'}
                      </span>
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {view.data.length === 0 ? (
              <tr>
                <td
                  data-testid="app-runtime-table-empty"
                  colSpan={Math.max(columns.length, 1)}
                  className="px-2 py-3 text-center text-text-secondary italic"
                >
                  {view.loading ? 'Loading…' : 'No rows'}
                </td>
              </tr>
            ) : (
              view.data.map((row, idx) => (
                <tr
                  key={
                    typeof row.__primaryKey === 'string' ||
                    typeof row.__primaryKey === 'number'
                      ? String(row.__primaryKey)
                      : idx
                  }
                  data-testid="app-runtime-table-row"
                  className="border-b border-border last:border-b-0"
                >
                  {columns.map((col) => (
                    <td
                      key={col}
                      data-testid={`app-runtime-table-cell-${col}`}
                      className="px-2 py-1 text-text-primary truncate max-w-[200px]"
                    >
                      {formatCellValue(row[col])}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <div
        data-testid="app-runtime-table-pagination"
        className="flex items-center gap-2 text-[11px] text-text-secondary font-mono"
      >
        <button
          type="button"
          data-testid="app-runtime-table-prev"
          onClick={view.goPrev}
          disabled={!view.hasPrevPage || view.loading}
          className="px-2 py-0.5 rounded border border-border bg-bg-primary text-text-primary disabled:opacity-50 disabled:cursor-not-allowed hover:border-accent-primary"
        >
          ← Prev
        </button>
        <span data-testid="app-runtime-table-page-label">
          Page {view.pageIndex + 1}
        </span>
        <button
          type="button"
          data-testid="app-runtime-table-next"
          onClick={view.goNext}
          disabled={!view.hasNextPage || view.loading}
          className="px-2 py-0.5 rounded border border-border bg-bg-primary text-text-primary disabled:opacity-50 disabled:cursor-not-allowed hover:border-accent-primary"
        >
          Next →
        </button>
        {view.totalCount !== undefined && (
          <span data-testid="app-runtime-table-total">
            Total {view.totalCount}
          </span>
        )}
        {view.error ? (
          <span
            data-testid="app-runtime-table-error"
            className="text-accent-error"
          >
            {view.error instanceof Error ? view.error.message : 'fetch failed'}
          </span>
        ) : null}
      </div>
    </div>
  );
}

function formatCellValue(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string' || typeof value === 'number') return String(value);
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
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
            label="ObjectSet (RID or ObjectType)"
            testId="prop-table-objectSet"
            value={String(instance.props.objectSet ?? '')}
            onChange={(v) => onPatch({ objectSet: v })}
            placeholder="ri.objectSet… or Customer"
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
          <label className="flex flex-col gap-1 text-xs text-text-secondary">
            Page Size
            <input
              type="number"
              min={1}
              max={500}
              data-testid="prop-table-pageSize"
              value={
                typeof instance.props.pageSize === 'number' ||
                typeof instance.props.pageSize === 'string'
                  ? String(instance.props.pageSize)
                  : '25'
              }
              onChange={(e) => {
                const v = Number(e.target.value);
                onPatch({ pageSize: Number.isFinite(v) && v > 0 ? v : 25 });
              }}
              className="px-2 py-1 rounded border border-border bg-bg-primary text-sm text-text-primary"
            />
          </label>
          <PropField
            label="Default Sort Field"
            testId="prop-table-orderByField"
            value={String(instance.props.orderByField ?? '')}
            onChange={(v) => onPatch({ orderByField: v })}
            placeholder="(none)"
          />
          <label className="flex flex-col gap-1 text-xs text-text-secondary">
            Default Sort Direction
            <select
              data-testid="prop-table-orderByDirection"
              value={String(instance.props.orderByDirection ?? 'asc')}
              onChange={(e) => onPatch({ orderByDirection: e.target.value })}
              className="px-2 py-1 rounded border border-border bg-bg-primary text-sm text-text-primary"
            >
              <option value="asc">asc</option>
              <option value="desc">desc</option>
            </select>
          </label>
          <PropField
            label="Filter Field"
            testId="prop-table-filterField"
            value={String(instance.props.filterField ?? '')}
            onChange={(v) => onPatch({ filterField: v })}
            placeholder="(none)"
          />
          <label className="flex flex-col gap-1 text-xs text-text-secondary">
            Filter Op
            <select
              data-testid="prop-table-filterOp"
              value={String(instance.props.filterOp ?? 'eq')}
              onChange={(e) => onPatch({ filterOp: e.target.value })}
              className="px-2 py-1 rounded border border-border bg-bg-primary text-sm text-text-primary"
            >
              {TABLE_FILTER_OPS.map((op) => (
                <option key={op} value={op}>
                  {op}
                </option>
              ))}
            </select>
          </label>
          <PropField
            label="Filter Value"
            testId="prop-table-filterValue"
            value={String(instance.props.filterValue ?? '')}
            onChange={(v) => onPatch({ filterValue: v })}
            placeholder="literal or {{var}}"
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

// formatRunActionResult renders an ActionResults envelope as a one-line
// summary suitable for the runtime message strip. Only non-zero counts
// are included so the typical "1 edit" call doesn't drag along five
// trailing zeros. When the server returned no edits envelope at all
// (validate-only / no-edits action), the summary collapses to a bare
// "ok".
function formatRunActionResult(
  actionType: string,
  edits: ActionResults | undefined,
): string {
  const segments: string[] = [];
  if (edits) {
    if (edits.addedObjectCount) segments.push(`+${edits.addedObjectCount}`);
    if (edits.modifiedObjectCount) segments.push(`~${edits.modifiedObjectCount}`);
    if (edits.deletedObjectCount) segments.push(`-${edits.deletedObjectCount}`);
    if (edits.addedLinksCount) segments.push(`+${edits.addedLinksCount} links`);
    if (edits.deletedLinksCount)
      segments.push(`-${edits.deletedLinksCount} links`);
  }
  const summary = segments.length === 0 ? 'ok' : segments.join(' ');
  return `runAction ${actionType} → ${summary}`;
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

interface VersionsPanelProps {
  versions: AppVersion[];
  status: 'idle' | 'loading' | 'ready' | 'error';
  loadError: string | null;
  rollbackError: string | null;
  rollbackBusy: number | null;
  onRollback: (version: number) => void;
  onClose: () => void;
}

// US-398: Versions drawer. Lists every history snapshot (newest first)
// and exposes a one-click rollback button per row. The drawer renders
// above the editor canvas so it's visible regardless of which side
// rails the lg breakpoint has collapsed.
function VersionsPanel({
  versions,
  status,
  loadError,
  rollbackError,
  rollbackBusy,
  onRollback,
  onClose,
}: VersionsPanelProps) {
  const liveVersion = versions.length > 0 ? versions[0].version : undefined;
  return (
    <section
      data-testid="app-versions-panel"
      data-status={status}
      className="mb-4 border border-border rounded bg-bg-secondary/40 p-3"
    >
      <div className="flex items-center justify-between mb-2">
        <h2 className="text-sm font-mono font-medium text-text-secondary">
          Version History
        </h2>
        <button
          type="button"
          data-testid="app-versions-close"
          onClick={onClose}
          className="text-xs text-text-secondary hover:text-text-primary"
          aria-label="Close version history"
        >
          ×
        </button>
      </div>
      {status === 'loading' && (
        <p
          data-testid="app-versions-loading"
          className="text-xs text-text-secondary italic"
        >
          Loading versions…
        </p>
      )}
      {status === 'error' && (
        <p
          data-testid="app-versions-error"
          className="text-xs text-status-error"
        >
          {loadError ?? 'Failed to load versions.'}
        </p>
      )}
      {status === 'ready' && versions.length === 0 && (
        <p
          data-testid="app-versions-empty"
          className="text-xs text-text-secondary italic"
        >
          No history rows yet.
        </p>
      )}
      {status === 'ready' && versions.length > 0 && (
        <ul
          data-testid="app-versions-list"
          className="flex flex-col gap-1 max-h-72 overflow-auto"
        >
          {versions.map((v) => {
            const isLive = v.version === liveVersion;
            const busy = rollbackBusy === v.version;
            return (
              <li
                key={v.version}
                data-testid={`app-versions-row-${v.version}`}
                data-version={v.version}
                data-live={isLive ? 'true' : 'false'}
                className="flex items-center justify-between gap-2 px-2 py-1.5 rounded border border-border bg-bg-primary"
              >
                <div className="flex flex-col">
                  <span className="text-sm text-text-primary font-mono">
                    v{v.version}
                    {isLive && (
                      <span
                        data-testid={`app-versions-live-badge-${v.version}`}
                        className="ml-2 px-1.5 py-0.5 rounded text-[10px] uppercase tracking-wide bg-accent-primary/20 text-accent-primary"
                      >
                        live
                      </span>
                    )}
                  </span>
                  <span className="text-xs text-text-secondary truncate">
                    {v.name} ·{' '}
                    {v.createdAt ? new Date(v.createdAt).toLocaleString() : '—'}
                    {v.createdBy ? ` · ${v.createdBy}` : ''}
                  </span>
                </div>
                <button
                  type="button"
                  data-testid={`app-versions-rollback-${v.version}`}
                  onClick={() => onRollback(v.version)}
                  disabled={busy || isLive || rollbackBusy !== null}
                  className="px-2 py-1 rounded border border-border bg-bg-secondary text-xs text-text-primary hover:border-accent-primary disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {busy ? 'Rolling back…' : isLive ? 'Live' : 'Rollback'}
                </button>
              </li>
            );
          })}
        </ul>
      )}
      {rollbackError && (
        <p
          data-testid="app-versions-rollback-error"
          className="mt-2 text-xs text-status-error"
        >
          {rollbackError}
        </p>
      )}
    </section>
  );
}

interface TemplatePickerPanelProps {
  templates: readonly AppTemplate[];
  onSelect: (template: AppTemplate) => void;
  onDismiss: () => void;
}

// US-399: Template picker. Auto-shown in new-App mode so authors can
// land on a sensible scaffold instead of a blank canvas. The panel is
// dismissible (Start blank), and the "Use template" buttons populate
// instances + variables + the App name from the selected template.
// Once an App has been saved (savedRid is set) the panel is hidden
// permanently — templates are a fresh-start affordance, not an undo
// for an already-saved layout.
function TemplatePickerPanel({
  templates,
  onSelect,
  onDismiss,
}: TemplatePickerPanelProps) {
  return (
    <section
      data-testid="app-template-picker"
      data-template-count={templates.length}
      className="mb-4 border border-border rounded bg-bg-secondary/40 p-3"
    >
      <div className="flex items-center justify-between mb-2">
        <div>
          <h2 className="text-sm font-mono font-medium text-text-secondary">
            Start from a template
          </h2>
          <p className="text-xs text-text-secondary mt-0.5">
            Pick a scaffold to populate the canvas, or start blank.
          </p>
        </div>
        <button
          type="button"
          data-testid="app-template-picker-blank"
          onClick={onDismiss}
          className="px-2 py-1 rounded border border-border bg-bg-secondary text-xs text-text-primary hover:border-accent-primary"
        >
          Start blank
        </button>
      </div>
      <ul
        data-testid="app-template-picker-list"
        className="grid grid-cols-1 md:grid-cols-3 gap-2"
      >
        {templates.map((tpl) => (
          <li
            key={tpl.id}
            data-testid={`app-template-card-${tpl.id}`}
            data-template-id={tpl.id}
            className="border border-border rounded p-3 bg-bg-primary flex flex-col gap-2"
          >
            <div className="flex flex-col">
              <span className="text-sm font-medium text-text-primary">
                {tpl.name}
              </span>
              <span className="text-xs text-text-secondary mt-1">
                {tpl.description}
              </span>
            </div>
            <button
              type="button"
              data-testid={`app-template-use-${tpl.id}`}
              onClick={() => onSelect(tpl)}
              className="self-start px-2 py-1 rounded border border-border bg-accent-primary/20 text-xs text-text-primary hover:border-accent-primary"
            >
              Use template
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

