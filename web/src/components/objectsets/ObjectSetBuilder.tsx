import type { ObjectSetDefinition, WhereClause } from '../../api/types';

const OBJECT_SET_TYPES = [
  'base',
  'filter',
  'union',
  'intersect',
  'subtract',
  'searchAround',
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
  }
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
        {['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'isNull', 'containsAnyTerm'].map(
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

export function ObjectSetBuilder({
  objectTypes,
  value,
  onChange,
  depth = 0,
}: ObjectSetBuilderProps) {
  const indent = depth * 16;

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
      </div>

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
