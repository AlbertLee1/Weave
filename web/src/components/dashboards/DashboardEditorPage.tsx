import { useCallback, useMemo, useState } from 'react';

// US-327 Dashboard Editor.
// PRD asked for react-grid-layout; the lib isn't installed and depends on
// react-resizable + react-draggable (~80KB) which would be the only consumer.
// Following the US-324/US-325/US-326 pattern (and learning #209 in
// progress.txt) — when the proposed lib is uninstalled and would add a heavy
// dep that's only used here, we ship a small native HTML5 DnD layout instead.

const COLUMN_COUNT = 12;
const DEFAULT_W = 4;
const DEFAULT_H = 2;
const ROW_HEIGHT_PX = 60;
const DND_MIME = 'application/x-weave-dashboard';

type WidgetType = 'text';

interface Widget {
  id: string;
  type: WidgetType;
  title: string;
  content: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

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

export function DashboardEditorPage() {
  const [widgets, setWidgets] = useState<Widget[]>([]);
  const [configuringId, setConfiguringId] = useState<string | null>(null);

  const addTextWidget = useCallback(() => {
    setWidgets((prev) => {
      const widget: Widget = {
        id: nextWidgetId(),
        type: 'text',
        title: 'New Widget',
        content: '',
        x: 0,
        y: findFirstFreeRow(prev),
        w: DEFAULT_W,
        h: DEFAULT_H,
      };
      return [...prev, widget];
    });
  }, []);

  const removeWidget = useCallback((id: string) => {
    setWidgets((prev) => prev.filter((w) => w.id !== id));
    setConfiguringId((cur) => (cur === id ? null : cur));
  }, []);

  const updateWidget = useCallback(
    (id: string, patch: Partial<Pick<Widget, 'title' | 'content'>>) => {
      setWidgets((prev) =>
        prev.map((w) => (w.id === id ? { ...w, ...patch } : w)),
      );
    },
    [],
  );

  const moveWidget = useCallback(
    (id: string, x: number, y: number) => {
      setWidgets((prev) =>
        prev.map((w) => {
          if (w.id !== id) return w;
          const maxX = Math.max(0, COLUMN_COUNT - w.w);
          return { ...w, x: clamp(x, 0, maxX), y: Math.max(0, y) };
        }),
      );
    },
    [],
  );

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
    <div data-testid="dashboard-editor-page" className="min-h-full p-6">
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
        <button
          type="button"
          data-testid="dashboard-widget-add"
          onClick={addTextWidget}
          className="px-3 py-1.5 rounded border border-border bg-bg-secondary text-sm text-text-primary hover:border-accent-primary"
        >
          + Add Widget
        </button>
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
              Click &ldquo;+ Add Widget&rdquo; to drop your first widget on the grid.
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
            onTitleChange={(title) => updateWidget(widget.id, { title })}
            onContentChange={(content) => updateWidget(widget.id, { content })}
            onDragStart={handleDragStart}
          />
        ))}
      </div>
    </div>
  );
}

interface DashboardWidgetProps {
  widget: Widget;
  isConfiguring: boolean;
  onConfigureToggle: () => void;
  onRemove: () => void;
  onTitleChange: (title: string) => void;
  onContentChange: (content: string) => void;
  onDragStart: (e: React.DragEvent, payload: DragPayload) => void;
}

function DashboardWidget({
  widget,
  isConfiguring,
  onConfigureToggle,
  onRemove,
  onTitleChange,
  onContentChange,
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
          <div className="flex flex-col gap-2">
            <label className="text-[10px] uppercase tracking-wider text-text-secondary font-mono">
              Title
              <input
                data-testid="dashboard-widget-title-input"
                type="text"
                value={widget.title}
                onChange={(e) => onTitleChange(e.target.value)}
                className="mt-1 w-full px-2 py-1 rounded border border-border bg-bg-secondary text-xs text-text-primary"
              />
            </label>
            <label className="text-[10px] uppercase tracking-wider text-text-secondary font-mono">
              Content
              <textarea
                data-testid="dashboard-widget-content-input"
                value={widget.content}
                onChange={(e) => onContentChange(e.target.value)}
                rows={3}
                className="mt-1 w-full px-2 py-1 rounded border border-border bg-bg-secondary text-xs text-text-primary font-mono"
              />
            </label>
          </div>
        ) : (
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
