import type { DataType } from '../../api/types';
import { DataTable, type Column } from '../common/DataTable';

interface PropertyRow extends Record<string, unknown> {
  apiName: string;
  baseType: string;
  isArray: boolean;
  isNullable: boolean;
  isSearchable: boolean;
  isSortable: boolean;
}

interface PropertiesTableProps {
  properties: Record<string, { dataType: DataType; rid: string }>;
}

function parseDataType(dt: DataType): { baseType: string; isArray: boolean } {
  if (dt.type === 'array' && dt.itemType) {
    return { baseType: dt.itemType.type, isArray: true };
  }
  return { baseType: dt.type, isArray: false };
}

function boolCell(value: unknown): React.ReactNode {
  return (
    <span className={value ? 'text-accent-success' : 'text-text-muted'}>
      {value ? '\u2713' : '\u2715'}
    </span>
  );
}

const columns: Column<PropertyRow>[] = [
  { key: 'apiName', header: 'API Name', sortable: true, frozen: true, width: '200px' },
  { key: 'baseType', header: 'Base Type', sortable: true, width: '120px' },
  { key: 'isArray', header: 'Array', render: (row) => boolCell(row.isArray), width: '70px' },
  { key: 'isNullable', header: 'Nullable', render: (row) => boolCell(row.isNullable), width: '80px' },
  { key: 'isSearchable', header: 'Searchable', render: (row) => boolCell(row.isSearchable), width: '90px' },
  { key: 'isSortable', header: 'Sortable', render: (row) => boolCell(row.isSortable), width: '80px' },
];

export function PropertiesTable({ properties }: PropertiesTableProps) {
  const rows: PropertyRow[] = Object.entries(properties).map(([apiName, prop]) => {
    const { baseType, isArray } = parseDataType(prop.dataType);
    return {
      apiName,
      baseType,
      isArray,
      // Nullable and searchability flags are not directly available on the
      // compact property wire format (Record<string, {dataType, rid}>).
      // Default to sensible values; a full Property fetch would supply these.
      isNullable: false,
      isSearchable: false,
      isSortable: false,
    };
  });

  return <DataTable columns={columns} data={rows} />;
}
