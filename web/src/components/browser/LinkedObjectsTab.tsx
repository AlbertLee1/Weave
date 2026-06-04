import { Fragment, useMemo, useState } from 'react';
import type { LinkProperty, LinkType } from '../../api/types';
import { useLinkedObjects } from '../../hooks/useObjects';
import {
  useLinkProperties,
  useUpdateLinkEdgeProperties,
} from '../../hooks/useLinkProperties';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';

interface LinkedObjectsTabProps {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  linkTypes: LinkType[];
}

// US-497 — coerce raw form-input strings into the typed shape the backend
// validates against. Blank optional fields are dropped so nullable
// LinkProperty rows stay unset rather than being persisted as "" / 0.
function coerceEdgeValues(
  schema: LinkProperty[],
  raw: Record<string, string>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const lp of schema) {
    const text = (raw[lp.apiName] ?? '').trim();
    if (text === '') {
      // Only required fields make it through with an explicit empty value;
      // the backend then rejects the missing-required-property case with
      // its own error message. Optional fields are simply dropped.
      if (!lp.isNullable) {
        out[lp.apiName] = '';
      }
      continue;
    }
    switch (lp.baseType) {
      case 'long':
      case 'integer':
      case 'short':
      case 'byte': {
        const n = Number(text);
        out[lp.apiName] = Number.isFinite(n) ? Math.trunc(n) : text;
        break;
      }
      case 'double':
      case 'float':
      case 'decimal': {
        const n = Number(text);
        out[lp.apiName] = Number.isFinite(n) ? n : text;
        break;
      }
      case 'boolean':
        out[lp.apiName] = text.toLowerCase() === 'true';
        break;
      default:
        out[lp.apiName] = text;
    }
  }
  return out;
}

function EdgePropertiesForm({
  ontologyApiName,
  linkType,
  sourcePk,
  targetPk,
  onClose,
}: {
  ontologyApiName: string;
  linkType: LinkType;
  sourcePk: string;
  targetPk: string;
  onClose: () => void;
}) {
  const { data: schema, isLoading } = useLinkProperties(
    ontologyApiName,
    linkType.rid,
  );
  const mutation = useUpdateLinkEdgeProperties(ontologyApiName, linkType.rid);
  const [values, setValues] = useState<Record<string, string>>({});

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const coerced = coerceEdgeValues(schema ?? [], values);
    mutation.mutate(
      { sourcePk, targetPk, values: coerced },
      { onSuccess: () => onClose() },
    );
  };

  return (
    <form
      data-testid={`edge-properties-form-${targetPk}`}
      onSubmit={onSubmit}
      className="mt-2 mb-1 p-3 bg-bg-tertiary border border-border rounded space-y-2"
    >
      <div className="text-xs font-sans font-medium text-text-secondary">
        Edge properties for {linkType.displayName} → {targetPk}
      </div>
      {isLoading && <LoadingSpinner size="sm" />}
      {!isLoading && (schema ?? []).length === 0 && (
        <p className="text-xs font-mono text-text-muted">
          No edge properties declared on this link type.
        </p>
      )}
      {!isLoading &&
        (schema ?? []).map((lp) => {
          const fieldId = `edge-prop-${targetPk}-${lp.apiName}`;
          return (
            <div key={lp.apiName} className="flex flex-col gap-1">
              <label
                htmlFor={fieldId}
                className="text-xs font-mono text-text-secondary"
              >
                {lp.displayName || lp.apiName}
                {!lp.isNullable && (
                  <span className="ml-1 text-accent-red">*</span>
                )}
                <span className="ml-2 text-text-muted">({lp.baseType})</span>
              </label>
              <input
                id={fieldId}
                type={
                  lp.baseType === 'long' ||
                  lp.baseType === 'integer' ||
                  lp.baseType === 'short' ||
                  lp.baseType === 'byte' ||
                  lp.baseType === 'double' ||
                  lp.baseType === 'float' ||
                  lp.baseType === 'decimal'
                    ? 'number'
                    : 'text'
                }
                value={values[lp.apiName] ?? ''}
                onChange={(e) =>
                  setValues((prev) => ({
                    ...prev,
                    [lp.apiName]: e.target.value,
                  }))
                }
                className="text-xs font-mono px-2 py-1 bg-bg-primary border border-border rounded"
              />
            </div>
          );
        })}
      {mutation.isError && (
        <p className="text-xs font-mono text-accent-red">
          Save failed: {(mutation.error as Error)?.message ?? 'unknown error'}
        </p>
      )}
      <div className="flex items-center gap-2 pt-1">
        <button
          type="submit"
          disabled={mutation.isPending}
          className="text-xs font-sans px-2 py-1 bg-accent-cyan text-bg-primary rounded disabled:opacity-50"
        >
          {mutation.isPending ? 'Saving…' : 'Save'}
        </button>
        <button
          type="button"
          onClick={onClose}
          className="text-xs font-sans px-2 py-1 border border-border text-text-secondary rounded"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

function LinkedObjectGroup({
  ontologyApiName,
  objectType,
  primaryKey,
  linkType,
}: {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  linkType: LinkType;
}) {
  // Traversal direction toggle. "forward" walks the link source -> target
  // (its declared direction); "reverse" walks target -> source so the user
  // can discover incoming/reverse links. The backend reads this off the
  // `?direction=` query param (pkg/oss/handlers.go) and the hook re-fetches
  // whenever it changes because direction is part of the query key.
  const [direction, setDirection] = useState<'forward' | 'reverse'>('forward');
  const { data, isLoading } = useLinkedObjects({
    ontologyApiName,
    objectType,
    primaryKey,
    linkType: linkType.apiName,
    pageSize: 10,
    direction,
  });
  // US-497 — only MANY_TO_MANY links carry edge_properties in
  // link_edges; ONE_TO_* edges have nowhere to store them so the edit
  // affordance is gated on cardinality, matching the backend resolver
  // at cmd/server/edge_properties_resolver.go:71.
  //
  // Edge-property edits PUT to /edges/{sourcePk}/{targetPk}/properties with
  // sourcePk=this object and targetPk=the row. That mapping only holds while
  // walking forward; in reverse the rows are incoming objects (the real edge
  // is row -> this object), so we suppress the edit affordance rather than
  // write to an inverted/nonexistent edge.
  const supportsEdgeProperties =
    linkType.cardinality === 'MANY_TO_MANY' && direction === 'forward';
  const [editingTargetPk, setEditingTargetPk] = useState<string | null>(null);

  const visibleKeys = useMemo(() => {
    if (!data?.data || data.data.length === 0) return [] as string[];
    return Object.keys(data.data[0])
      .filter((k) => !k.startsWith('__') || k === '__primaryKey')
      .slice(0, 5);
  }, [data]);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <h4 className="text-xs font-sans font-medium text-text-primary">
          {linkType.displayName}
        </h4>
        <span className="text-xs font-mono text-text-muted">
          {linkType.linkedObjectTypeApiName}
        </span>
        {data?.totalCount && (
          <span className="text-xs font-mono text-text-secondary">
            ({data.totalCount})
          </span>
        )}
        <div
          role="group"
          aria-label={`Traversal direction for ${linkType.displayName}`}
          className="ml-auto inline-flex overflow-hidden rounded border border-border"
        >
          {(['forward', 'reverse'] as const).map((dir) => {
            const active = direction === dir;
            return (
              <button
                key={dir}
                type="button"
                data-testid={`link-direction-${dir}-${linkType.apiName}`}
                aria-pressed={active}
                onClick={() => setDirection(dir)}
                className={`text-xs font-sans px-2 py-0.5 ${
                  active
                    ? 'bg-accent-cyan text-bg-primary'
                    : 'bg-bg-tertiary text-text-secondary hover:text-text-primary'
                }`}
              >
                {dir === 'forward' ? 'Forward' : 'Reverse'}
              </button>
            );
          })}
        </div>
      </div>

      {isLoading && <LoadingSpinner size="sm" />}

      {!isLoading && (!data?.data || data.data.length === 0) && (
        <p className="text-xs text-text-muted font-mono py-2">No linked objects</p>
      )}

      {!isLoading && data?.data && data.data.length > 0 && (
        <div className="overflow-x-auto border border-border rounded">
          <table className="w-full text-xs" data-testid="linked-objects-table">
            <thead>
              <tr className="bg-bg-tertiary border-b border-border">
                {visibleKeys.map((key) => (
                  <th
                    key={key}
                    className="px-2 py-1.5 text-left font-mono text-text-secondary font-medium"
                  >
                    {key === '__primaryKey' ? 'Primary Key' : key}
                  </th>
                ))}
                {supportsEdgeProperties && (
                  <th className="px-2 py-1.5 text-right font-mono text-text-secondary font-medium">
                    Edge
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {data.data.map((obj, i) => {
                const rowPk = String(obj.__primaryKey ?? '');
                const isEditing = editingTargetPk === rowPk;
                return (
                  <Fragment key={rowPk || `row-${i}`}>
                    <tr className="border-b border-border last:border-b-0">
                      {visibleKeys.map((key) => (
                        <td
                          key={key}
                          className={`px-2 py-1.5 font-mono ${
                            key === '__primaryKey'
                              ? 'text-accent-cyan'
                              : 'text-text-primary'
                          }`}
                        >
                          {obj[key] === null || obj[key] === undefined
                            ? ''
                            : typeof obj[key] === 'object'
                              ? JSON.stringify(obj[key])
                              : String(obj[key])}
                        </td>
                      ))}
                      {supportsEdgeProperties && (
                        <td className="px-2 py-1.5 text-right">
                          <button
                            type="button"
                            data-testid={`edit-edge-properties-${rowPk}`}
                            onClick={() =>
                              setEditingTargetPk(isEditing ? null : rowPk)
                            }
                            className="text-xs font-sans px-2 py-0.5 border border-border rounded text-text-secondary hover:text-text-primary"
                          >
                            {isEditing ? 'Close' : 'Edit edge'}
                          </button>
                        </td>
                      )}
                    </tr>
                    {supportsEdgeProperties && isEditing && (
                      <tr>
                        <td colSpan={visibleKeys.length + 1} className="p-0">
                          <EdgePropertiesForm
                            ontologyApiName={ontologyApiName}
                            linkType={linkType}
                            sourcePk={primaryKey}
                            targetPk={rowPk}
                            onClose={() => setEditingTargetPk(null)}
                          />
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export function LinkedObjectsTab({
  ontologyApiName,
  objectType,
  primaryKey,
  linkTypes,
}: LinkedObjectsTabProps) {
  if (linkTypes.length === 0) {
    return (
      <EmptyState
        title="No link types"
        description="This object type has no outgoing link types defined."
      />
    );
  }

  return (
    <div className="space-y-4">
      {linkTypes.map((lt) => (
        <LinkedObjectGroup
          key={lt.rid}
          ontologyApiName={ontologyApiName}
          objectType={objectType}
          primaryKey={primaryKey}
          linkType={lt}
        />
      ))}
    </div>
  );
}
