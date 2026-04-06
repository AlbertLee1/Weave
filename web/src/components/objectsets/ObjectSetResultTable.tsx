import { useMemo } from 'react';
import type { WireObject } from '../../api/types';
import { truncate } from '../../lib/formatters';

interface ObjectSetResultTableProps {
  data: WireObject[];
  totalCount?: string;
  hasNextPage: boolean;
  hasPrevPage: boolean;
  onNextPage: () => void;
  onPrevPage: () => void;
  currentPage: number;
}

const HIDDEN_KEYS = new Set(['__rid', '__apiName']);

export function ObjectSetResultTable({
  data,
  totalCount,
  hasNextPage,
  hasPrevPage,
  onNextPage,
  onPrevPage,
  currentPage,
}: ObjectSetResultTableProps) {
  // Derive columns from the union of keys across all rows, preserving insertion order
  const columns = useMemo<string[]>(() => {
    const seen = new Set<string>();
    const cols: string[] = [];
    for (const row of data) {
      for (const key of Object.keys(row)) {
        if (!seen.has(key) && !HIDDEN_KEYS.has(key)) {
          seen.add(key);
          cols.push(key);
        }
      }
    }
    return cols;
  }, [data]);

  return (
    <div className="flex flex-col gap-2">
      {/* Pagination header */}
      <div className="flex items-center justify-between text-xs font-mono text-text-secondary">
        <span>
          {totalCount !== undefined ? `${totalCount} total` : ''}
        </span>
        <div className="flex items-center gap-2">
          <span>Page {currentPage}</span>
          <button
            aria-label="prev page"
            onClick={onPrevPage}
            disabled={!hasPrevPage}
            className="px-2 py-1 border border-border rounded disabled:opacity-40 hover:bg-bg-secondary"
          >
            Prev
          </button>
          <button
            aria-label="next page"
            onClick={onNextPage}
            disabled={!hasNextPage}
            className="px-2 py-1 border border-border rounded disabled:opacity-40 hover:bg-bg-secondary"
          >
            Next
          </button>
        </div>
      </div>

      {/* Table */}
      <div className="overflow-x-auto border border-border rounded">
        <table className="w-full text-xs font-mono">
          <thead>
            <tr className="border-b border-border bg-bg-secondary">
              {columns.map((col) => (
                <th
                  key={col}
                  className="px-3 py-2 text-left text-text-secondary font-medium whitespace-nowrap"
                >
                  {col}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.map((row, i) => (
              <tr
                key={String(row.__primaryKey ?? i)}
                className="border-b border-border last:border-0 hover:bg-bg-secondary/50"
              >
                {columns.map((col) => {
                  const val = row[col];
                  let display = '';
                  if (val !== null && val !== undefined) {
                    display =
                      typeof val === 'object'
                        ? truncate(JSON.stringify(val), 80)
                        : String(val);
                  }
                  return (
                    <td
                      key={col}
                      className="px-3 py-2 text-text-primary whitespace-nowrap"
                    >
                      {display}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
