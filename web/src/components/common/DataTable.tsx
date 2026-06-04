import { useState } from 'react';

export interface Column<T> {
  key: string;
  header: string;
  render?: (row: T) => React.ReactNode;
  sortable?: boolean;
  frozen?: boolean;
  width?: string;
}

interface DataTableProps<T> {
  columns: Column<T>[];
  data: T[];
  onRowClick?: (row: T) => void;
  onSort?: (field: string, direction: 'asc' | 'desc') => void;
  pageSize?: number;
  totalCount?: string;
  onNextPage?: () => void;
  onPrevPage?: () => void;
  hasNextPage?: boolean;
  hasPrevPage?: boolean;
  currentPage?: number;
}

export function DataTable<T extends Record<string, unknown>>({
  columns,
  data,
  onRowClick,
  onSort,
  pageSize = 25,
  totalCount,
  onNextPage,
  onPrevPage,
  hasNextPage,
  hasPrevPage,
  currentPage = 1,
}: DataTableProps<T>) {
  const [sortField, setSortField] = useState<string | null>(null);
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');

  function handleSort(field: string) {
    const newDir = sortField === field && sortDir === 'asc' ? 'desc' : 'asc';
    setSortField(field);
    setSortDir(newDir);
    onSort?.(field, newDir);
  }

  return (
    <div className="flex flex-col">
      <div className="overflow-x-auto border border-border rounded">
        <table className="w-full text-sm" data-testid="data-table">
          <thead>
            <tr className="bg-bg-tertiary border-b border-border">
              {columns.map((col) => (
                <th
                  key={col.key}
                  className={`px-3 py-2 text-left text-xs font-mono text-text-secondary font-medium ${
                    col.frozen ? 'sticky left-0 bg-bg-tertiary z-10' : ''
                  } ${col.sortable ? 'cursor-pointer hover:text-text-primary' : ''}`}
                  style={col.width ? { width: col.width } : undefined}
                  aria-sort={
                    col.sortable
                      ? sortField === col.key
                        ? sortDir === 'asc'
                          ? 'ascending'
                          : 'descending'
                        : 'none'
                      : undefined
                  }
                >
                  {col.sortable ? (
                    <button
                      type="button"
                      onClick={() => handleSort(col.key)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault();
                          handleSort(col.key);
                        }
                      }}
                      className="flex items-center gap-1 bg-transparent p-0 font-mono text-xs font-medium text-inherit cursor-pointer"
                    >
                      {col.header}
                      {sortField === col.key && (
                        <span className="text-accent-cyan">
                          {sortDir === 'asc' ? '\u2191' : '\u2193'}
                        </span>
                      )}
                    </button>
                  ) : (
                    <span className="flex items-center gap-1">{col.header}</span>
                  )}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.map((row, i) => (
              <tr
                key={i}
                className={`border-b border-border last:border-b-0 ${
                  onRowClick
                    ? 'cursor-pointer hover:bg-bg-tertiary transition-colors'
                    : ''
                }`}
                onClick={() => onRowClick?.(row)}
              >
                {columns.map((col) => (
                  <td
                    key={col.key}
                    className={`px-3 py-2 font-mono text-xs ${
                      col.frozen
                        ? 'sticky left-0 bg-bg-primary z-10 text-accent-cyan'
                        : 'text-text-primary'
                    }`}
                  >
                    {col.render
                      ? col.render(row)
                      : String(row[col.key] ?? '')}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {(onNextPage || onPrevPage || totalCount) && (
        <div className="flex items-center justify-between px-3 py-2 text-xs text-text-secondary font-mono" data-testid="pagination">
          <span>
            {totalCount ? `${totalCount} total` : `${data.length} rows`}
            {pageSize && ` | ${pageSize}/page`}
          </span>
          <div className="flex items-center gap-2">
            {hasPrevPage && (
              <button
                onClick={onPrevPage}
                aria-label="Previous page"
                className="px-2 py-1 bg-bg-tertiary rounded hover:bg-bg-elevated transition-colors"
              >
                Prev
              </button>
            )}
            <span>Page {currentPage}</span>
            {hasNextPage && (
              <button
                onClick={onNextPage}
                aria-label="Next page"
                className="px-2 py-1 bg-bg-tertiary rounded hover:bg-bg-elevated transition-colors"
              >
                Next
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
