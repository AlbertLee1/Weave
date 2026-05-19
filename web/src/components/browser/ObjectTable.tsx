import { useMemo } from 'react';
import type { ObjectType, WireObject } from '../../api/types';
import { DataTable, type Column } from '../common/DataTable';
import { truncate } from '../../lib/formatters';

export interface ObjectTableSelection {
  selectedKeys: Set<string>;
  onToggle: (primaryKey: string) => void;
  onToggleAll: (select: boolean) => void;
}

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
  sortablePropertyKeys?: ReadonlySet<string>;
  selection?: ObjectTableSelection;
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
  sortablePropertyKeys,
  selection,
}: ObjectTableProps) {
  const pageKeys = useMemo(
    () => data.map((row) => String(row.__primaryKey ?? '')).filter(Boolean),
    [data],
  );
  const allOnPageSelected =
    !!selection &&
    pageKeys.length > 0 &&
    pageKeys.every((k) => selection.selectedKeys.has(k));
  const someOnPageSelected =
    !!selection && pageKeys.some((k) => selection.selectedKeys.has(k));

  const columns = useMemo<Column<WireObject>[]>(() => {
    const cols: Column<WireObject>[] = [];

    if (selection) {
      cols.push({
        key: '__select',
        header: '',
        frozen: true,
        width: '36px',
        render: (row) => {
          const pk = String(row.__primaryKey ?? '');
          const checked = selection.selectedKeys.has(pk);
          return (
            <input
              type="checkbox"
              aria-label={`Select ${pk}`}
              data-testid={`select-row-${pk}`}
              checked={checked}
              onClick={(e) => e.stopPropagation()}
              onChange={() => selection.onToggle(pk)}
              className="cursor-pointer"
            />
          );
        },
      });
    }

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
          sortable: sortablePropertyKeys?.has(apiName) ?? false,
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
  }, [objectType, derivedColumns, sortablePropertyKeys, selection]);

  return (
    <div>
      {selection && (
        <div className="flex items-center gap-2 px-2 pb-2 text-xs font-mono text-text-secondary">
          <input
            type="checkbox"
            aria-label="Select all on page"
            data-testid="select-all"
            checked={allOnPageSelected}
            ref={(el) => {
              if (el) el.indeterminate = !allOnPageSelected && someOnPageSelected;
            }}
            onChange={(e) => selection.onToggleAll(e.target.checked)}
            className="cursor-pointer"
          />
          <span>Select all on page</span>
        </div>
      )}
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
    </div>
  );
}
