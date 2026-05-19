import type {
  DerivedPropertyDef,
  NearestNeighborsObjectSet,
  ObjectSetDefinition,
  StaticObjectSet,
  WhereClause,
} from '../../api/types';
import {
  isEditableObjectSetType,
  unsupportedObjectSetMessage,
} from '../../lib/objectSetBuilder';

const OBJECT_SET_TYPES = [
  'base',
  'static',
  'filter',
  'union',
  'intersect',
  'subtract',
  'searchAround',
  'withProperties',
  'nearestNeighbors',
] as const;

type SupportedType = (typeof OBJECT_SET_TYPES)[number];

interface ObjectSetBuilderProps {
  objectTypes: string[];
  value: ObjectSetDefinition;
  onChange: (def: ObjectSetDefinition) => void;
  depth?: number;
}

function defaultForType(
  type: SupportedType,
  objectTypes: string[],
): ObjectSetDefinition {
  const firstType = objectTypes[0] ?? '';
  switch (type) {
    case 'base':
      return { type: 'base', objectType: firstType };
    case 'static':
      return { type: 'static', objectType: firstType, primaryKeys: [] };
    case 'filter':
      return {
        type: 'filter',
        objectSet: { type: 'base', objectType: firstType },
        where: { type: 'eq', field: '', value: '' },
      };
    case 'union':
      return {
        type: 'union',
        objectSets: [
          { type: 'base', objectType: firstType },
          { type: 'base', objectType: firstType },
        ],
      };
    case 'intersect':
      return {
        type: 'intersect',
        objectSets: [
          { type: 'base', objectType: firstType },
          { type: 'base', objectType: firstType },
        ],
      };
    case 'subtract':
      return {
        type: 'subtract',
        objectSets: [
          { type: 'base', objectType: firstType },
          { type: 'base', objectType: firstType },
        ],
      };
    case 'searchAround':
      return {
        type: 'searchAround',
        objectSet: { type: 'base', objectType: firstType },
        link: '',
        direction: 'forward',
      };
    case 'withProperties':
      return {
        type: 'withProperties',
        objectSet: { type: 'base', objectType: firstType },
        derivedProperties: [
          {
            name: 'derived',
            link: '',
            direction: 'forward',
            metric: 'count',
          },
        ],
      };
    case 'nearestNeighbors':
      return {
        type: 'nearestNeighbors',
        objectSet: { type: 'base', objectType: firstType },
        propertyIdentifier: { property: { apiName: '' } },
        numNeighbors: 10,
        query: { text: { value: '' } },
      };
  }
}

const DERIVED_METRICS = ['count', 'sum', 'avg', 'min', 'max'] as const;

function DerivedPropertyEditor({
  value,
  onChange,
  onRemove,
}: {
  value: DerivedPropertyDef;
  onChange: (dp: DerivedPropertyDef) => void;
  onRemove?: () => void;
}) {
  const needsField = value.metric !== 'count';
  return (
    <div
      className="flex flex-wrap gap-2 items-center mt-1"
      data-testid="derived-property-row"
    >
      <input
        className="text-xs font-mono bg-bg-secondary border border-border rounded px-1 py-0.5 text-text-primary w-28"
        placeholder="name"
        value={value.name}
        onChange={(e) => onChange({ ...value, name: e.target.value })}
        aria-label="derived name"
        data-testid="derived-name"
      />
      <input
        className="text-xs font-mono bg-bg-secondary border border-border rounded px-1 py-0.5 text-text-primary w-32"
        placeholder="link apiName"
        value={value.link}
        onChange={(e) => onChange({ ...value, link: e.target.value })}
        aria-label="derived link"
        data-testid="derived-link"
      />
      <select
        className="text-xs font-mono bg-bg-secondary border border-border rounded px-1 py-0.5 text-text-primary"
        value={value.direction ?? 'forward'}
        onChange={(e) =>
          onChange({
            ...value,
            direction: e.target.value as 'forward' | 'reverse',
          })
        }
        aria-label="derived direction"
        data-testid="derived-direction"
      >
        <option value="forward">forward</option>
        <option value="reverse">reverse</option>
      </select>
      <select
        className="text-xs font-mono bg-bg-secondary border border-border rounded px-1 py-0.5 text-text-primary"
        value={value.metric}
        onChange={(e) =>
          onChange({
            ...value,
            metric: e.target.value as DerivedPropertyDef['metric'],
          })
        }
        aria-label="derived metric"
        data-testid="derived-metric"
      >
        {DERIVED_METRICS.map((m) => (
          <option key={m} value={m}>
            {m}
          </option>
        ))}
      </select>
      {needsField && (
        <input
          className="text-xs font-mono bg-bg-secondary border border-border rounded px-1 py-0.5 text-text-primary w-24"
          placeholder="field"
          value={value.field ?? ''}
          onChange={(e) => onChange({ ...value, field: e.target.value })}
          aria-label="derived field"
          data-testid="derived-field"
        />
      )}
      {onRemove && (
        <button
          type="button"
          className="text-xs font-mono text-accent-error hover:text-accent-error/70"
          onClick={onRemove}
          aria-label="remove derived property"
        >
          x
        </button>
      )}
    </div>
  );
}

function WhereEditor({
  where,
  onChange,
}: {
  where: WhereClause;
  onChange: (w: WhereClause) => void;
}) {
  return (
    <div className="flex gap-2 items-center mt-1">
      <select
        className="text-xs font-mono bg-bg-secondary border border-border rounded px-1 py-0.5 text-text-primary"
        value={where.type}
        onChange={(e) => onChange({ ...where, type: e.target.value })}
        aria-label="where type"
      >
        {['eq', 'gt', 'gte', 'lt', 'lte', 'isNull', 'containsAnyTerm'].map(
          (t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ),
        )}
      </select>
      <input
        className="text-xs font-mono bg-bg-secondary border border-border rounded px-1 py-0.5 text-text-primary w-24"
        placeholder="field"
        value={where.field ?? ''}
        onChange={(e) => onChange({ ...where, field: e.target.value })}
        aria-label="where field"
      />
      <input
        className="text-xs font-mono bg-bg-secondary border border-border rounded px-1 py-0.5 text-text-primary w-24"
        placeholder="value"
        value={String(where.value ?? '')}
        onChange={(e) => onChange({ ...where, value: e.target.value })}
        aria-label="where value"
      />
    </div>
  );
}

function updateNearest(
  value: NearestNeighborsObjectSet,
  patch: Partial<NearestNeighborsObjectSet>,
): NearestNeighborsObjectSet {
  return { ...value, ...patch };
}

function parsePrimaryKeys(value: string): string[] {
  return value
    .split(/\r?\n/)
    .map((pk) => pk.trim())
    .filter((pk) => pk.length > 0);
}

function updateStatic(
  value: StaticObjectSet,
  patch: Partial<StaticObjectSet>,
): StaticObjectSet {
  return { ...value, ...patch };
}

function UnsupportedObjectSetView({
  value,
  depth,
}: {
  value: ObjectSetDefinition;
  depth: number;
}) {
  const indent = depth * 16;
  return (
    <div
      className="border border-accent-error/30 rounded bg-accent-error/5 p-3 flex flex-col gap-2"
      data-testid="objectset-readonly-unsupported"
      style={{ marginLeft: indent > 0 ? indent : undefined }}
    >
      <div className="text-xs font-sans text-accent-error font-medium">
        Read-only ObjectSet: {value.type}
      </div>
      <div className="text-xs font-mono text-accent-error">
        {unsupportedObjectSetMessage(value.type)}
      </div>
      <pre className="text-xs font-mono text-text-secondary bg-bg-tertiary border border-border rounded p-2 overflow-x-auto">
        {JSON.stringify(value, null, 2)}
      </pre>
    </div>
  );
}

export function ObjectSetBuilder({
  objectTypes,
  value,
  onChange,
  depth = 0,
}: ObjectSetBuilderProps) {
  const indent = depth * 16;

  if (!isEditableObjectSetType(value.type)) {
    return <UnsupportedObjectSetView value={value} depth={depth} />;
  }

  function handleTypeChange(newType: string) {
    if (OBJECT_SET_TYPES.includes(newType as SupportedType)) {
      onChange(defaultForType(newType as SupportedType, objectTypes));
    }
  }

  return (
    <div
      className="border border-border rounded bg-bg-secondary p-2 flex flex-col gap-1"
      style={{ marginLeft: indent > 0 ? indent : undefined }}
    >
      {/* Type row */}
      <div className="flex items-center gap-2">
        <select
          className="text-xs font-mono bg-bg-tertiary border border-border rounded px-1 py-0.5 text-accent-cyan"
          value={value.type}
          onChange={(e) => handleTypeChange(e.target.value)}
          aria-label="objectset type"
        >
          {OBJECT_SET_TYPES.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>

        {/* Base: pick object type */}
        {value.type === 'base' && (
          <select
            className="text-xs font-mono bg-bg-tertiary border border-border rounded px-1 py-0.5 text-text-primary"
            value={value.objectType}
            onChange={(e) =>
              onChange({ type: 'base', objectType: e.target.value })
            }
            aria-label="object type"
          >
            {objectTypes.map((ot) => (
              <option key={ot} value={ot}>
                {ot}
              </option>
            ))}
          </select>
        )}

        {/* Static: pick object type */}
        {value.type === 'static' && (
          <select
            className="text-xs font-mono bg-bg-tertiary border border-border rounded px-1 py-0.5 text-text-primary"
            value={value.objectType}
            onChange={(e) =>
              onChange(updateStatic(value, { objectType: e.target.value }))
            }
            aria-label="object type"
          >
            {objectTypes.map((ot) => (
              <option key={ot} value={ot}>
                {ot}
              </option>
            ))}
          </select>
        )}

        {/* SearchAround: link + direction */}
        {value.type === 'searchAround' && (
          <>
            <input
              className="text-xs font-mono bg-bg-tertiary border border-border rounded px-1 py-0.5 text-text-primary w-28"
              placeholder="link apiName"
              value={value.link}
              onChange={(e) =>
                onChange({ ...value, link: e.target.value })
              }
              aria-label="link"
            />
            <select
              className="text-xs font-mono bg-bg-tertiary border border-border rounded px-1 py-0.5 text-text-primary"
              value={value.direction ?? 'forward'}
              onChange={(e) =>
                onChange({
                  ...value,
                  direction: e.target.value as 'forward' | 'reverse',
                })
              }
              aria-label="direction"
            >
              <option value="forward">forward</option>
              <option value="reverse">reverse</option>
            </select>
          </>
        )}

        {/* NearestNeighbors: property + K + text query */}
        {value.type === 'nearestNeighbors' && (
          <>
            <input
              className="text-xs font-mono bg-bg-tertiary border border-border rounded px-1 py-0.5 text-text-primary w-32"
              placeholder="embedding field"
              value={value.propertyIdentifier?.property.apiName ?? ''}
              onChange={(e) =>
                onChange(updateNearest(value, {
                  propertyIdentifier: {
                    property: { apiName: e.target.value },
                  },
                }))
              }
              aria-label="embedding property"
            />
            <input
              className="text-xs font-mono bg-bg-tertiary border border-border rounded px-1 py-0.5 text-text-primary w-20"
              type="number"
              min={1}
              value={value.numNeighbors ?? 10}
              onChange={(e) =>
                onChange(updateNearest(value, {
                  numNeighbors: Number(e.target.value),
                }))
              }
              aria-label="neighbors"
            />
            <input
              className="text-xs font-mono bg-bg-tertiary border border-border rounded px-1 py-0.5 text-text-primary min-w-48 flex-1"
              placeholder="query text"
              value={value.query?.text?.value ?? ''}
              onChange={(e) =>
                onChange(updateNearest(value, {
                  query: { text: { value: e.target.value } },
                }))
              }
              aria-label="query text"
            />
          </>
        )}
      </div>

      {/* Static: explicit primary keys */}
      {value.type === 'static' && (
        <textarea
          className="text-xs font-mono bg-bg-tertiary border border-border rounded px-2 py-1 text-text-primary min-h-20"
          placeholder="primary keys, one per line"
          value={value.primaryKeys.join('\n')}
          onChange={(e) =>
            onChange(updateStatic(value, {
              primaryKeys: parsePrimaryKeys(e.target.value),
            }))
          }
          aria-label="primary keys"
        />
      )}

      {/* Filter: nested objectSet + where */}
      {value.type === 'filter' && (
        <div className="flex flex-col gap-1 pl-2 border-l border-border">
          <ObjectSetBuilder
            objectTypes={objectTypes}
            value={value.objectSet}
            onChange={(nested) => onChange({ ...value, objectSet: nested })}
            depth={depth + 1}
          />
          <WhereEditor
            where={value.where}
            onChange={(w) => onChange({ ...value, where: w })}
          />
        </div>
      )}

      {/* SearchAround: nested objectSet */}
      {value.type === 'searchAround' && (
        <div className="pl-2 border-l border-border">
          <ObjectSetBuilder
            objectTypes={objectTypes}
            value={value.objectSet}
            onChange={(nested) => onChange({ ...value, objectSet: nested })}
            depth={depth + 1}
          />
        </div>
      )}

      {/* NearestNeighbors: nested candidate ObjectSet */}
      {value.type === 'nearestNeighbors' && (
        <div className="pl-2 border-l border-border">
          <ObjectSetBuilder
            objectTypes={objectTypes}
            value={value.objectSet}
            onChange={(nested) => onChange({ ...value, objectSet: nested })}
            depth={depth + 1}
          />
        </div>
      )}

      {/* withProperties: nested inner + derived property rows */}
      {value.type === 'withProperties' && (
        <div className="flex flex-col gap-1 pl-2 border-l border-border">
          <ObjectSetBuilder
            objectTypes={objectTypes}
            value={value.objectSet}
            onChange={(nested) => onChange({ ...value, objectSet: nested })}
            depth={depth + 1}
          />
          <div
            className="flex flex-col gap-1 mt-1"
            data-testid="derived-property-list"
          >
            {(value.derivedProperties ?? []).map((dp, i) => (
              <DerivedPropertyEditor
                key={i}
                value={dp}
                onChange={(next) => {
                  const updated = [...(value.derivedProperties ?? [])];
                  updated[i] = next;
                  onChange({ ...value, derivedProperties: updated });
                }}
                onRemove={
                  (value.derivedProperties ?? []).length > 1
                    ? () => {
                        const updated = [...(value.derivedProperties ?? [])];
                        updated.splice(i, 1);
                        onChange({ ...value, derivedProperties: updated });
                      }
                    : undefined
                }
              />
            ))}
            <button
              type="button"
              className="text-xs font-mono text-accent-cyan hover:text-accent-cyan/70 self-start mt-1"
              data-testid="derived-property-add"
              onClick={() =>
                onChange({
                  ...value,
                  derivedProperties: [
                    ...(value.derivedProperties ?? []),
                    {
                      name: 'derived',
                      link: '',
                      direction: 'forward',
                      metric: 'count',
                    },
                  ],
                })
              }
            >
              + add derived property
            </button>
          </div>
        </div>
      )}

      {/* Union / Intersect / Subtract: list of objectSets */}
      {(value.type === 'union' ||
        value.type === 'intersect' ||
        value.type === 'subtract') && (
        <div className="flex flex-col gap-1 pl-2 border-l border-border">
          {value.objectSets.map((os, i) => (
            <ObjectSetBuilder
              key={i}
              objectTypes={objectTypes}
              value={os}
              onChange={(nested) => {
                const updated = [...value.objectSets];
                updated[i] = nested;
                onChange({ ...value, objectSets: updated });
              }}
              depth={depth + 1}
            />
          ))}
          <button
            className="text-xs font-mono text-accent-cyan hover:text-accent-cyan/70 self-start mt-1"
            onClick={() =>
              onChange({
                ...value,
                objectSets: [
                  ...value.objectSets,
                  { type: 'base', objectType: objectTypes[0] ?? '' },
                ],
              })
            }
          >
            + add branch
          </button>
        </div>
      )}
    </div>
  );
}
