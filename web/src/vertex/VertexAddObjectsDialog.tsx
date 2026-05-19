// VTX-027 — "+ Add objects" dialog.
//
// User journey: TopBar button opens this dialog → ObjectType dropdown
// (driven by useObjectTypes against the supplied ontology) + free-text
// search box (driven by useSearchObjects with a `contains` filter on the
// chosen object type's titleProperty, defaulting to `name`). Each result
// is a checkbox row; the Add button hands the selected rids back via
// `onAdd`. The page-level state then folds the new objects into the
// rendered projection via `mergeAddedNodes`.
//
// Search fires on every keystroke — no debouncing yet; OSS search is
// cursor-paginated and TanStack Query dedupes identically-shaped queries
// so the typical "user types 'JFK'" flow only burns 3 round-trips. We can
// add a debounce later if the request volume becomes a concern.
//
// The dialog is a plain absolutely-positioned modal — no Radix dependency
// since the rest of the workspace uses raw Tailwind for popovers
// (LayoutMenu, VertexNodeContextMenu).

import { useMemo, useState } from 'react';

import { useObjectTypes } from '../hooks/useObjectTypes';
import { useSearchObjects } from '../hooks/useObjects';
import type { WireObject } from '../api/types';

export interface VertexAddObjectsDialogProps {
  open: boolean;
  ontologyApiName: string;
  /** Optional default object type api name (e.g. derived from the active layer). */
  defaultObjectType?: string;
  onClose: () => void;
  onAdd: (objects: AddedObjectInput[]) => void;
}

export interface AddedObjectInput {
  rid: string;
  label: string;
  ontologyApiName: string;
  objectType: string;
  primaryKey?: string;
}

function pickLabel(obj: WireObject, titleProperty: string): string {
  const v = obj[titleProperty];
  if (typeof v === 'string' && v !== '') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  if (typeof obj.__primaryKey === 'string' && obj.__primaryKey !== '') return obj.__primaryKey;
  if (typeof obj.__primaryKey === 'number') return String(obj.__primaryKey);
  return obj.__rid;
}

function pickPrimaryKey(obj: WireObject): string | undefined {
  if (typeof obj.__primaryKey === 'string' && obj.__primaryKey !== '') {
    return obj.__primaryKey;
  }
  if (typeof obj.__primaryKey === 'number') return String(obj.__primaryKey);
  return undefined;
}

export function VertexAddObjectsDialog({
  open,
  ontologyApiName,
  defaultObjectType,
  onClose,
  onAdd,
}: VertexAddObjectsDialogProps) {
  const objectTypesQ = useObjectTypes(ontologyApiName);
  // Two-layer state pattern (mirrors VertexWorkspacePage's pinned seed/diff):
  // `userPickedType` holds the explicit dropdown selection ('' = none yet)
  // and the effective type derives the default from props + the loaded
  // object-type list. Avoids a setState-in-effect for the "default once
  // data arrives" case (lint rule react-hooks/set-state-in-effect).
  const [userPickedType, setUserPickedType] = useState<string>('');
  const [term, setTerm] = useState<string>('');
  const [checked, setChecked] = useState<ReadonlySet<string>>(() => new Set());

  const type = useMemo<string>(() => {
    if (userPickedType !== '') return userPickedType;
    const list = objectTypesQ.data;
    if (!list || list.length === 0) return '';
    if (defaultObjectType && list.some((t) => t.apiName === defaultObjectType)) {
      return defaultObjectType;
    }
    return list[0].apiName;
  }, [userPickedType, defaultObjectType, objectTypesQ.data]);

  // Re-mounting the dialog (parent gates `{open && <Dialog />}`) gives a
  // fresh state bag on each open — no reset effect required.

  const titleProperty = useMemo(() => {
    const t = objectTypesQ.data?.find((x) => x.apiName === type);
    return t?.titleProperty ?? 'name';
  }, [type, objectTypesQ.data]);

  const searchQ = useSearchObjects({
    ontologyApiName,
    objectType: type,
    pageSize: 50,
    select: ['__rid', '__primaryKey', titleProperty],
    where:
      term !== ''
        ? { type: 'contains', field: titleProperty, value: term }
        : undefined,
    enabled: open && type !== '',
  });

  if (!open) return null;

  const results = searchQ.data?.data ?? [];
  const toggle = (rid: string) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(rid)) next.delete(rid);
      else next.add(rid);
      return next;
    });
  };

  const handleAdd = () => {
    const picked = results
      .filter((o) => checked.has(o.__rid))
      .map<AddedObjectInput>((o) => ({
        rid: o.__rid,
        label: pickLabel(o, titleProperty),
        ontologyApiName,
        objectType: type,
        primaryKey: pickPrimaryKey(o),
      }));
    onAdd(picked);
    onClose();
  };

  return (
    <div
      data-testid="vertex-add-objects-dialog"
      role="dialog"
      aria-label="Add objects"
      className="absolute inset-0 z-40 flex items-center justify-center bg-black/40"
    >
      <div className="flex w-[420px] max-h-[80vh] flex-col rounded border border-zinc-700 bg-zinc-950 p-4 text-xs text-zinc-100 shadow-2xl">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold">Add objects</h2>
          <button
            type="button"
            data-testid="vertex-add-objects-close"
            onClick={onClose}
            className="rounded px-2 py-1 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
            aria-label="Close"
          >
            ×
          </button>
        </div>
        <label className="mb-2 block">
          <span className="mb-1 block text-zinc-400">Object type</span>
          <select
            data-testid="vertex-add-objects-type"
            value={type}
            onChange={(e) => {
              setUserPickedType(e.target.value);
              setChecked(new Set());
            }}
            className="w-full rounded border border-zinc-700 bg-zinc-900 px-2 py-1 text-zinc-100"
          >
            {(objectTypesQ.data ?? []).map((t) => (
              <option
                key={t.apiName}
                value={t.apiName}
                data-testid={`vertex-add-objects-type-option-${t.apiName}`}
              >
                {t.displayName || t.apiName}
              </option>
            ))}
          </select>
        </label>
        <label className="mb-2 block">
          <span className="mb-1 block text-zinc-400">Search</span>
          <input
            type="text"
            data-testid="vertex-add-objects-search"
            value={term}
            onChange={(e) => setTerm(e.target.value)}
            placeholder="Filter by name…"
            className="w-full rounded border border-zinc-700 bg-zinc-900 px-2 py-1 text-zinc-100"
          />
        </label>
        <div
          data-testid="vertex-add-objects-results"
          className="mb-3 flex-1 overflow-y-auto rounded border border-zinc-800 bg-zinc-900"
        >
          {searchQ.isLoading && (
            <div data-testid="vertex-add-objects-results-loading" className="p-2 text-zinc-500">
              Loading…
            </div>
          )}
          {!searchQ.isLoading && results.length === 0 && (
            <div data-testid="vertex-add-objects-results-empty" className="p-2 text-zinc-500">
              No matches.
            </div>
          )}
          {results.map((obj) => {
            const rid = obj.__rid;
            const label = pickLabel(obj, titleProperty);
            const isChecked = checked.has(rid);
            return (
              <label
                key={rid}
                className="flex cursor-pointer items-center gap-2 px-2 py-1 hover:bg-zinc-800"
              >
                <input
                  type="checkbox"
                  data-testid={`vertex-add-objects-row-${rid}`}
                  checked={isChecked}
                  onChange={() => toggle(rid)}
                />
                <span className="flex-1 truncate" title={rid}>
                  {label}
                </span>
              </label>
            );
          })}
        </div>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="vertex-add-objects-cancel"
            onClick={onClose}
            className="rounded border border-zinc-700 bg-zinc-900 px-3 py-1 hover:bg-zinc-800"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="vertex-add-objects-add"
            disabled={checked.size === 0}
            onClick={handleAdd}
            className="rounded bg-blue-600 px-3 py-1 text-white hover:bg-blue-500 disabled:cursor-not-allowed disabled:bg-zinc-700"
          >
            Add{checked.size > 0 ? ` (${checked.size})` : ''}
          </button>
        </div>
      </div>
    </div>
  );
}
