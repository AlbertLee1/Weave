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

function getHighlightSnippet(row: WireObject, field: string): string | undefined {
  const raw = row._highlights;
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return undefined;
  const snippets = (raw as Record<string, unknown>)[field];
  if (!Array.isArray(snippets)) return undefined;
  return snippets.find((s): s is string => typeof s === 'string' && s.length > 0);
}

function renderHighlightSnippet(snippet: string): React.ReactNode {
  const parts = snippet.split(/(<\/?mark>)/gi);
  let marked = false;
  return (
    <span className="whitespace-normal">
      {parts.map((part, index) => {
        const token = part.toLowerCase();
        if (token === '<mark>') {
          marked = true;
          return null;
        }
        if (token === '</mark>') {
          marked = false;
          return null;
        }
        if (!part) return null;
        return marked ? (
          <mark
            key={index}
            className="rounded bg-accent-amber/25 px-0.5 text-text-primary"
          >
            {part}
          </mark>
        ) : (
          <span key={index}>{part}</span>
        );
      })}
    </span>
  );
}

function renderCellValue(
  row: WireObject,
  field: string,
  value: unknown,
): React.ReactNode {
  const snippet = getHighlightSnippet(row, field);
  if (snippet) return renderHighlightSnippet(snippet);
  if (value === null || value === undefined) return '';
  if (typeof value === 'object') return truncate(JSON.stringify(value), 80);
  return truncate(String(value), 80);
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
      render: (row) => renderCellValue(row, objectType.primaryKey, row.__primaryKey),
    });

    // Generate columns from objectType.properties
    if (objectType.properties) {
      for (const apiName of Object.keys(objectType.properties)) {
        // Skip if this is the primary key (already rendered as __primaryKey)
        if (apiName === objectType.primaryKey) continue;

        cols.push({
          key: apiName,
          header: apiName,
          sortable: sortablePropertyKeys?.has(apiName) ?? false,
          render: (row) => renderCellValue(row, apiName, row[apiName]),
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
