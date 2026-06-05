// VTX-020 + VTX-021: right-side selection sidebar.
//
// Renders nothing when the selection set is empty. With one selected
// node, shows a 4-tab panel: Properties / Series / Linked Events /
// Derived Funcs (VTX-021). With two or more, shows a count + the list
// of selected labels (batch mode, VTX-020).

import { useMemo, useState } from 'react';

import type { SelectionState } from '../features/vertex/selections/selectionState';
import type { ExtendedLabel } from '../features/vertex/render/extendedLabels';
import { useGetObject, useObjectActivity } from '../hooks/useObjects';
import { useTimeSeriesPoints } from '../hooks/useTimeSeries';
import { VertexMiniSparkline } from './VertexMiniSparkline';

export interface VertexObjectSummary {
  rid: string;
  /** Human label (typically `properties.name` or fallback to rid). */
  label: string;
  /** Flat property bag pulled from the SystemGraph payload. */
  properties: Record<string, unknown>;
  /** Ontology api name (when the layer carried `ontology: <apiName>`). */
  ontologyApiName?: string;
  /** Object-type api name (e.g. "Airport"). */
  objectType?: string;
  /** Primary key — explicit `properties.primaryKey` or last `.`-segment of rid. */
  primaryKey?: string;
}

export interface VertexSelectionSidebarProps {
  selection: SelectionState;
  objectsByRid: ReadonlyMap<string, VertexObjectSummary>;
  /**
   * Optional per-rid extended labels. Series tab pulls `timeSeries`
   * labels; Derived Funcs tab pulls `measure` labels.
   */
  extendedLabelsByRid?: ReadonlyMap<string, ExtendedLabel[]>;
}

type TabId = 'properties' | 'series' | 'linkedEvents' | 'derivedFuncs';

const TABS: Array<{ id: TabId; label: string }> = [
  { id: 'properties', label: 'Properties' },
  { id: 'series', label: 'Series' },
  { id: 'linkedEvents', label: 'Linked Events' },
  { id: 'derivedFuncs', label: 'Derived Funcs' },
];

function stringifyValue(v: unknown): string {
  if (v === null || v === undefined) return '—';
  if (typeof v === 'string') return v === '' ? '—' : v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  try {
    return JSON.stringify(v);
  } catch {
    return '—';
  }
}

export function VertexSelectionSidebar({
  selection,
  objectsByRid,
  extendedLabelsByRid,
}: VertexSelectionSidebarProps) {
  if (selection.size === 0) return null;

  const rids = Array.from(selection);
  const isSingle = rids.length === 1;

  return (
    <aside
      data-testid="vertex-selection-sidebar"
      className="flex h-full w-72 flex-col border-l border-zinc-800 bg-zinc-950 text-xs text-zinc-100"
    >
      {isSingle ? (
        <SingleObjectPanel
          object={objectsByRid.get(rids[0]) ?? null}
          rid={rids[0]}
          extendedLabels={extendedLabelsByRid?.get(rids[0]) ?? []}
        />
      ) : (
        <BatchPanel rids={rids} objectsByRid={objectsByRid} />
      )}
    </aside>
  );
}

function SingleObjectPanel({
  object,
  rid,
  extendedLabels,
}: {
  object: VertexObjectSummary | null;
  rid: string;
  extendedLabels: ExtendedLabel[];
}) {
  const [active, setActive] = useState<TabId>('properties');
  const heading = object?.label ?? rid;
  return (
    <>
      <header
        data-testid="vertex-selection-sidebar-header"
        className="border-b border-zinc-800 px-3 py-2 font-mono text-sm"
      >
        {heading}
      </header>
      <nav
        data-testid="vertex-sidebar-tabs"
        aria-label="Selection tabs"
        className="flex border-b border-zinc-800"
      >
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            data-testid={`vertex-sidebar-tab-${t.id}`}
            data-active={active === t.id ? 'true' : 'false'}
            onClick={() => setActive(t.id)}
            className={
              'flex-1 border-b-2 px-2 py-1 text-[11px] ' +
              (active === t.id
                ? 'border-blue-500 text-blue-300'
                : 'border-transparent text-zinc-400 hover:text-zinc-200')
            }
          >
            {t.label}
          </button>
        ))}
      </nav>
      <div className="flex-1 overflow-y-auto p-2">
        {active === 'properties' && (
          <PropertiesTab object={object} rid={rid} />
        )}
        {active === 'series' && (
          <SeriesTab object={object} rid={rid} labels={extendedLabels} />
        )}
        {active === 'linkedEvents' && (
          <LinkedEventsTab object={object} rid={rid} />
        )}
        {active === 'derivedFuncs' && (
          <DerivedFuncsTab labels={extendedLabels} />
        )}
      </div>
    </>
  );
}

function PropertiesTab({
  object,
  rid,
}: {
  object: VertexObjectSummary | null;
  rid: string;
}) {
  const ont = object?.ontologyApiName ?? '';
  const ot = object?.objectType ?? '';
  const pk = object?.primaryKey ?? '';
  const query = useGetObject(ont, ot, pk);
  const fresh = query.data;
  // Merge: prefer OSS fresh values, fall back to snapshot properties.
  // Strip wire-only fields (__rid / __apiName / __primaryKey) so they
  // don't pollute the displayed rows.
  const merged = useMemo(() => {
    const base: Record<string, unknown> = { ...(object?.properties ?? {}) };
    if (fresh) {
      for (const [k, v] of Object.entries(fresh)) {
        if (k.startsWith('__')) continue;
        base[k] = v;
      }
    }
    return base;
  }, [object?.properties, fresh]);
  const entries = Object.entries(merged);
  return (
    <>
      <div className="mb-1 text-[10px] uppercase tracking-wide text-zinc-500">
        Properties
      </div>
      {query.isLoading && (
        <div
          data-testid="vertex-sidebar-properties-loading"
          className="text-zinc-500"
        >
          Loading…
        </div>
      )}
      {entries.length === 0 && !query.isLoading && (
        <div className="text-zinc-500">No properties available.</div>
      )}
      <dl data-testid="vertex-sidebar-properties-list">
        {entries.map(([k, v]) => (
          <div
            key={k}
            data-testid={`vertex-selection-sidebar-prop-${k}`}
            className="flex items-baseline justify-between gap-2 border-b border-zinc-900 py-1"
          >
            <dt className="text-zinc-400">{k}</dt>
            <dd className="font-mono text-zinc-100">{stringifyValue(v)}</dd>
          </div>
        ))}
      </dl>
      {/* Hide the rid in plain sight so future debug overlays can find it. */}
      <input type="hidden" value={rid} readOnly />
    </>
  );
}

function SeriesTab({
  object,
  rid,
  labels,
}: {
  object: VertexObjectSummary | null;
  rid: string;
  labels: ExtendedLabel[];
}) {
  const tsLabels = labels.filter((l) => l.kind === 'timeSeries');
  if (tsLabels.length === 0) {
    return (
      <div data-testid="vertex-sidebar-series-empty" className="text-zinc-500">
        No time-series labels on this object.
      </div>
    );
  }
  return (
    <div data-testid="vertex-sidebar-series-list">
      {tsLabels.map((l) => (
        <SeriesRow key={l.key} object={object} rid={rid} label={l} />
      ))}
    </div>
  );
}

function SeriesRow({
  object,
  rid,
  label,
}: {
  object: VertexObjectSummary | null;
  rid: string;
  label: ExtendedLabel;
}) {
  const property = label.key.split(':')[1] ?? label.label;
  const ont = object?.ontologyApiName ?? '';
  const ot = object?.objectType ?? '';
  const pk = object?.primaryKey ?? '';
  const query = useTimeSeriesPoints({
    ontologyApiName: ont,
    objectType: ot,
    primaryKey: pk,
    property,
  });
  const values = useMemo<number[]>(() => {
    const data = query.data;
    if (!Array.isArray(data)) return [];
    const out: number[] = [];
    for (const pt of data) {
      const v = (pt as { value: unknown }).value;
      if (typeof v === 'number' && Number.isFinite(v)) out.push(v);
    }
    return out;
  }, [query.data]);
  return (
    <div
      data-testid={`vertex-sidebar-series-row-${label.label}`}
      data-rid={rid}
      className="border-b border-zinc-900 py-1"
    >
      <div className="text-zinc-300">{label.label}</div>
      {values.length >= 2 ? (
        <VertexMiniSparkline
          values={values}
          testId="vertex-sidebar-series-sparkline"
          className="mt-1 h-6 w-full"
        />
      ) : (
        <span
          data-testid="vertex-sidebar-series-sparkline"
          className="font-mono text-zinc-500"
        >
          ▁▂▃▄▅
        </span>
      )}
    </div>
  );
}

function LinkedEventsTab({
  object,
  rid,
}: {
  object: VertexObjectSummary | null;
  rid: string;
}) {
  const ont = object?.ontologyApiName ?? '';
  const ot = object?.objectType ?? '';
  const pk = object?.primaryKey ?? '';
  const query = useObjectActivity({
    ontologyApiName: ont,
    objectType: ot,
    primaryKey: pk,
    pageSize: 50,
  });
  if (!ont || !ot || !pk) {
    return (
      <div data-testid="vertex-sidebar-linked-events-unavailable" className="text-zinc-500">
        No object metadata available — cannot fetch events.
      </div>
    );
  }
  if (query.isLoading) {
    return (
      <div data-testid="vertex-sidebar-linked-events-loading" className="text-zinc-500">
        Loading events…
      </div>
    );
  }
  if (query.isError) {
    return (
      <div data-testid="vertex-sidebar-linked-events-error" className="text-red-400">
        Failed to load events.
      </div>
    );
  }
  const pages = query.data?.pages ?? [];
  const entries = pages.flatMap((p) => p.data ?? []).slice(0, 50);
  if (entries.length === 0) {
    return (
      <div data-testid="vertex-sidebar-linked-events-empty" className="text-zinc-500">
        No recent events.
      </div>
    );
  }
  return (
    <ul
      data-testid="vertex-sidebar-linked-events-list"
      data-rid={rid}
      className="space-y-1"
    >
      {entries.map((e) => (
        <li
          key={e.id}
          data-testid={`vertex-sidebar-linked-event-${e.id}`}
          className="border-b border-zinc-900 py-1"
        >
          <div className="flex items-baseline justify-between gap-2">
            <span className="text-zinc-300">{e.editType}</span>
            <span className="font-mono text-[10px] text-zinc-500">v{e.version}</span>
          </div>
          <div className="text-[10px] text-zinc-500">{e.recordedAt}</div>
        </li>
      ))}
    </ul>
  );
}

function DerivedFuncsTab({ labels }: { labels: ExtendedLabel[] }) {
  const measures = labels.filter((l) => l.kind === 'measure');
  if (measures.length === 0) {
    return (
      <div data-testid="vertex-sidebar-derived-funcs-empty" className="text-zinc-500">
        No derived functions attached.
      </div>
    );
  }
  return (
    <ul data-testid="vertex-sidebar-derived-funcs-list" className="space-y-1">
      {measures.map((m) => (
        <li
          key={m.key}
          data-testid={`vertex-sidebar-derived-func-${m.label}`}
          className="border-b border-zinc-900 py-1"
        >
          <div className="text-zinc-200">{m.label}</div>
          <div className="text-[10px] text-zinc-500">Awaiting evaluation</div>
        </li>
      ))}
    </ul>
  );
}

function BatchPanel({
  rids,
  objectsByRid,
}: {
  rids: string[];
  objectsByRid: ReadonlyMap<string, VertexObjectSummary>;
}) {
  return (
    <>
      <header
        data-testid="vertex-selection-sidebar-header"
        className="border-b border-zinc-800 px-3 py-2"
      >
        <div className="text-sm">
          <span data-testid="vertex-selection-sidebar-count" className="font-mono">
            {rids.length}
          </span>{' '}
          objects selected
        </div>
      </header>
      <div data-testid="vertex-selection-sidebar-batch" className="flex-1 overflow-y-auto p-2">
        <ul>
          {rids.map((rid) => {
            const summary = objectsByRid.get(rid);
            return (
              <li
                key={rid}
                data-testid={`vertex-selection-sidebar-item-${rid}`}
                className="border-b border-zinc-900 py-1 font-mono"
              >
                {summary?.label ?? rid}
              </li>
            );
          })}
        </ul>
      </div>
    </>
  );
}
