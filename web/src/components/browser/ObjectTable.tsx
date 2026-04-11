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
  // derivedColumns are property keys that are not declared on the ObjectType
  // schema but are attached to each row by the backend (e.g. withProperties
  // DerivedPropertyDef outputs). They render after the schema columns and are
  // tagged `data-derived-column` so Playwright specs can target them.
  derivedColumns?: string[];
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
  derivedColumns,
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

    // Derived columns (e.g. withProperties output). Tag the cell via a
    // zero-width wrapper so Playwright can select with data-derived-column.
    if (derivedColumns && derivedColumns.length > 0) {
      const seen = new Set(cols.map((c) => c.key));
      for (const name of derivedColumns) {
        if (!name || seen.has(name)) continue;
        seen.add(name);
        cols.push({
          key: name,
          header: name,
          sortable: false,
          render: (row) => {
            const val = row[name];
            const display =
              val === null || val === undefined
                ? ''
                : typeof val === 'object'
                  ? truncate(JSON.stringify(val), 80)
                  : truncate(String(val), 80);
            return (
              <span data-derived-column={name} data-testid={`derived-cell-${name}`}>
                {display}
              </span>
            );
          },
        });
      }
    }

    return cols;
  }, [objectType, derivedColumns]);

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
