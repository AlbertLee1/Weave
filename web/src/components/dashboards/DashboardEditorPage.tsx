import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import * as React from 'react';
import {
  createDashboard,
  deleteDashboard,
  duplicateDashboard,
  getDashboard,
  listDashboards,
  updateDashboard,
  type Dashboard,
} from '../../api/dashboards';
import { ChartView } from './widgets/ChartView';
import { StatView } from './widgets/StatView';
import { MapView } from './widgets/MapView';
import type {
  Widget,
  WidgetType,
  TextWidget,
  ChartWidget,
  TableWidget,
  StatWidget,
  MapWidget,
  ChartType,
  StatTrend,
  WidgetDataSource,
} from './widgets/types';

// US-327 Dashboard Editor + US-328 Widget Library + US-329 Save/Share
// + US-428 Widget Library Completion (real chart/stat/map renderers and
// ObjectSet / Aggregation data binding — see widgets/ subfolder).
//
// PRD asked for react-grid-layout; the lib isn't installed and depends on
// react-resizable + react-draggable (~80KB) which would be the only consumer.
// Following the US-324/US-325/US-326 pattern (and learning #209 in
// progress.txt) — when the proposed lib is uninstalled and would add a heavy
// dep that's only used here, we ship a small native HTML5 DnD layout instead.
//
// Same logic for US-428: PRD asked for echarts; the codebase already
// standardised on uplot for time-series and inline SVG for small charts
// (see TimeSeriesChart + the dashboards/widgets/ChartView.tsx renderer).
// Adding echarts (~500 KB) for 8-bucket categorical charts would dwarf
// everything else in the bundle, so the new ChartView ships hand-rolled
// SVG line / bar / pie renderers instead.

const COLUMN_COUNT = 12;
const DEFAULT_W = 4;
const DEFAULT_H = 2;
const ROW_HEIGHT_PX = 60;
const DND_MIME = 'application/x-weave-dashboard';

type DragKind = 'move' | 'resize';

interface DragPayload {
  id: string;
  kind: DragKind;
  // Offset within the widget in pixels at drag-start, used so the widget snaps
  // under the cursor instead of jumping its top-left to the cursor position.
  offsetX: number;
  offsetY: number;
}

let widgetIdCounter = 0;
function nextWidgetId(): string {
  widgetIdCounter += 1;
  return `widget-${widgetIdCounter}-${Date.now().toString(36)}`;
}

function clamp(n: number, min: number, max: number): number {
  if (n < min) return min;
  if (n > max) return max;
  return n;
}

function findFirstFreeRow(widgets: Widget[]): number {
  if (widgets.length === 0) return 0;
  return widgets.reduce((acc, w) => Math.max(acc, w.y + w.h), 0);
}

function makeWidget(type: WidgetType, y: number): Widget {
  const base = {
    id: nextWidgetId(),
    x: 0,
    y,
    w: DEFAULT_W,
    h: DEFAULT_H,
  };
  switch (type) {
    case 'text':
      return { ...base, type: 'text', title: 'New Widget', content: '' };
    case 'chart':
      return {
        ...base,
        type: 'chart',
        title: 'Chart',
        chartType: 'bar',
        values: [12, 19, 8, 15, 22],
      };
    case 'table':
      return {
        ...base,
        type: 'table',
        title: 'Table',
        columns: ['Name', 'Value'],
        rows: [
          ['Alpha', '1'],
          ['Beta', '2'],
        ],
      };
    case 'stat':
      return {
        ...base,
        type: 'stat',
        title: 'Stat',
        value: '0',
        label: 'Metric',
        trend: 'neutral',
      };
    case 'map':
      return {
        ...base,
        type: 'map',
        title: 'Map',
        latitude: 0,
        longitude: 0,
        zoom: 2,
      };
  }
}

function parseValuesInput(raw: string): number[] {
  return raw
    .split(/[,\n]/)
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
    .map((s) => Number(s))
    .filter((n) => Number.isFinite(n));
}

function parseRowsInput(raw: string): string[][] {
  return raw
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((line) => line.split(',').map((c) => c.trim()));
}

function parseColumnsInput(raw: string): string[] {
  return raw
    .split(',')
    .map((c) => c.trim())
    .filter((c) => c.length > 0);
}

export interface DashboardEditorPageProps {
  // When present, the editor loads the matching dashboard on mount and
  // routes Save through PUT. When absent, the editor starts empty and
  // routes Save through POST → onSaved.
  id?: string;
  // Fired with the freshly-created id after a successful POST. The
  // route wrapper navigates to /dashboards/{id}; tests can observe the
  // call directly. No-op by default.
  onSaved?: (id: string) => void;
}

export function DashboardEditorPage({
  id,
  onSaved,
}: DashboardEditorPageProps = {}) {
  const [widgets, setWidgets] = useState<Widget[]>([]);
  const [configuringId, setConfiguringId] = useState<string | null>(null);
  const [name, setName] = useState('Untitled Dashboard');
  const [savedId, setSavedId] = useState<string | null>(id ?? null);
  const [isPublic, setIsPublic] = useState(false);
  const [savedDashboards, setSavedDashboards] = useState<Dashboard[]>([]);
  const [saveStatus, setSaveStatus] = useState<
    'idle' | 'saving' | 'saved' | 'error'
  >('idle');
  const [shareStatus, setShareStatus] = useState<'idle' | 'copied'>('idle');
  const [duplicateStatus, setDuplicateStatus] = useState<
    'idle' | 'duplicating' | 'error'
  >('idle');
  const [deleteStatus, setDeleteStatus] = useState<
    'idle' | 'deleting' | 'error'
  >('idle');
  // Gate the destructive DELETE behind an explicit confirm step rendered
  // inline in the toolbar (rather than window.confirm, which can't be
  // observed deterministically in tests).
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  // Focus restore for the confirm dialog (a11y): the Delete button swaps
  // itself out for the dialog when opened, so the dialog can't capture it as
  // a stable trigger on mount. Instead the parent — which owns the button's
  // lifecycle — restores focus to the freshly re-rendered Delete button when
  // the dialog closes (confirmingDelete true → false), so keyboard users land
  // back on the control they activated.
  const deleteButtonRef = useRef<HTMLButtonElement>(null);
  const prevConfirmingDelete = useRef(confirmingDelete);
  useEffect(() => {
    const closed = prevConfirmingDelete.current && !confirmingDelete;
    prevConfirmingDelete.current = confirmingDelete;
    // Only restore focus while the editor still shows a Delete button. After a
    // successful delete the editor resets to an unsaved state (no savedId →
    // no Delete button), so there is nothing to focus.
    if (closed && savedId) {
      deleteButtonRef.current?.focus();
    }
  }, [confirmingDelete, savedId]);

  // Load on mount when an id was passed in (the SPA's /dashboards/:id
  // route wrapper supplies it). Failures fall back to the empty editor
  // — the SPA shows a generic toast and the user can always start over.
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    getDashboard(id)
      .then((d) => {
        if (cancelled) return;
        setName(d.name);
        setIsPublic(d.isPublic);
        const loaded = Array.isArray(d.definition?.widgets)
          ? (d.definition.widgets as Widget[])
          : [];
        setWidgets(loaded);
        setSavedId(d.id);
      })
      .catch(() => {
        // Silent fallback to ephemeral editor — the wider page chrome
        // is responsible for surfacing a toast in production.
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  // Eagerly fetch the caller's saved dashboards so the "Load" picker is
  // populated on first render. 404 (degraded mode) is treated as "no
  // saved dashboards" — same shape as the SavedSearches panel.
  useEffect(() => {
    let cancelled = false;
    listDashboards()
      .then((resp) => {
        if (cancelled) return;
        setSavedDashboards(resp.dashboards ?? []);
      })
      .catch(() => {
        if (cancelled) return;
        setSavedDashboards([]);
      });
    return () => {
      cancelled = true;
    };
  }, [savedId]);

  const handleSave = useCallback(async () => {
    setSaveStatus('saving');
    try {
      const definition = { widgets };
      if (savedId) {
        const updated = await updateDashboard({
          id: savedId,
          name,
          definition,
          isPublic,
        });
        setIsPublic(updated.isPublic);
      } else {
        const created = await createDashboard({
          name,
          definition,
          isPublic,
        });
        setSavedId(created.id);
        setIsPublic(created.isPublic);
        onSaved?.(created.id);
      }
      setSaveStatus('saved');
    } catch {
      setSaveStatus('error');
    }
  }, [name, widgets, savedId, isPublic, onSaved]);

  const handleShare = useCallback(async () => {
    if (!savedId) return;
    const url = `${window.location.origin}/dashboards/${savedId}`;
    try {
      // navigator.clipboard isn't always available in tests / older
      // browsers; the visible href on the share button is the reliable
      // fallback for a manual copy.
      await navigator.clipboard?.writeText(url);
      setShareStatus('copied');
      window.setTimeout(() => setShareStatus('idle'), 1500);
    } catch {
      // Even if writeText fails (e.g. insecure context), the link is
      // visible to the user as the share button's href.
    }
  }, [savedId]);

  const handleDuplicate = useCallback(async () => {
    if (!savedId) return;
    setDuplicateStatus('duplicating');
    try {
      const dup = await duplicateDashboard(savedId);
      // Load the copy by navigating to its id — the same path the Load
      // picker and Save-new flow use to swap the editor's mounted row.
      onSaved?.(dup.id);
      setDuplicateStatus('idle');
    } catch {
      setDuplicateStatus('error');
    }
  }, [savedId, onSaved]);

  const handleDelete = useCallback(async () => {
    if (!savedId) return;
    setDeleteStatus('deleting');
    try {
      await deleteDashboard(savedId);
      // Reset the editor back to a fresh, unsaved state — the deleted row no
      // longer exists, so clearing savedId/name/widgets matches what a brand
      // new editor shows.
      setConfirmingDelete(false);
      setSavedId(null);
      setName('Untitled Dashboard');
      setIsPublic(false);
      setWidgets([]);
      setConfiguringId(null);
      setDeleteStatus('idle');
      setSaveStatus('idle');
      // Tell the route wrapper to leave the now-deleted dashboard's URL. The
      // empty id signals "navigate away from this dashboard" (the Save-new /
      // Load / Duplicate flows pass a real id to mount a row instead).
      onSaved?.('');
    } catch {
      setDeleteStatus('error');
    }
  }, [savedId, onSaved]);

  const shareUrl = savedId
    ? `${window.location.origin}/dashboards/${savedId}`
    : '';

  const addWidget = useCallback((type: WidgetType) => {
    setWidgets((prev) => [...prev, makeWidget(type, findFirstFreeRow(prev))]);
  }, []);

  const removeWidget = useCallback((id: string) => {
    setWidgets((prev) => prev.filter((w) => w.id !== id));
    setConfiguringId((cur) => (cur === id ? null : cur));
  }, []);

  const patchWidget = useCallback((id: string, patch: Partial<Widget>) => {
    setWidgets((prev) =>
      prev.map((w) => (w.id === id ? ({ ...w, ...patch } as Widget) : w)),
    );
  }, []);

  const moveWidget = useCallback((id: string, x: number, y: number) => {
    setWidgets((prev) =>
      prev.map((w) => {
        if (w.id !== id) return w;
        const maxX = Math.max(0, COLUMN_COUNT - w.w);
        return { ...w, x: clamp(x, 0, maxX), y: Math.max(0, y) };
      }),
    );
  }, []);

  const resizeWidget = useCallback((id: string, w: number, h: number) => {
    setWidgets((prev) =>
      prev.map((widget) => {
        if (widget.id !== id) return widget;
        const maxW = Math.max(1, COLUMN_COUNT - widget.x);
        return {
          ...widget,
          w: clamp(w, 1, maxW),
          h: Math.max(1, h),
        };
      }),
    );
  }, []);

  const handleDragStart = useCallback(
    (e: React.DragEvent, payload: DragPayload) => {
      e.dataTransfer.setData(DND_MIME, JSON.stringify(payload));
      e.dataTransfer.effectAllowed = 'move';
      e.stopPropagation();
    },
    [],
  );

  const handleGridDragOver = useCallback((e: React.DragEvent) => {
    if (e.dataTransfer.types.includes(DND_MIME)) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
    }
  }, []);

  const handleGridDrop = useCallback(
    (e: React.DragEvent) => {
      const raw = e.dataTransfer.getData(DND_MIME);
      if (!raw) return;
      e.preventDefault();
      let payload: DragPayload;
      try {
        payload = JSON.parse(raw) as DragPayload;
      } catch {
        return;
      }
      const rect = e.currentTarget.getBoundingClientRect();
      const cellWidth = rect.width / COLUMN_COUNT;
      const relX = e.clientX - rect.left;
      const relY = e.clientY - rect.top;
      if (payload.kind === 'move') {
        // Snap top-left of widget under cursor (cursor position - drag offset).
        const cellX = Math.floor((relX - payload.offsetX) / cellWidth);
        const cellY = Math.floor((relY - payload.offsetY) / ROW_HEIGHT_PX);
        moveWidget(payload.id, cellX, cellY);
      } else if (payload.kind === 'resize') {
        const widget = widgets.find((w) => w.id === payload.id);
        if (!widget) return;
        // Resize handle anchor is bottom-right; new w/h = (cursor - widget origin) / cell.
        const widgetOriginX = widget.x * cellWidth;
        const widgetOriginY = widget.y * ROW_HEIGHT_PX;
        const newW = Math.round((relX - widgetOriginX) / cellWidth);
        const newH = Math.round((relY - widgetOriginY) / ROW_HEIGHT_PX);
        resizeWidget(payload.id, newW, newH);
      }
    },
    [moveWidget, resizeWidget, widgets],
  );

  const totalRows = useMemo(() => {
    if (widgets.length === 0) return 8;
    return Math.max(8, findFirstFreeRow(widgets) + 2);
  }, [widgets]);

  return (
    <div
      data-testid="dashboard-editor-page"
      data-dashboard-id={savedId ?? ''}
      className="min-h-full p-6"
    >
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
            Dashboard Editor
          </h1>
          <p className="text-sm text-text-secondary mt-1">
            Drag widgets to lay out your dashboard. Resize from the bottom-right
            corner.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            data-testid="dashboard-widget-add"
            onClick={() => addWidget('text')}
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
          >
            + Text
          </button>
          <button
            type="button"
            data-testid="dashboard-widget-add-chart"
            onClick={() => addWidget('chart')}
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
          >
            + Chart
          </button>
          <button
            type="button"
            data-testid="dashboard-widget-add-table"
            onClick={() => addWidget('table')}
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
          >
            + Table
          </button>
          <button
            type="button"
            data-testid="dashboard-widget-add-stat"
            onClick={() => addWidget('stat')}
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
          >
            + Stat
          </button>
          <button
            type="button"
            data-testid="dashboard-widget-add-map"
            onClick={() => addWidget('map')}
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
          >
            + Map
          </button>
        </div>
      </div>

      <div
        data-testid="dashboard-toolbar"
        className="flex flex-wrap items-center gap-2 mb-3"
      >
        <input
          type="text"
          data-testid="dashboard-name-input"
          aria-label="Dashboard name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="px-2 py-1 rounded border border-border bg-bg-secondary text-sm text-text-primary"
        />
        <label className="flex items-center gap-1 text-xs text-text-secondary font-mono">
          <input
            type="checkbox"
            data-testid="dashboard-public-toggle"
            checked={isPublic}
            onChange={(e) => setIsPublic(e.target.checked)}
          />
          Public
        </label>
        <button
          type="button"
          data-testid="dashboard-save"
          onClick={() => {
            void handleSave();
          }}
          className="px-3 py-1.5 rounded border border-border bg-accent-primary/20 text-sm text-text-primary hover:border-accent-primary"
        >
          {savedId ? 'Save' : 'Save New'}
        </button>
        {saveStatus !== 'idle' && (
          <span
            data-testid="dashboard-save-status"
            data-save-status={saveStatus}
            className="text-xs font-mono text-text-secondary"
          >
            {saveStatus === 'saving' && 'Saving…'}
            {saveStatus === 'saved' && 'Saved'}
            {saveStatus === 'error' && 'Save failed'}
          </span>
        )}
        {savedId && (
          <button
            type="button"
            data-testid="dashboard-share"
            onClick={() => {
              void handleShare();
            }}
            title={shareUrl}
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
          >
            {shareStatus === 'copied' ? 'Link copied' : 'Share Link'}
          </button>
        )}
        {savedId && (
          <span
            data-testid="dashboard-share-url"
            className="text-xs font-mono text-text-secondary truncate"
          >
            {shareUrl}
          </span>
        )}
        {savedId && (
          <button
            type="button"
            data-testid="dashboard-duplicate"
            onClick={() => {
              void handleDuplicate();
            }}
            disabled={duplicateStatus === 'duplicating'}
            title="Create a copy of this dashboard"
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary disabled:opacity-60"
          >
            {duplicateStatus === 'duplicating' ? 'Duplicating…' : 'Duplicate'}
          </button>
        )}
        {duplicateStatus === 'error' && (
          <span
            data-testid="dashboard-duplicate-status"
            className="text-xs font-mono text-accent-error"
          >
            Duplicate failed
          </span>
        )}
        {savedId && !confirmingDelete && (
          <button
            ref={deleteButtonRef}
            type="button"
            data-testid="dashboard-delete"
            onClick={() => setConfirmingDelete(true)}
            disabled={deleteStatus === 'deleting'}
            title="Delete this dashboard"
            className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-accent-error hover:border-accent-error disabled:opacity-60"
          >
            Delete
          </button>
        )}
        {savedId && confirmingDelete && (
          <DeleteConfirmDialog
            deleting={deleteStatus === 'deleting'}
            onConfirm={() => {
              void handleDelete();
            }}
            onCancel={() => setConfirmingDelete(false)}
          />
        )}
        {deleteStatus === 'error' && (
          <span
            data-testid="dashboard-delete-status"
            className="text-xs font-mono text-accent-error"
          >
            Delete failed
          </span>
        )}
        {savedDashboards.length > 0 && (
          <select
            data-testid="dashboard-load-select"
            aria-label="Load saved dashboard"
            value={savedId ?? ''}
            onChange={(e) => {
              const next = e.target.value;
              if (!next) return;
              onSaved?.(next);
            }}
            className="px-2 py-1 rounded border border-border bg-bg-secondary text-xs text-text-primary"
          >
            <option value="">Load saved…</option>
            {savedDashboards.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        )}
      </div>

      <div
        data-testid="dashboard-grid"
        onDragOver={handleGridDragOver}
        onDrop={handleGridDrop}
        className="relative border border-border rounded bg-bg-secondary/40"
        style={{
          display: 'grid',
          gridTemplateColumns: `repeat(${COLUMN_COUNT}, minmax(0, 1fr))`,
          gridAutoRows: `${ROW_HEIGHT_PX}px`,
          minHeight: totalRows * ROW_HEIGHT_PX,
          padding: 8,
          gap: 8,
        }}
      >
        {widgets.length === 0 && (
          <div
            data-testid="dashboard-empty"
            style={{ gridColumn: `1 / -1`, gridRow: `1 / span 4` }}
            className="flex flex-col items-center justify-center text-center text-text-secondary"
          >
            <p className="text-sm">No widgets yet.</p>
            <p className="text-xs font-mono mt-1">
              Pick a widget type to drop your first widget on the grid.
            </p>
          </div>
        )}
        {widgets.map((widget) => (
          <DashboardWidget
            key={widget.id}
            widget={widget}
            isConfiguring={configuringId === widget.id}
            onConfigureToggle={() =>
              setConfiguringId((cur) => (cur === widget.id ? null : widget.id))
            }
            onRemove={() => removeWidget(widget.id)}
            onPatch={(patch) => patchWidget(widget.id, patch)}
            onDragStart={handleDragStart}
          />
        ))}
      </div>
    </div>
  );
}

// Elements that can receive keyboard focus, used by the dialog's focus trap.
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

interface DeleteConfirmDialogProps {
  deleting: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

// Self-drawn confirm popover for the destructive Delete action. It is NOT the
// shared common/Modal (which already traps + restores focus), so it carries
// its own focus management, mirroring VertexShareLinkPanel (#229): on open we
// move focus inside, keep Tab/Shift+Tab cycling within (focus trap, degenerate
// -safe), and close on Escape via the existing cancel callback — so keyboard
// users never end up stranded behind the popover. The parent conditionally
// mounts/unmounts this on open/close (mount == open, unmount == close) and
// owns focus *restore*: the Delete button swaps itself out for this dialog, so
// the parent re-focuses the freshly re-rendered Delete button on close.
function DeleteConfirmDialog({
  deleting,
  onConfirm,
  onCancel,
}: DeleteConfirmDialogProps) {
  const dialogRef = useRef<HTMLSpanElement>(null);

  // Move focus inside on mount so it never sits on the page behind the dialog.
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const first = dialog.querySelector<HTMLElement>(FOCUSABLE_SELECTOR);
    // Prefer the first focusable child; fall back to the dialog itself
    // (focusable via tabIndex={-1}).
    if (first) first.focus();
    else dialog.focus();
  }, []);

  // Escape closes the dialog through the existing cancel callback. While a
  // delete is in flight the Cancel button is disabled to keep the dialog up
  // until the request settles; Escape honours the same guard so the two paths
  // can't diverge.
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && !deleting) onCancel();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [onCancel, deleting]);

  // Focus trap: keep Tab / Shift+Tab cycling among the dialog's focusable
  // elements instead of escaping to the toolbar behind it.
  const handleTrapKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLSpanElement>) => {
      if (e.key !== 'Tab') return;
      const dialog = dialogRef.current;
      if (!dialog) return;
      const focusables = Array.from(
        dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
      );

      // Degenerate case: nothing focusable inside — keep focus on the dialog.
      if (focusables.length === 0) {
        e.preventDefault();
        dialog.focus();
        return;
      }

      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement;

      if (e.shiftKey) {
        // Shift+Tab on the first element (or focus outside) wraps to last.
        if (active === first || !dialog.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        // Tab on the last element (or focus outside) wraps to first.
        if (active === last || !dialog.contains(active)) {
          e.preventDefault();
          first.focus();
        }
      }
    },
    [],
  );

  return (
    <span
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-label="Confirm delete dashboard"
      data-testid="dashboard-delete-confirm-dialog"
      tabIndex={-1}
      onKeyDown={handleTrapKeyDown}
      className="inline-flex items-center gap-2 px-2 py-1 rounded border border-accent-error/60 bg-accent-error/10"
    >
      <span className="text-xs font-mono text-text-primary">
        Delete this dashboard?
      </span>
      <button
        type="button"
        data-testid="dashboard-delete-confirm"
        onClick={onConfirm}
        disabled={deleting}
        className="px-2 py-0.5 rounded border border-accent-error text-xs text-accent-error hover:bg-accent-error/20 disabled:opacity-60"
      >
        {deleting ? 'Deleting…' : 'Confirm Delete'}
      </button>
      <button
        type="button"
        data-testid="dashboard-delete-cancel"
        onClick={onCancel}
        disabled={deleting}
        className="px-2 py-0.5 rounded border border-border text-xs text-text-secondary hover:text-text-primary disabled:opacity-60"
      >
        Cancel
      </button>
    </span>
  );
}

interface DashboardWidgetProps {
  widget: Widget;
  isConfiguring: boolean;
  onConfigureToggle: () => void;
  onRemove: () => void;
  onPatch: (patch: Partial<Widget>) => void;
  onDragStart: (e: React.DragEvent, payload: DragPayload) => void;
}

function DashboardWidget({
  widget,
  isConfiguring,
  onConfigureToggle,
  onRemove,
  onPatch,
  onDragStart,
}: DashboardWidgetProps) {
  return (
    <div
      data-testid="dashboard-widget"
      data-widget-id={widget.id}
      data-widget-type={widget.type}
      data-widget-x={widget.x}
      data-widget-y={widget.y}
      data-widget-w={widget.w}
      data-widget-h={widget.h}
      className="relative border border-border rounded bg-bg-primary text-text-primary flex flex-col overflow-hidden"
      style={{
        gridColumn: `${widget.x + 1} / span ${widget.w}`,
        gridRow: `${widget.y + 1} / span ${widget.h}`,
      }}
    >
      <div
        data-testid="dashboard-widget-drag-handle"
        draggable
        onDragStart={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          const offsetX = Number.isFinite(e.clientX)
            ? e.clientX - rect.left
            : 0;
          const offsetY = Number.isFinite(e.clientY)
            ? e.clientY - rect.top
            : 0;
          onDragStart(e, {
            id: widget.id,
            kind: 'move',
            offsetX,
            offsetY,
          });
        }}
        className="flex items-center justify-between px-2 py-1 border-b border-border bg-bg-secondary/60 cursor-move"
      >
        <span
          data-testid="dashboard-widget-title"
          className="text-xs font-mono font-medium truncate"
        >
          {widget.title}
        </span>
        <div className="flex items-center gap-1">
          <button
            type="button"
            data-testid="dashboard-widget-configure"
            aria-label={`Configure ${widget.title}`}
            onClick={onConfigureToggle}
            className="text-text-secondary hover:text-text-primary text-xs px-1"
          >
            ⚙
          </button>
          <button
            type="button"
            data-testid="dashboard-widget-remove"
            aria-label={`Remove ${widget.title}`}
            onClick={onRemove}
            className="text-text-secondary hover:text-accent-error text-xs px-1"
          >
            ×
          </button>
        </div>
      </div>
      <div className="flex-1 p-2 overflow-auto">
        {isConfiguring ? (
          <WidgetConfig widget={widget} onPatch={onPatch} />
        ) : (
          <WidgetDisplay widget={widget} />
        )}
      </div>
      <div
        data-testid="dashboard-widget-resize-handle"
        draggable
        onDragStart={(e) => {
          onDragStart(e, {
            id: widget.id,
            kind: 'resize',
            offsetX: 0,
            offsetY: 0,
          });
        }}
        aria-label={`Resize ${widget.title}`}
        className="absolute bottom-0 right-0 w-3 h-3 cursor-nwse-resize text-text-secondary"
        style={{
          background:
            'linear-gradient(135deg, transparent 50%, rgba(245,158,11,0.4) 50%)',
        }}
      />
    </div>
  );
}

function WidgetDisplay({ widget }: { widget: Widget }) {
  switch (widget.type) {
    case 'text':
      return <TextDisplay widget={widget} />;
    case 'chart':
      return <ChartView widget={widget} />;
    case 'table':
      return <TableDisplay widget={widget} />;
    case 'stat':
      return <StatView widget={widget} />;
    case 'map':
      return <MapView widget={widget} />;
  }
}

function WidgetConfig({
  widget,
  onPatch,
}: {
  widget: Widget;
  onPatch: (patch: Partial<Widget>) => void;
}) {
  switch (widget.type) {
    case 'text':
      return <TextConfig widget={widget} onPatch={onPatch} />;
    case 'chart':
      return <ChartConfig widget={widget} onPatch={onPatch} />;
    case 'table':
      return <TableConfig widget={widget} onPatch={onPatch} />;
    case 'stat':
      return <StatConfig widget={widget} onPatch={onPatch} />;
    case 'map':
      return <MapConfig widget={widget} onPatch={onPatch} />;
  }
}

const labelClass =
  'text-[10px] uppercase tracking-wider text-text-secondary font-mono';
const inputClass =
  'mt-1 w-full px-2 py-1 rounded border border-border bg-bg-secondary text-xs text-text-primary';
const monoInputClass = `${inputClass} font-mono`;

function TitleInput({
  value,
  onChange,
}: {
  value: string;
  onChange: (next: string) => void;
}) {
  return (
    <label className={labelClass}>
      Title
      <input
        data-testid="dashboard-widget-title-input"
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={inputClass}
      />
    </label>
  );
}

function TextDisplay({ widget }: { widget: TextWidget }) {
  return (
    <div
      data-testid="dashboard-widget-content"
      className="text-xs whitespace-pre-wrap text-text-primary"
    >
      {widget.content || (
        <span className="text-text-secondary italic">
          Click ⚙ to configure
        </span>
      )}
    </div>
  );
}

function TextConfig({
  widget,
  onPatch,
}: {
  widget: TextWidget;
  onPatch: (patch: Partial<TextWidget>) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <TitleInput
        value={widget.title}
        onChange={(title) => onPatch({ title })}
      />
      <label className={labelClass}>
        Content
        <textarea
          data-testid="dashboard-widget-content-input"
          value={widget.content}
          onChange={(e) => onPatch({ content: e.target.value })}
          rows={3}
          className={monoInputClass}
        />
      </label>
    </div>
  );
}

function ChartConfig({
  widget,
  onPatch,
}: {
  widget: ChartWidget;
  onPatch: (patch: Partial<ChartWidget>) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <TitleInput
        value={widget.title}
        onChange={(title) => onPatch({ title })}
      />
      <label className={labelClass}>
        Chart Type
        <select
          data-testid="dashboard-widget-chart-type-select"
          value={widget.chartType}
          onChange={(e) =>
            onPatch({ chartType: e.target.value as ChartType })
          }
          className={inputClass}
        >
          <option value="bar">bar</option>
          <option value="line">line</option>
          <option value="pie">pie</option>
        </select>
      </label>
      <label className={labelClass}>
        Values (comma-separated)
        <input
          data-testid="dashboard-widget-chart-values-input"
          type="text"
          defaultValue={widget.values.join(', ')}
          onChange={(e) => onPatch({ values: parseValuesInput(e.target.value) })}
          className={monoInputClass}
        />
      </label>
      <DataSourceConfig
        source={widget.dataSource}
        onChange={(dataSource) => onPatch({ dataSource })}
      />
    </div>
  );
}

function TableDisplay({ widget }: { widget: TableWidget }) {
  return (
    <table
      data-testid="dashboard-widget-table"
      className="w-full text-xs border-collapse"
    >
      <thead>
        <tr>
          {widget.columns.map((col, i) => (
            <th
              key={`col-${i}`}
              className="text-left px-1 py-0.5 border-b border-border font-mono text-[10px] uppercase tracking-wider text-text-secondary"
            >
              {col}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {widget.rows.map((row, ri) => (
          <tr key={`row-${ri}`}>
            {row.map((cell, ci) => (
              <td
                key={`cell-${ri}-${ci}`}
                className="px-1 py-0.5 border-b border-border/40 text-text-primary"
              >
                {cell}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TableConfig({
  widget,
  onPatch,
}: {
  widget: TableWidget;
  onPatch: (patch: Partial<TableWidget>) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <TitleInput
        value={widget.title}
        onChange={(title) => onPatch({ title })}
      />
      <label className={labelClass}>
        Columns (comma-separated)
        <input
          data-testid="dashboard-widget-table-columns-input"
          type="text"
          defaultValue={widget.columns.join(', ')}
          onChange={(e) =>
            onPatch({ columns: parseColumnsInput(e.target.value) })
          }
          className={monoInputClass}
        />
      </label>
      <label className={labelClass}>
        Rows (one per line, comma-separated cells)
        <textarea
          data-testid="dashboard-widget-table-rows-input"
          defaultValue={widget.rows.map((r) => r.join(', ')).join('\n')}
          onChange={(e) => onPatch({ rows: parseRowsInput(e.target.value) })}
          rows={3}
          className={monoInputClass}
        />
      </label>
    </div>
  );
}

function StatConfig({
  widget,
  onPatch,
}: {
  widget: StatWidget;
  onPatch: (patch: Partial<StatWidget>) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <TitleInput
        value={widget.title}
        onChange={(title) => onPatch({ title })}
      />
      <label className={labelClass}>
        Value
        <input
          data-testid="dashboard-widget-stat-value-input"
          type="text"
          value={widget.value}
          onChange={(e) => onPatch({ value: e.target.value })}
          className={inputClass}
        />
      </label>
      <label className={labelClass}>
        Label
        <input
          data-testid="dashboard-widget-stat-label-input"
          type="text"
          value={widget.label}
          onChange={(e) => onPatch({ label: e.target.value })}
          className={inputClass}
        />
      </label>
      <label className={labelClass}>
        Trend
        <select
          data-testid="dashboard-widget-stat-trend-select"
          value={widget.trend}
          onChange={(e) => onPatch({ trend: e.target.value as StatTrend })}
          className={inputClass}
        >
          <option value="neutral">neutral</option>
          <option value="up">up</option>
          <option value="down">down</option>
        </select>
      </label>
      <label className={labelClass}>
        Sparkline (comma-separated)
        <input
          data-testid="dashboard-widget-stat-sparkline-input"
          type="text"
          defaultValue={(widget.sparkline ?? []).join(', ')}
          onChange={(e) =>
            onPatch({ sparkline: parseValuesInput(e.target.value) })
          }
          className={monoInputClass}
        />
      </label>
      <DataSourceConfig
        source={widget.dataSource}
        onChange={(dataSource) => onPatch({ dataSource })}
      />
    </div>
  );
}

function MapConfig({
  widget,
  onPatch,
}: {
  widget: MapWidget;
  onPatch: (patch: Partial<MapWidget>) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <TitleInput
        value={widget.title}
        onChange={(title) => onPatch({ title })}
      />
      <label className={labelClass}>
        Latitude
        <input
          data-testid="dashboard-widget-map-lat-input"
          type="text"
          inputMode="decimal"
          defaultValue={String(widget.latitude)}
          onChange={(e) => {
            const n = Number(e.target.value);
            if (Number.isFinite(n)) onPatch({ latitude: n });
          }}
          className={monoInputClass}
        />
      </label>
      <label className={labelClass}>
        Longitude
        <input
          data-testid="dashboard-widget-map-lng-input"
          type="text"
          inputMode="decimal"
          defaultValue={String(widget.longitude)}
          onChange={(e) => {
            const n = Number(e.target.value);
            if (Number.isFinite(n)) onPatch({ longitude: n });
          }}
          className={monoInputClass}
        />
      </label>
      <label className={labelClass}>
        Zoom
        <input
          data-testid="dashboard-widget-map-zoom-input"
          type="text"
          inputMode="numeric"
          defaultValue={String(widget.zoom)}
          onChange={(e) => {
            const n = Number(e.target.value);
            if (Number.isFinite(n)) onPatch({ zoom: n });
          }}
          className={monoInputClass}
        />
      </label>
      <label className={labelClass}>
        GeoJSON (optional)
        <textarea
          data-testid="dashboard-widget-map-geojson-input"
          defaultValue={
            widget.geojson === undefined
              ? ''
              : typeof widget.geojson === 'string'
                ? widget.geojson
                : JSON.stringify(widget.geojson)
          }
          onChange={(e) => {
            const raw = e.target.value.trim();
            if (raw === '') {
              onPatch({ geojson: undefined });
              return;
            }
            try {
              onPatch({ geojson: JSON.parse(raw) });
            } catch {
              // Keep the raw string so the user can edit a partial paste
              // without losing it; the runtime parser handles strings too.
              onPatch({ geojson: raw });
            }
          }}
          rows={3}
          className={monoInputClass}
        />
      </label>
    </div>
  );
}

// US-428: shared data-source picker for chart and stat widgets. Map widgets
// currently bind to a static GeoJSON literal — leaflet shape data is too
// heterogeneous to fit the same `metric / property / groupBy` shape.
function DataSourceConfig({
  source,
  onChange,
}: {
  source: WidgetDataSource | undefined;
  onChange: (next: WidgetDataSource | undefined) => void;
}) {
  const kind = source?.kind ?? 'inline';
  const agg =
    source && source.kind === 'aggregation'
      ? source
      : {
          kind: 'aggregation' as const,
          ontology: '',
          objectType: '',
          metric: 'count' as const,
        };
  return (
    <fieldset
      data-testid="dashboard-widget-data-source"
      data-source-kind={kind}
      className="border border-border/60 rounded p-2 mt-1"
    >
      <legend className={`${labelClass} px-1`}>Data Source</legend>
      <label className={labelClass}>
        Source
        <select
          data-testid="dashboard-widget-data-source-kind"
          value={kind}
          onChange={(e) => {
            if (e.target.value === 'inline') {
              onChange(undefined);
            } else {
              onChange({ ...agg });
            }
          }}
          className={inputClass}
        >
          <option value="inline">Inline values</option>
          <option value="aggregation">Aggregation</option>
        </select>
      </label>
      {kind === 'aggregation' && (
        <div className="grid grid-cols-2 gap-2 mt-2">
          <label className={labelClass}>
            Ontology
            <input
              data-testid="dashboard-widget-data-source-ontology"
              type="text"
              value={agg.ontology}
              onChange={(e) =>
                onChange({ ...agg, ontology: e.target.value })
              }
              className={monoInputClass}
            />
          </label>
          <label className={labelClass}>
            Object Type
            <input
              data-testid="dashboard-widget-data-source-object-type"
              type="text"
              value={agg.objectType}
              onChange={(e) =>
                onChange({ ...agg, objectType: e.target.value })
              }
              className={monoInputClass}
            />
          </label>
          <label className={labelClass}>
            Metric
            <select
              data-testid="dashboard-widget-data-source-metric"
              value={agg.metric}
              onChange={(e) =>
                onChange({
                  ...agg,
                  metric: e.target.value as typeof agg.metric,
                })
              }
              className={inputClass}
            >
              <option value="count">count</option>
              <option value="sum">sum</option>
              <option value="avg">avg</option>
              <option value="min">min</option>
              <option value="max">max</option>
            </select>
          </label>
          <label className={labelClass}>
            Property
            <input
              data-testid="dashboard-widget-data-source-property"
              type="text"
              value={agg.property ?? ''}
              onChange={(e) =>
                onChange({
                  ...agg,
                  property: e.target.value || undefined,
                })
              }
              className={monoInputClass}
            />
          </label>
          <label className={`${labelClass} col-span-2`}>
            Group By
            <input
              data-testid="dashboard-widget-data-source-group-by"
              type="text"
              value={agg.groupBy ?? ''}
              onChange={(e) =>
                onChange({
                  ...agg,
                  groupBy: e.target.value || undefined,
                })
              }
              className={monoInputClass}
            />
          </label>
        </div>
      )}
    </fieldset>
  );
}
