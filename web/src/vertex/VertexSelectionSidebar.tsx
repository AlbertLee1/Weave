// VTX-020: right-side selection sidebar.
//
// Renders nothing when the selection set is empty. With one selected
// node, shows a header + properties bag. With two or more, shows a
// count + the list of selected labels (batch mode).
//
// The richer per-tab sidebar (Properties / Series / Linked Events /
// Derived Funcs) lands in VTX-021; this story only delivers the open /
// close + properties dump the BDD demands.

import type { SelectionState } from '../features/vertex/selections/selectionState';

export interface VertexObjectSummary {
  rid: string;
  /** Human label (typically `properties.name` or fallback to rid). */
  label: string;
  /** Flat property bag pulled from the SystemGraph payload. */
  properties: Record<string, unknown>;
}

export interface VertexSelectionSidebarProps {
  selection: SelectionState;
  objectsByRid: ReadonlyMap<string, VertexObjectSummary>;
}

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

export function VertexSelectionSidebar({ selection, objectsByRid }: VertexSelectionSidebarProps) {
  if (selection.size === 0) return null;

  const rids = Array.from(selection);
  const isSingle = rids.length === 1;

  return (
    <aside
      data-testid="vertex-selection-sidebar"
      className="flex h-full w-72 flex-col border-l border-zinc-800 bg-zinc-950 text-xs text-zinc-100"
    >
      {isSingle ? (
        <SingleObjectPanel object={objectsByRid.get(rids[0]) ?? null} rid={rids[0]} />
      ) : (
        <BatchPanel rids={rids} objectsByRid={objectsByRid} />
      )}
    </aside>
  );
}

function SingleObjectPanel({ object, rid }: { object: VertexObjectSummary | null; rid: string }) {
  const heading = object?.label ?? rid;
  const props = object?.properties ?? {};
  const entries = Object.entries(props);
  return (
    <>
      <header
        data-testid="vertex-selection-sidebar-header"
        className="border-b border-zinc-800 px-3 py-2 font-mono text-sm"
      >
        {heading}
      </header>
      <div className="flex-1 overflow-y-auto p-2">
        <div className="mb-1 text-[10px] uppercase tracking-wide text-zinc-500">Properties</div>
        {entries.length === 0 && (
          <div className="text-zinc-500">No properties available.</div>
        )}
        <dl>
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
      </div>
    </>
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
