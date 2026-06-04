import { useMemo } from 'react';
import type { ObjectType, WireObject } from '../../api/types';
import type { ReactionSummary } from '../../api/reactions';
import { useReactionsBatch } from '../../hooks/useReactions';
import { DataTable, type Column } from '../common/DataTable';
import { truncate } from '../../lib/formatters';

export interface ObjectTableSelection {
  selectedKeys: Set<string>;
  onToggle: (primaryKey: string) => void;
  onToggleAll: (select: boolean) => void;
}

// rowRid is the canonical reaction/watch/comment target for an object-list row
// — the same __rid ObjectDetail feeds into ReactionBar/WatchButton/CommentsTab.
function rowRid(row: WireObject): string {
  return String(row.__rid ?? '');
}

// ReactionDigest renders the compact per-row reaction summary shown in the
// object list: the single highest-count emoji plus the total reaction count
// across all emojis (e.g. "👍 4"). Rows with no reactions render an empty,
// muted cell so the column width stays stable without implying "0 reactions"
// is a clickable affordance. Clicking is intentionally NOT wired here — the
// full ReactionBar lives in ObjectDetail; the list cell is read-only.
function ReactionDigest({ summary }: { summary: ReactionSummary | undefined }) {
  const emojis = summary?.emojis ?? [];
  if (emojis.length === 0) {
    return (
      <span className="text-text-tertiary" aria-hidden="true">
        —
      </span>
    );
  }
  // Summaries arrive count-desc, emoji-asc from the store, so emojis[0] is the
  // top emoji; guard anyway in case a caller passes an unsorted slice.
  const top = emojis.reduce((a, b) =>
    b.count > a.count ? b : a,
  );
  const total = emojis.reduce((sum, e) => sum + e.count, 0);
  return (
    <span className="inline-flex items-center gap-1 font-mono text-xs text-text-secondary">
      <span aria-hidden="true">{top.emoji}</span>
      <span>{total}</span>
    </span>
  );
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
  // showReactions appends a compact per-row reaction digest column. All visible
  // rows are fetched with a SINGLE POST /api/v2/reactions/batch (not one GET per
  // row). Opt-in so bare renders (and pages that don't want the column) neither
  // pay the request nor require a QueryClientProvider in the tree.
  showReactions?: boolean;
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

// ObjectTable is the public entry point. The reaction batch fetch is mounted
// only when showReactions is set, so callers (and bare unit renders) that omit
// it neither issue the request nor need a QueryClientProvider in the tree.
export function ObjectTable(props: ObjectTableProps) {
  if (props.showReactions) {
    return <ObjectTableWithReactions {...props} />;
  }
  return <ObjectTableView {...props} reactionsByRid={undefined} />;
}

// ObjectTableWithReactions issues the SINGLE batched POST /api/v2/reactions/batch
// covering every visible row RID, then hands the resulting Map down to the view.
function ObjectTableWithReactions(props: ObjectTableProps) {
  const targetRids = useMemo(
    () => props.data.map(rowRid).filter(Boolean),
    [props.data],
  );
  const batch = useReactionsBatch(targetRids);
  return (
    <ObjectTableView {...props} reactionsByRid={batch.data?.byRid} />
  );
}

interface ObjectTableViewProps extends ObjectTableProps {
  reactionsByRid: Map<string, ReactionSummary> | undefined;
}

function ObjectTableView({
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
  showReactions,
  reactionsByRid,
}: ObjectTableViewProps) {
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

    // Per-row reactions digest. One column, fed from the single batched fetch
    // re-keyed by RID — never a per-row request. Read-only; the interactive
    // ReactionBar stays in ObjectDetail.
    if (showReactions) {
      cols.push({
        key: '__reactions',
        header: 'Reactions',
        sortable: false,
        width: '96px',
        render: (row) => {
          const rid = rowRid(row);
          const summary = rid ? reactionsByRid?.get(rid) : undefined;
          return (
            <span data-testid={`row-reactions-${rid}`}>
              <ReactionDigest summary={summary} />
            </span>
          );
        },
      });
    }

    return cols;
  }, [
    objectType,
    derivedColumns,
    sortablePropertyKeys,
    selection,
    showReactions,
    reactionsByRid,
  ]);

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
