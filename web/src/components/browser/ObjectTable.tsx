import { useMemo } from 'react';
import type { ObjectType, WireObject } from '../../api/types';
import { DataTable, type Column } from '../common/DataTable';
import { truncate } from '../../lib/formatters';

interface ObjectTableProps {
  ontologyApiName: string;
  objectType: ObjectType;
  data: WireObject[];
  onRowClick?: (row: WireObject) => void;
  onSort?: (field: string, direction: 'asc' | 'desc') => void;
  pageSize?: number;
  totalCount?: string;
  onNextPage?: () => void;
  onPrevPage?: () => void;
  hasNextPage?: boolean;
  hasPrevPage?: boolean;
  currentPage?: number;
}

export function ObjectTable({
  objectType,
  data,
  onRowClick,
  onSort,
  pageSize,
  totalCount,
  onNextPage,
  onPrevPage,
  hasNextPage,
  hasPrevPage,
  currentPage,
}: ObjectTableProps) {
  const columns = useMemo<Column<WireObject>[]>(() => {
    const cols: Column<WireObject>[] = [];

    // Primary key column is always first and frozen
    cols.push({
      key: '__primaryKey',
      header: objectType.primaryKey,
      frozen: true,
      sortable: true,
      width: '180px',
      render: (row) => String(row.__primaryKey ?? ''),
    });

    // Generate columns from objectType.properties
    if (objectType.properties) {
      for (const [apiName, _prop] of Object.entries(objectType.properties)) {
        // Skip if this is the primary key (already rendered as __primaryKey)
        if (apiName === objectType.primaryKey) continue;

        cols.push({
          key: apiName,
          header: apiName,
          sortable: true,
          render: (row) => {
            const val = row[apiName];
            if (val === null || val === undefined) return '';
            if (typeof val === 'object') return truncate(JSON.stringify(val), 80);
            return truncate(String(val), 80);
          },
        });
      }
    }

    return cols;
  }, [objectType]);

  return (
    <DataTable<WireObject>
      columns={columns}
      data={data}
      onRowClick={onRowClick}
      onSort={onSort}
      pageSize={pageSize}
      totalCount={totalCount}
      onNextPage={onNextPage}
      onPrevPage={onPrevPage}
      hasNextPage={hasNextPage}
      hasPrevPage={hasPrevPage}
      currentPage={currentPage}
    />
  );
}
