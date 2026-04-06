import { useMemo } from 'react';
import type { ObjectSetDefinition } from '../../api/types';
import type { SavedObjectSet } from '../../lib/objectSetBuilder';
import { definitionToNode, validateNode } from '../../lib/objectSetBuilder';
import { ObjectSetBuilder } from './ObjectSetBuilder';

interface ObjectSetComposerProps {
  objectTypes: string[];
  value: ObjectSetDefinition;
  onChange: (def: ObjectSetDefinition) => void;
  onExecute: () => void;
  onSaveAs: () => void;
  onShare?: () => void;
  savedObjectSets: SavedObjectSet[];
  onLoadSaved: (saved: SavedObjectSet) => void;
  onDeleteSaved: (id: string) => void;
}

// Walks the definition and collects the implied object type at each branch of
// a union / intersect / subtract. Returns the set of distinct types found
// across the immediate children. Used to surface the type-mismatch warning
// from the design doc.
function findSetOpBranchTypes(
  def: ObjectSetDefinition,
): { node: ObjectSetDefinition; types: string[] }[] {
  const out: { node: ObjectSetDefinition; types: string[] }[] = [];
  walk(def, out);
  return out;
}

function walk(
  def: ObjectSetDefinition,
  out: { node: ObjectSetDefinition; types: string[] }[],
): void {
  switch (def.type) {
    case 'union':
    case 'intersect':
    case 'subtract': {
      const types = def.objectSets.map(resolveType).filter((t) => !!t);
      const distinct = Array.from(new Set(types));
      if (distinct.length > 1) {
        out.push({ node: def, types: distinct });
      }
      for (const child of def.objectSets) walk(child, out);
      return;
    }
    case 'filter':
    case 'searchAround':
    case 'withProperties':
    case 'nearestNeighbors':
      walk(def.objectSet, out);
      return;
    default:
      return;
  }
}

// resolveType returns the leaf object type for a definition, or '' if it
// cannot be determined statically (e.g. searchAround changes type).
function resolveType(def: ObjectSetDefinition): string {
  switch (def.type) {
    case 'base':
      return def.objectType;
    case 'filter':
    case 'withProperties':
    case 'nearestNeighbors':
      return resolveType(def.objectSet);
    case 'union':
    case 'intersect':
    case 'subtract':
      return def.objectSets.length > 0 ? resolveType(def.objectSets[0]) : '';
    case 'searchAround':
      return ''; // changes type — backend reports the resolved type
    case 'reference':
      return '';
  }
}

export function ObjectSetComposer({
  objectTypes,
  value,
  onChange,
  onExecute,
  onSaveAs,
  onShare,
  savedObjectSets,
  onLoadSaved,
  onDeleteSaved,
}: ObjectSetComposerProps) {
  const errors = useMemo(() => validateNode(definitionToNode(value)), [value]);
  const branchWarnings = useMemo(() => findSetOpBranchTypes(value), [value]);
  const canExecute = errors.length === 0;

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Action bar */}
      <div className="flex items-center gap-2 px-4 py-3 border-b border-border bg-bg-secondary/40">
        <button
          type="button"
          onClick={onExecute}
          disabled={!canExecute}
          className="bg-accent-cyan text-bg-primary px-4 py-1.5 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          Execute
        </button>
        <button
          type="button"
          onClick={onSaveAs}
          className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
        >
          Save As
        </button>
        {onShare && (
          <button
            type="button"
            onClick={onShare}
            className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
          >
            Share Link
          </button>
        )}
      </div>

      {/* Tree builder */}
      <div className="flex-1 overflow-y-auto p-4 flex flex-col gap-4">
        <div>
          <div className="text-xs font-sans text-text-secondary mb-2 uppercase tracking-wider">
            Definition
          </div>
          <ObjectSetBuilder
            objectTypes={objectTypes}
            value={value}
            onChange={onChange}
          />
        </div>

        {/* Validation errors */}
        {errors.length > 0 && (
          <div className="border border-accent-error/30 bg-accent-error/5 rounded p-3">
            <div className="text-xs font-sans text-accent-error font-medium mb-1">
              Validation
            </div>
            <ul className="text-xs font-mono text-accent-error space-y-1">
              {errors.map((err, i) => (
                <li key={i}>- {err}</li>
              ))}
            </ul>
          </div>
        )}

        {/* Branch type-mismatch warnings */}
        {branchWarnings.length > 0 && (
          <div className="border border-accent-amber/30 bg-accent-amber/5 rounded p-3">
            <div className="text-xs font-sans text-accent-amber font-medium mb-1">
              Warning
            </div>
            <ul className="text-xs font-mono text-accent-amber space-y-1">
              {branchWarnings.map((w, i) => (
                <li key={i}>
                  {w.node.type} branches resolve to different object types ({w.types.join(', ')}); the backend will silently use the first one.
                </li>
              ))}
            </ul>
          </div>
        )}

        {/* Saved object sets */}
        <div>
          <div className="text-xs font-sans text-text-secondary mb-2 uppercase tracking-wider">
            Saved
          </div>
          {savedObjectSets.length === 0 ? (
            <div className="text-xs font-mono text-text-muted">
              No saved object sets yet. Click Save As to keep one.
            </div>
          ) : (
            <ul className="flex flex-col gap-1">
              {savedObjectSets.map((s) => (
                <li
                  key={s.id}
                  className="flex items-center justify-between gap-2 px-2 py-1 border border-border rounded bg-bg-tertiary"
                >
                  <button
                    type="button"
                    onClick={() => onLoadSaved(s)}
                    className="flex-1 text-left text-xs font-mono text-text-primary hover:text-accent-cyan truncate"
                    title={`Created ${s.createdAt}`}
                  >
                    {s.name}
                  </button>
                  <button
                    type="button"
                    onClick={() => onDeleteSaved(s.id)}
                    className="text-xs font-mono text-accent-error hover:text-accent-error/70"
                    aria-label={`delete ${s.name}`}
                  >
                    x
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
