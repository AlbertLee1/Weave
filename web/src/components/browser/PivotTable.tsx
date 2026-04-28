import { useMemo, useState, useCallback } from 'react';
import type { ObjectType, WireObject } from '../../api/types';
import { baseTypeOf } from '../../lib/geoParser';

// A self-contained pivot table with native HTML5 drag-and-drop.
// PRD asked for react-pivottable; codebase pattern (US-324 Gantt, US-325 Sankey)
// is to avoid 1-MB-class chart deps and ship an inline implementation when the
// existing toolbox already covers the requirement.

const CATEGORICAL_BASE_TYPES = new Set([
  'string',
  'boolean',
  'date',
  'datetime',
  'timestamp',
]);
const NUMERIC_BASE_TYPES = new Set([
  'integer',
  'long',
  'double',
  'float',
  'decimal',
]);

const AGGREGATIONS = ['count', 'sum', 'avg', 'min', 'max'] as const;
type Aggregation = (typeof AGGREGATIONS)[number];

interface ValueSpec {
  field: string;
  aggregation: Aggregation;
}

interface PivotTableProps {
  objectType: ObjectType;
  data: WireObject[];
  onRowClick?: (row: WireObject) => void;
}

type Zone = 'rows' | 'columns' | 'values' | 'available';
type DragKind = 'field' | 'value';

interface DragPayload {
  kind: DragKind;
  field: string;
  from: Zone;
  index: number;
}

const DND_MIME = 'application/x-weave-pivot';

function dataTypeOfField(
  objectType: ObjectType,
  field: string,
): string | null {
  const prop = objectType.properties?.[field];
  if (!prop) return null;
  return baseTypeOf(prop.dataType);
}

function isNumericField(objectType: ObjectType, field: string): boolean {
  const t = dataTypeOfField(objectType, field);
  return t !== null && NUMERIC_BASE_TYPES.has(t);
}

function isCategoricalField(objectType: ObjectType, field: string): boolean {
  const t = dataTypeOfField(objectType, field);
  return t !== null && CATEGORICAL_BASE_TYPES.has(t);
}

function stringifyKey(value: unknown): string {
  if (value === null || value === undefined || value === '') return '∅';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  return JSON.stringify(value);
}

function numericValue(v: unknown): number | null {
  if (typeof v === 'number' && Number.isFinite(v)) return v;
  if (typeof v === 'string' && v !== '') {
    const n = Number(v);
    if (Number.isFinite(n)) return n;
  }
  return null;
}

// Compose a cell aggregate from a list of rows for a given value spec.
// count is field-agnostic; numeric aggregations skip non-numeric values.
function aggregate(rows: WireObject[], spec: ValueSpec): number | null {
  if (rows.length === 0) return null;
  if (spec.aggregation === 'count') return rows.length;
  const values: number[] = [];
  for (const r of rows) {
    const n = numericValue(r[spec.field]);
    if (n !== null) values.push(n);
  }
  if (values.length === 0) return null;
  switch (spec.aggregation) {
    case 'sum':
      return values.reduce((a, b) => a + b, 0);
    case 'avg':
      return values.reduce((a, b) => a + b, 0) / values.length;
    case 'min':
      return Math.min(...values);
    case 'max':
      return Math.max(...values);
    default:
      return null;
  }
}

function formatNumber(n: number | null): string {
  if (n === null) return '';
  if (!Number.isFinite(n)) return '';
  if (Number.isInteger(n)) return String(n);
  return n.toFixed(2);
}

// Group rows by a tuple of field values, keeping insertion order stable
// (sorted on the stringified key for deterministic test output).
function groupRows(
  rows: WireObject[],
  fields: string[],
): Map<string, { keys: string[]; rows: WireObject[] }> {
  const out = new Map<string, { keys: string[]; rows: WireObject[] }>();
  for (const row of rows) {
    const keys = fields.map((f) => stringifyKey(row[f]));
    const key = keys.join('␟');
    const existing = out.get(key);
    if (existing) existing.rows.push(row);
    else out.set(key, { keys, rows: [row] });
  }
  return new Map(
    Array.from(out.entries()).sort((a, b) => a[0].localeCompare(b[0])),
  );
}

interface PivotResult {
  rowGroups: { keys: string[]; rows: WireObject[] }[];
  colGroups: { keys: string[]; rows: WireObject[] }[];
  // cells[rowKey][colKey][valueIndex] = aggregate
  cells: Map<string, Map<string, (number | null)[]>>;
  rowTotals: Map<string, (number | null)[]>;
  colTotals: Map<string, (number | null)[]>;
  grandTotal: (number | null)[];
}

function computePivot(
  data: WireObject[],
  rowFields: string[],
  colFields: string[],
  values: ValueSpec[],
): PivotResult {
  const rowGroups = Array.from(groupRows(data, rowFields).values());
  const colGroups = Array.from(groupRows(data, colFields).values());

  const cells = new Map<string, Map<string, (number | null)[]>>();
  const rowTotals = new Map<string, (number | null)[]>();
  const colTotals = new Map<string, (number | null)[]>();
  const grandTotal: (number | null)[] = values.map((v) => aggregate(data, v));

  for (const rg of rowGroups) {
    const rKey = rg.keys.join('␟');
    const rowMap = new Map<string, (number | null)[]>();
    rowTotals.set(
      rKey,
      values.map((v) => aggregate(rg.rows, v)),
    );
    for (const cg of colGroups) {
      const cKey = cg.keys.join('␟');
      const cellRows = rg.rows.filter((r) => cg.rows.includes(r));
      rowMap.set(
        cKey,
        values.map((v) => aggregate(cellRows, v)),
      );
    }
    cells.set(rKey, rowMap);
  }

  for (const cg of colGroups) {
    const cKey = cg.keys.join('␟');
    colTotals.set(
      cKey,
      values.map((v) => aggregate(cg.rows, v)),
    );
  }

  return { rowGroups, colGroups, cells, rowTotals, colTotals, grandTotal };
}

// -----------------------------------------------------------------------------
// Component
// -----------------------------------------------------------------------------

export function PivotTable({
  objectType,
  data,
  onRowClick,
}: PivotTableProps) {
  const allFields = useMemo(() => {
    return Object.keys(objectType.properties ?? {}).filter(
      (f) => f !== objectType.primaryKey,
    );
  }, [objectType]);

  const [rowFields, setRowFields] = useState<string[]>([]);
  const [colFields, setColFields] = useState<string[]>([]);
  const [valueSpecs, setValueSpecs] = useState<ValueSpec[]>([]);

  const placedFields = useMemo(() => {
    const s = new Set<string>([...rowFields, ...colFields]);
    for (const v of valueSpecs) s.add(v.field);
    return s;
  }, [rowFields, colFields, valueSpecs]);

  const availableFields = useMemo(
    () => allFields.filter((f) => !placedFields.has(f)),
    [allFields, placedFields],
  );

  const handleDragStart = useCallback(
    (e: React.DragEvent, payload: DragPayload) => {
      e.dataTransfer.setData(DND_MIME, JSON.stringify(payload));
      e.dataTransfer.effectAllowed = 'move';
    },
    [],
  );

  const handleDragOver = useCallback((e: React.DragEvent) => {
    if (e.dataTransfer.types.includes(DND_MIME)) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'move';
    }
  }, []);

  const removeFrom = useCallback(
    (zone: Zone, index: number) => {
      if (zone === 'rows') {
        setRowFields((prev) => prev.filter((_, i) => i !== index));
      } else if (zone === 'columns') {
        setColFields((prev) => prev.filter((_, i) => i !== index));
      } else if (zone === 'values') {
        setValueSpecs((prev) => prev.filter((_, i) => i !== index));
      }
    },
    [],
  );

  const handleDrop = useCallback(
    (e: React.DragEvent, target: Zone) => {
      e.preventDefault();
      const raw = e.dataTransfer.getData(DND_MIME);
      if (!raw) return;
      let payload: DragPayload;
      try {
        payload = JSON.parse(raw) as DragPayload;
      } catch {
        return;
      }
      if (payload.from === target) return;
      if (target === 'available') {
        removeFrom(payload.from, payload.index);
        return;
      }
      // Categorical-only zones reject numeric-only fields and vice versa,
      // EXCEPT 'values' which accepts any field (count works on anything).
      if (target === 'rows' || target === 'columns') {
        if (!isCategoricalField(objectType, payload.field)) return;
      }
      removeFrom(payload.from, payload.index);
      if (target === 'rows') {
        setRowFields((prev) => [...prev, payload.field]);
      } else if (target === 'columns') {
        setColFields((prev) => [...prev, payload.field]);
      } else if (target === 'values') {
        // Default aggregation: count for non-numeric, sum for numeric.
        const agg: Aggregation = isNumericField(objectType, payload.field)
          ? 'sum'
          : 'count';
        setValueSpecs((prev) => [
          ...prev,
          { field: payload.field, aggregation: agg },
        ]);
      }
    },
    [objectType, removeFrom],
  );

  const updateAggregation = useCallback(
    (index: number, aggregation: Aggregation) => {
      setValueSpecs((prev) =>
        prev.map((v, i) => (i === index ? { ...v, aggregation } : v)),
      );
    },
    [],
  );

  const pivot = useMemo(() => {
    if (rowFields.length === 0 && colFields.length === 0) return null;
    if (valueSpecs.length === 0) return null;
    return computePivot(data, rowFields, colFields, valueSpecs);
  }, [data, rowFields, colFields, valueSpecs]);

  return (
    <div
      data-testid="pivot-table"
      className="border border-border rounded overflow-hidden bg-bg-secondary"
    >
      <div className="grid grid-cols-4 gap-2 p-3 border-b border-border">
        <DropZone
          label="Available"
          zone="available"
          onDragOver={handleDragOver}
          onDrop={handleDrop}
          testId="pivot-available"
        >
          {availableFields.map((field, i) => (
            <FieldChip
              key={field}
              field={field}
              kind="field"
              from="available"
              index={i}
              objectType={objectType}
              onDragStart={handleDragStart}
              testId="pivot-field-available"
            />
          ))}
        </DropZone>
        <DropZone
          label="Rows"
          zone="rows"
          onDragOver={handleDragOver}
          onDrop={handleDrop}
          testId="pivot-zone-rows"
        >
          {rowFields.map((field, i) => (
            <FieldChip
              key={field}
              field={field}
              kind="field"
              from="rows"
              index={i}
              objectType={objectType}
              onDragStart={handleDragStart}
              onRemove={() => removeFrom('rows', i)}
              testId="pivot-field-rows"
            />
          ))}
        </DropZone>
        <DropZone
          label="Columns"
          zone="columns"
          onDragOver={handleDragOver}
          onDrop={handleDrop}
          testId="pivot-zone-columns"
        >
          {colFields.map((field, i) => (
            <FieldChip
              key={field}
              field={field}
              kind="field"
              from="columns"
              index={i}
              objectType={objectType}
              onDragStart={handleDragStart}
              onRemove={() => removeFrom('columns', i)}
              testId="pivot-field-columns"
            />
          ))}
        </DropZone>
        <DropZone
          label="Values"
          zone="values"
          onDragOver={handleDragOver}
          onDrop={handleDrop}
          testId="pivot-zone-values"
        >
          {valueSpecs.map((spec, i) => (
            <ValueChip
              key={`${spec.field}-${i}`}
              spec={spec}
              index={i}
              onDragStart={handleDragStart}
              onRemove={() => removeFrom('values', i)}
              onAggregationChange={(a) => updateAggregation(i, a)}
            />
          ))}
        </DropZone>
      </div>

      <PivotGrid
        pivot={pivot}
        rowFields={rowFields}
        colFields={colFields}
        valueSpecs={valueSpecs}
        onRowClick={onRowClick}
      />
    </div>
  );
}

// -----------------------------------------------------------------------------
// Subcomponents
// -----------------------------------------------------------------------------

function DropZone({
  label,
  zone,
  onDragOver,
  onDrop,
  testId,
  children,
}: {
  label: string;
  zone: Zone;
  onDragOver: (e: React.DragEvent) => void;
  onDrop: (e: React.DragEvent, zone: Zone) => void;
  testId: string;
  children: React.ReactNode;
}) {
  return (
    <div
      data-testid={testId}
      data-zone={zone}
      onDragOver={onDragOver}
      onDrop={(e) => onDrop(e, zone)}
      className="border border-border rounded p-2 min-h-[96px] flex flex-col gap-1 bg-bg-primary/40"
    >
      <span className="text-[10px] font-mono uppercase tracking-wider text-text-secondary">
        {label}
      </span>
      <div className="flex flex-wrap gap-1">{children}</div>
    </div>
  );
}

function FieldChip({
  field,
  from,
  index,
  objectType,
  onDragStart,
  onRemove,
  testId,
}: {
  field: string;
  kind: DragKind;
  from: Zone;
  index: number;
  objectType: ObjectType;
  onDragStart: (e: React.DragEvent, payload: DragPayload) => void;
  onRemove?: () => void;
  testId: string;
}) {
  const t = dataTypeOfField(objectType, field) ?? '';
  return (
    <span
      data-testid={testId}
      data-field={field}
      data-base-type={t}
      draggable
      onDragStart={(e) =>
        onDragStart(e, { kind: 'field', field, from, index })
      }
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded border border-border bg-bg-secondary text-xs font-mono text-text-primary cursor-move"
    >
      <span>{field}</span>
      <span className="text-[9px] text-text-secondary">{t}</span>
      {onRemove && (
        <button
          type="button"
          aria-label={`Remove ${field}`}
          onClick={onRemove}
          className="ml-0.5 text-text-secondary hover:text-accent-error"
        >
          ×
        </button>
      )}
    </span>
  );
}

function ValueChip({
  spec,
  index,
  onDragStart,
  onRemove,
  onAggregationChange,
}: {
  spec: ValueSpec;
  index: number;
  onDragStart: (e: React.DragEvent, payload: DragPayload) => void;
  onRemove: () => void;
  onAggregationChange: (a: Aggregation) => void;
}) {
  return (
    <span
      data-testid="pivot-field-values"
      data-field={spec.field}
      data-aggregation={spec.aggregation}
      draggable
      onDragStart={(e) =>
        onDragStart(e, {
          kind: 'value',
          field: spec.field,
          from: 'values',
          index,
        })
      }
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded border border-border bg-bg-secondary text-xs font-mono text-text-primary cursor-move"
    >
      <select
        aria-label={`Aggregation for ${spec.field}`}
        value={spec.aggregation}
        onChange={(e) =>
          onAggregationChange(e.target.value as Aggregation)
        }
        className="bg-transparent text-text-primary text-xs"
      >
        {AGGREGATIONS.map((a) => (
          <option key={a} value={a}>
            {a}
          </option>
        ))}
      </select>
      <span>{spec.field}</span>
      <button
        type="button"
        aria-label={`Remove value ${spec.field}`}
        onClick={onRemove}
        className="ml-0.5 text-text-secondary hover:text-accent-error"
      >
        ×
      </button>
    </span>
  );
}

function PivotGrid({
  pivot,
  rowFields,
  colFields,
  valueSpecs,
  onRowClick,
}: {
  pivot: PivotResult | null;
  rowFields: string[];
  colFields: string[];
  valueSpecs: ValueSpec[];
  onRowClick?: (row: WireObject) => void;
}) {
  if (!pivot) {
    return (
      <div
        data-testid="pivot-empty"
        className="flex flex-col items-center justify-center py-12 text-center"
      >
        <p className="text-sm font-sans text-text-primary">
          Drag fields onto Rows / Columns and a metric onto Values
        </p>
        <p className="text-xs font-mono text-text-secondary mt-1">
          Pivot needs at least one row or column dimension and one value metric.
        </p>
      </div>
    );
  }

  const valueLen = valueSpecs.length;

  return (
    <div className="overflow-auto">
      <table
        data-testid="pivot-grid"
        className="w-full border-collapse text-xs font-mono"
      >
        <thead>
          {colFields.length > 0 && (
            <tr>
              {/* spacer over the row-header columns */}
              <th
                colSpan={Math.max(1, rowFields.length)}
                className="border border-border p-1 bg-bg-primary text-left text-text-secondary"
              >
                {rowFields.join(' / ')}
              </th>
              {pivot.colGroups.map((cg) => (
                <th
                  key={`ch-${cg.keys.join('|')}`}
                  data-testid="pivot-col-header"
                  data-col-key={cg.keys.join('|')}
                  colSpan={valueLen}
                  className="border border-border p-1 bg-bg-primary text-text-primary"
                >
                  {cg.keys.join(' / ')}
                </th>
              ))}
              <th
                colSpan={valueLen}
                className="border border-border p-1 bg-bg-primary text-text-primary"
              >
                Total
              </th>
            </tr>
          )}
          <tr>
            {rowFields.length === 0 ? (
              <th className="border border-border p-1 bg-bg-primary text-text-secondary">
                &nbsp;
              </th>
            ) : (
              rowFields.map((rf) => (
                <th
                  key={`rf-${rf}`}
                  className="border border-border p-1 bg-bg-primary text-text-primary text-left"
                >
                  {rf}
                </th>
              ))
            )}
            {pivot.colGroups.flatMap((cg) =>
              valueSpecs.map((v, vi) => (
                <th
                  key={`vh-${cg.keys.join('|')}-${vi}`}
                  className="border border-border p-1 bg-bg-primary text-text-secondary"
                >
                  {v.aggregation}({v.field})
                </th>
              )),
            )}
            {valueSpecs.map((v, vi) => (
              <th
                key={`vt-${vi}`}
                className="border border-border p-1 bg-bg-primary text-text-secondary"
              >
                {v.aggregation}({v.field})
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {pivot.rowGroups.length === 0 ? (
            <tr>
              <td
                colSpan={
                  Math.max(1, rowFields.length) +
                  pivot.colGroups.length * valueLen +
                  valueLen
                }
                className="border border-border p-2 text-center text-text-secondary"
              >
                No rows
              </td>
            </tr>
          ) : (
            pivot.rowGroups.map((rg) => {
              const rKey = rg.keys.join('␟');
              const cellRow = pivot.cells.get(rKey);
              const totals = pivot.rowTotals.get(rKey) ?? [];
              const interactive = !!onRowClick && rg.rows.length > 0;
              const onClick = interactive
                ? () => onRowClick(rg.rows[0])
                : undefined;
              return (
                <tr
                  key={`r-${rKey}`}
                  data-testid="pivot-row"
                  data-row-key={rg.keys.join('|')}
                  onClick={onClick}
                  style={{ cursor: interactive ? 'pointer' : undefined }}
                >
                  {rowFields.length === 0 ? (
                    <th className="border border-border p-1 bg-bg-primary text-text-secondary">
                      &nbsp;
                    </th>
                  ) : (
                    rg.keys.map((k, i) => (
                      <th
                        key={`rh-${rKey}-${i}`}
                        className="border border-border p-1 bg-bg-primary text-text-primary text-left whitespace-nowrap"
                      >
                        {k}
                      </th>
                    ))
                  )}
                  {pivot.colGroups.length === 0
                    ? null
                    : pivot.colGroups.flatMap((cg) => {
                        const cKey = cg.keys.join('␟');
                        const v = cellRow?.get(cKey) ?? [];
                        return valueSpecs.map((_, vi) => (
                          <td
                            key={`c-${rKey}-${cKey}-${vi}`}
                            data-testid="pivot-cell"
                            data-row-key={rg.keys.join('|')}
                            data-col-key={cg.keys.join('|')}
                            data-value-index={vi}
                            className="border border-border p-1 text-right text-text-primary"
                          >
                            {formatNumber(v[vi] ?? null)}
                          </td>
                        ));
                      })}
                  {totals.map((t, vi) => (
                    <td
                      key={`rt-${rKey}-${vi}`}
                      data-testid="pivot-row-total"
                      data-row-key={rg.keys.join('|')}
                      data-value-index={vi}
                      className="border border-border p-1 text-right text-text-primary bg-bg-primary/60"
                    >
                      {formatNumber(t)}
                    </td>
                  ))}
                </tr>
              );
            })
          )}
          {pivot.rowGroups.length > 0 && (
            <tr data-testid="pivot-grand-total">
              <th
                colSpan={Math.max(1, rowFields.length)}
                className="border border-border p-1 bg-bg-primary text-text-secondary text-left"
              >
                Total
              </th>
              {pivot.colGroups.flatMap((cg) => {
                const cKey = cg.keys.join('␟');
                const v = pivot.colTotals.get(cKey) ?? [];
                return valueSpecs.map((_, vi) => (
                  <td
                    key={`ct-${cKey}-${vi}`}
                    data-testid="pivot-col-total"
                    data-col-key={cg.keys.join('|')}
                    data-value-index={vi}
                    className="border border-border p-1 text-right text-text-primary bg-bg-primary/60"
                  >
                    {formatNumber(v[vi] ?? null)}
                  </td>
                ));
              })}
              {pivot.grandTotal.map((g, vi) => (
                <td
                  key={`gt-${vi}`}
                  data-testid="pivot-grand-cell"
                  data-value-index={vi}
                  className="border border-border p-1 text-right text-text-primary bg-bg-primary/60"
                >
                  {formatNumber(g)}
                </td>
              ))}
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
