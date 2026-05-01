import type { CSSProperties, ReactNode } from 'react';

type Dimension = number | string;

type SkeletonVariant = 'text' | 'rect' | 'circle';

const SHIMMER_BG =
  'linear-gradient(90deg, rgba(245,158,11,0.06) 0%, rgba(245,158,11,0.16) 50%, rgba(245,158,11,0.06) 100%)';

function toSize(value: Dimension | undefined): string | undefined {
  if (value === undefined) return undefined;
  return typeof value === 'number' ? `${value}px` : value;
}

interface PlaceholderProps {
  width?: Dimension;
  height?: Dimension;
  variant?: SkeletonVariant;
  className?: string;
  style?: CSSProperties;
  'data-testid'?: string;
}

function Placeholder({
  width,
  height,
  variant = 'rect',
  className = '',
  style,
  'data-testid': dataTestId,
}: PlaceholderProps) {
  const radius =
    variant === 'circle'
      ? 'rounded-full'
      : variant === 'text'
        ? 'rounded-sm'
        : 'rounded-md';
  return (
    <span
      aria-hidden="true"
      data-testid={dataTestId}
      className={`block ${radius} ${className}`.trim()}
      style={{
        width: toSize(width) ?? '100%',
        height: toSize(height) ?? (variant === 'text' ? '0.75rem' : '1rem'),
        background: SHIMMER_BG,
        backgroundSize: '200% 100%',
        animation: 'shimmer 1.6s ease-in-out infinite',
        ...style,
      }}
    />
  );
}

function StatusRegion({
  children,
  ariaLabel,
  className = '',
}: {
  children: ReactNode;
  ariaLabel: string;
  className?: string;
}) {
  return (
    <div
      role="status"
      aria-live="polite"
      aria-busy="true"
      aria-label={ariaLabel}
      className={className}
    >
      {children}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Primitive Skeleton
// ---------------------------------------------------------------------------

export interface SkeletonProps {
  width?: Dimension;
  height?: Dimension;
  variant?: SkeletonVariant;
  className?: string;
  style?: CSSProperties;
  'aria-label'?: string;
  'data-testid'?: string;
}

export function Skeleton({
  width,
  height,
  variant = 'rect',
  className = '',
  style,
  'aria-label': ariaLabel = 'Loading',
  'data-testid': dataTestId,
}: SkeletonProps) {
  return (
    <StatusRegion ariaLabel={ariaLabel} className={className}>
      <Placeholder
        width={width}
        height={height}
        variant={variant}
        style={style}
        data-testid={dataTestId}
      />
    </StatusRegion>
  );
}

// ---------------------------------------------------------------------------
// Multi-line text
// ---------------------------------------------------------------------------

export interface SkeletonTextProps {
  lines?: number;
  lineHeight?: Dimension;
  lastLineWidth?: Dimension;
  className?: string;
  'aria-label'?: string;
  'data-testid'?: string;
}

export function SkeletonText({
  lines = 3,
  lineHeight = 12,
  lastLineWidth = '60%',
  className = '',
  'aria-label': ariaLabel = 'Loading',
}: SkeletonTextProps) {
  const count = Math.max(1, lines);
  return (
    <StatusRegion
      ariaLabel={ariaLabel}
      className={`flex flex-col gap-2 ${className}`.trim()}
    >
      {Array.from({ length: count }, (_, i) => {
        const isLast = i === count - 1 && count > 1;
        return (
          <Placeholder
            key={i}
            data-testid="skeleton-text-line"
            variant="text"
            width={isLast ? lastLineWidth : '100%'}
            height={lineHeight}
          />
        );
      })}
    </StatusRegion>
  );
}

// ---------------------------------------------------------------------------
// Card placeholder (matches OntologyCard / dashboard tile shape)
// ---------------------------------------------------------------------------

export interface SkeletonCardProps {
  className?: string;
  bodyLines?: number;
  'aria-label'?: string;
}

export function SkeletonCard({
  className = '',
  bodyLines = 2,
  'aria-label': ariaLabel = 'Loading card',
}: SkeletonCardProps) {
  const count = Math.max(1, bodyLines);
  return (
    <StatusRegion
      ariaLabel={ariaLabel}
      className={`block rounded-2xl border border-border bg-bg-secondary p-5 ${className}`.trim()}
    >
      <div data-testid="skeleton-card" className="flex flex-col gap-3">
        <Placeholder
          data-testid="skeleton-card-title"
          variant="text"
          height={18}
          width="55%"
        />
        <div className="flex flex-col gap-2">
          {Array.from({ length: count }, (_, i) => (
            <Placeholder
              key={i}
              data-testid="skeleton-card-body-line"
              variant="text"
              height={10}
              width={i === count - 1 ? '70%' : '100%'}
            />
          ))}
        </div>
      </div>
    </StatusRegion>
  );
}

// ---------------------------------------------------------------------------
// Table placeholder (matches ObjectTable shape)
// ---------------------------------------------------------------------------

export interface SkeletonTableProps {
  rows?: number;
  columns?: number;
  showHeader?: boolean;
  className?: string;
  'aria-label'?: string;
}

export function SkeletonTable({
  rows = 5,
  columns = 4,
  showHeader = true,
  className = '',
  'aria-label': ariaLabel = 'Loading table',
}: SkeletonTableProps) {
  const rowCount = Math.max(1, rows);
  const colCount = Math.max(1, columns);
  return (
    <StatusRegion
      ariaLabel={ariaLabel}
      className={`overflow-hidden rounded-md border border-border bg-bg-secondary ${className}`.trim()}
    >
      {showHeader && (
        <div
          data-testid="skeleton-table-header"
          className="grid gap-3 border-b border-border bg-bg-tertiary px-4 py-3"
          style={{ gridTemplateColumns: `repeat(${colCount}, minmax(0, 1fr))` }}
        >
          {Array.from({ length: colCount }, (_, c) => (
            <Placeholder
              key={c}
              data-testid="skeleton-table-header-cell"
              variant="text"
              height={10}
              width="50%"
            />
          ))}
        </div>
      )}
      <div className="flex flex-col">
        {Array.from({ length: rowCount }, (_, r) => (
          <div
            key={r}
            data-testid="skeleton-table-row"
            className="grid gap-3 border-b border-border/60 px-4 py-3 last:border-b-0"
            style={{
              gridTemplateColumns: `repeat(${colCount}, minmax(0, 1fr))`,
            }}
          >
            {Array.from({ length: colCount }, (_, c) => (
              <Placeholder
                key={c}
                data-testid="skeleton-table-cell"
                variant="text"
                height={12}
                width={c === 0 ? '70%' : c === colCount - 1 ? '40%' : '90%'}
              />
            ))}
          </div>
        ))}
      </div>
    </StatusRegion>
  );
}

// ---------------------------------------------------------------------------
// List placeholder (sidebar / type tree / saved searches)
// ---------------------------------------------------------------------------

export interface SkeletonListProps {
  items?: number;
  withAvatar?: boolean;
  className?: string;
  'aria-label'?: string;
}

export function SkeletonList({
  items = 6,
  withAvatar = false,
  className = '',
  'aria-label': ariaLabel = 'Loading list',
}: SkeletonListProps) {
  const count = Math.max(1, items);
  return (
    <StatusRegion
      ariaLabel={ariaLabel}
      className={`flex flex-col gap-2 ${className}`.trim()}
    >
      {Array.from({ length: count }, (_, i) => (
        <div
          key={i}
          data-testid="skeleton-list-item"
          className="flex items-center gap-3 px-3 py-2"
        >
          {withAvatar && (
            <Placeholder
              variant="circle"
              width={24}
              height={24}
              data-testid="skeleton-list-avatar"
            />
          )}
          <Placeholder
            variant="text"
            height={12}
            width={`${60 + ((i * 7) % 30)}%`}
          />
        </div>
      ))}
    </StatusRegion>
  );
}
