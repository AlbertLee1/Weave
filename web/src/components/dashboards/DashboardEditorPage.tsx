import { useCallback, useMemo, useState } from 'react';

// US-327 Dashboard Editor + US-328 Widget Library.
// PRD asked for react-grid-layout; the lib isn't installed and depends on
// react-resizable + react-draggable (~80KB) which would be the only consumer.
// Following the US-324/US-325/US-326 pattern (and learning #209 in
// progress.txt) — when the proposed lib is uninstalled and would add a heavy
// dep that's only used here, we ship a small native HTML5 DnD layout instead.
//
// Widget types live inline as a discriminated union; each type owns a small
// display + config sub-component and a sensible default-factory.

const COLUMN_COUNT = 12;
const DEFAULT_W = 4;
const DEFAULT_H = 2;
const ROW_HEIGHT_PX = 60;
const DND_MIME = 'application/x-weave-dashboard';

type ChartType = 'bar' | 'line' | 'pie';
type StatTrend = 'up' | 'down' | 'neutral';

interface BaseWidget {
  id: string;
  title: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

interface TextWidget extends BaseWidget {
  type: 'text';
  content: string;
}

interface ChartWidget extends BaseWidget {
  type: 'chart';
  chartType: ChartType;
  values: number[];
}

interface TableWidget extends BaseWidget {
  type: 'table';
  columns: string[];
  rows: string[][];
}

interface StatWidget extends BaseWidget {
  type: 'stat';
  value: string;
  label: string;
  trend: StatTrend;
}

interface MapWidget extends BaseWidget {
  type: 'map';
  latitude: number;
  longitude: number;
  zoom: number;
}

type Widget = TextWidget | ChartWidget | TableWidget | StatWidget | MapWidget;
type WidgetType = Widget['type'];

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

export function DashboardEditorPage() {
  const [widgets, setWidgets] = useState<Widget[]>([]);
  const [configuringId, setConfiguringId] = useState<string | null>(null);

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
      return <ChartDisplay widget={widget} />;
    case 'table':
      return <TableDisplay widget={widget} />;
    case 'stat':
      return <StatDisplay widget={widget} />;
    case 'map':
      return <MapDisplay widget={widget} />;
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

function ChartDisplay({ widget }: { widget: ChartWidget }) {
  const max = widget.values.length === 0 ? 0 : Math.max(...widget.values, 0);
  return (
    <div
      data-testid="dashboard-widget-chart"
      data-chart-type={widget.chartType}
      data-chart-values={widget.values.join(',')}
      className="w-full h-full flex items-end justify-around gap-1"
    >
      {widget.values.length === 0 && (
        <span className="text-text-secondary italic text-xs self-center">
          No values
        </span>
      )}
      {widget.values.map((v, i) => {
        const ratio = max > 0 ? Math.max(0.05, v / max) : 0.05;
        return (
          <div
            key={i}
            data-testid="dashboard-widget-chart-bar"
            className="flex-1 bg-accent-primary/70 rounded-sm"
            style={{ height: `${Math.round(ratio * 100)}%`, minHeight: 2 }}
            title={String(v)}
          />
        );
      })}
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

function StatDisplay({ widget }: { widget: StatWidget }) {
  const trendSymbol =
    widget.trend === 'up' ? '▲' : widget.trend === 'down' ? '▼' : '—';
  const trendClass =
    widget.trend === 'up'
      ? 'text-accent-success'
      : widget.trend === 'down'
        ? 'text-accent-error'
        : 'text-text-secondary';
  return (
    <div
      data-testid="dashboard-widget-stat"
      data-stat-trend={widget.trend}
      className="w-full h-full flex flex-col items-center justify-center text-center"
    >
      <div className="text-2xl font-semibold text-text-primary">
        {widget.value}
      </div>
      <div className="text-[10px] uppercase tracking-wider text-text-secondary mt-1">
        {widget.label}
      </div>
      <div className={`text-xs mt-1 ${trendClass}`}>{trendSymbol}</div>
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
    </div>
  );
}

function MapDisplay({ widget }: { widget: MapWidget }) {
  return (
    <div
      data-testid="dashboard-widget-map"
      data-map-lat={widget.latitude}
      data-map-lng={widget.longitude}
      data-map-zoom={widget.zoom}
      className="w-full h-full flex items-center justify-center bg-bg-secondary/40 text-xs font-mono text-text-secondary relative"
    >
      <span className="absolute top-1 left-1">
        {widget.latitude.toFixed(4)}, {widget.longitude.toFixed(4)} · z
        {widget.zoom}
      </span>
      <span className="text-base text-accent-primary">📍</span>
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
    </div>
  );
}
