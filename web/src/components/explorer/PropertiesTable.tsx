import type { DataType, Property } from '../../api/types';
import { DataTable, type Column } from '../common/DataTable';
import { Badge } from '../common/Badge';

interface CompactProperty {
  dataType: DataType;
  rid: string;
}

type PropertiesMap = Record<string, CompactProperty>;

/**
 * DOG-005: Searchable / Sortable / Nullable flags are *not* on the compact
 * `objectType.properties` map — they live on the detailed Property[] returned
 * by /objectTypes/byRid/{rid}/properties. When `detailedProperties` is
 * supplied, each row uses that as the source of truth so the Explorer table
 * matches the API instead of confidently showing every flag as false.
 *
 * `detailedStatus` controls how rows degrade when the detailed fetch has not
 * resolved yet ("loading" → "…") or failed ("error" → "?") so the operator
 * sees an explicit unknown state instead of a misleading ✕.
 */
type DetailedStatus = 'idle' | 'loading' | 'error' | 'ready';

interface PropertiesTableProps {
  properties: PropertiesMap;
  detailedProperties?: Property[];
  detailedStatus?: DetailedStatus;
}

interface PropertyRow extends Record<string, unknown> {
  apiName: string;
  baseType: string;
  isArray: boolean;
  isNullable: FlagValue;
  isSearchable: FlagValue;
  isSortable: FlagValue;
  // Lifecycle metadata sourced from the authoritative detailed Property[].
  // Undefined when detailed data has not resolved (or did not carry it).
  status?: string;
  deprecatedReason?: string;
  editOnly?: boolean;
}

type FlagValue = boolean | 'loading' | 'unknown';

function parseDataType(dt: DataType): { baseType: string; isArray: boolean } {
  if (dt.type === 'array' && dt.itemType) {
    return { baseType: dt.itemType.type, isArray: true };
  }
  return { baseType: dt.type, isArray: false };
}

function flagCell(value: FlagValue): React.ReactNode {
  if (value === 'loading') {
    return <span className="text-text-muted" aria-label="loading">…</span>;
  }
  if (value === 'unknown') {
    return <span className="text-text-muted" aria-label="unknown">?</span>;
  }
  return (
    <span className={value ? 'text-accent-success' : 'text-text-muted'}>
      {value ? '✓' : '✕'}
    </span>
  );
}

function boolCell(value: unknown): React.ReactNode {
  return (
    <span className={value ? 'text-accent-success' : 'text-text-muted'}>
      {value ? '✓' : '✕'}
    </span>
  );
}

/**
 * Status cell surfaces the property's lifecycle metadata carried on the
 * authoritative detailed Property[]: a DEPRECATED badge (with the
 * deprecatedReason as a hover tooltip so operators see *why* it's gone) and
 * an explicit edit-only indicator for write-only properties. ACTIVE /
 * non-deprecated properties render neither, keeping the column quiet.
 */
function statusCell(row: PropertyRow): React.ReactNode {
  const isDeprecated = row.status === 'DEPRECATED';
  if (!isDeprecated && !row.editOnly) {
    return <span className="text-text-muted">—</span>;
  }
  return (
    <span className="inline-flex items-center gap-1.5">
      {isDeprecated && (
        <Badge variant="error" className="cursor-help">
          <span title={row.deprecatedReason || undefined}>DEPRECATED</span>
        </Badge>
      )}
      {row.editOnly && (
        <span
          className="text-xs text-accent-warning"
          title="Property is edit-only (write-only): it can be set by Actions but is not read back from the object."
        >
          Edit-only
        </span>
      )}
    </span>
  );
}

const columns: Column<PropertyRow>[] = [
  { key: 'apiName', header: 'API Name', sortable: true, frozen: true, width: '200px' },
  { key: 'baseType', header: 'Base Type', sortable: true, width: '120px' },
  { key: 'isArray', header: 'Array', render: (row) => boolCell(row.isArray), width: '70px' },
  { key: 'isNullable', header: 'Nullable', render: (row) => flagCell(row.isNullable), width: '80px' },
  { key: 'isSearchable', header: 'Searchable', render: (row) => flagCell(row.isSearchable), width: '90px' },
  { key: 'isSortable', header: 'Sortable', render: (row) => flagCell(row.isSortable), width: '80px' },
  { key: 'status', header: 'Status', render: (row) => statusCell(row), width: '170px' },
];

function fallbackFlag(status: DetailedStatus): FlagValue {
  if (status === 'loading') return 'loading';
  if (status === 'error') return 'unknown';
  // 'idle' / 'ready' with no detailed data falls back to false (legacy
  // behaviour) — but the parent should normally pass a non-idle status
  // whenever it actually fired the detailed fetch.
  return false;
}

export function PropertiesTable({
  properties,
  detailedProperties,
  detailedStatus = 'idle',
}: PropertiesTableProps) {
  const detailedByApiName = new Map<string, Property>();
  for (const p of detailedProperties ?? []) {
    detailedByApiName.set(p.apiName, p);
  }

  const rows: PropertyRow[] = Object.entries(properties).map(([apiName, prop]) => {
    const { baseType, isArray } = parseDataType(prop.dataType);
    const detailed = detailedByApiName.get(apiName);
    if (detailed) {
      return {
        apiName,
        baseType,
        isArray,
        isNullable: detailed.isNullable,
        isSearchable: detailed.isSearchable,
        isSortable: detailed.isSortable,
        status: detailed.status,
        deprecatedReason: detailed.deprecatedReason,
        editOnly: detailed.editOnly,
      };
    }
    const fallback = fallbackFlag(detailedStatus);
    return {
      apiName,
      baseType,
      isArray,
      isNullable: fallback,
      isSearchable: fallback,
      isSortable: fallback,
    };
  });

  return <DataTable columns={columns} data={rows} />;
}
