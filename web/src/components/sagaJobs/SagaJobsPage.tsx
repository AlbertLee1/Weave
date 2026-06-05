import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react';
import { useParams } from 'react-router';
import { ApiRequestError } from '../../api/client';
import {
  SAGA_STATUSES,
  type Saga,
  type SagaStatus,
  type SagaStep,
  type SagaStepStatus,
} from '../../api/sagaJobs';
import type { SagaDLQEntry } from '../../api/sagaDLQ';
import { useSagaDetail, useSagaJobs } from '../../hooks/useSagaJobs';
import {
  useDropSagaDLQ,
  useRetrySagaDLQ,
  useSagaDLQ,
} from '../../hooks/useSagaDLQ';
import { useToastStore } from '../../stores/toastStore';
import { EmptyState } from '../common/EmptyState';
import { SkeletonTable } from '../common/Skeleton';

// US-044 (PC-A08): Action Saga / Job monitoring UI.
//
// The page renders the saga list for the active ontology with a 5-state
// colored badge (RUNNING / SUCCESS / COMPENSATING / COMPENSATED /
// FAILED) and a status filter tab row. Clicking a row opens the detail
// drawer with a per-step timeline (status marker per step,
// compensation badges, retry / attempt count, and a "View DLQ" link
// when the step ended in COMPENSATION_FAILED). A separate DLQ drawer
// from the filter bar exposes Replay / Discard with a second-confirm
// modal — the operator action surface for stuck compensations.
//
// Reachable via `/actions/:ontology/jobs` (App.tsx); the Sidebar
// "Saga Jobs" entry surfaces once an active ontology is selected.

// Ordered options for the status-filter tablist: "All" first, then the
// five saga statuses. Drives both rendering and the roving-tabindex
// keyboard navigation index math.
const STATUS_FILTER_OPTIONS: (SagaStatus | 'ALL')[] = ['ALL', ...SAGA_STATUSES];

const STATUS_BADGE_STYLE: Record<SagaStatus, string> = {
  RUNNING: 'bg-sky-500/10 text-sky-300 border border-sky-500/30',
  SUCCESS: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  COMPENSATING: 'bg-amber-500/10 text-amber-300 border border-amber-500/30',
  COMPENSATED: 'bg-violet-500/10 text-violet-300 border border-violet-500/30',
  FAILED: 'bg-rose-500/10 text-rose-300 border border-rose-500/30',
};

const STEP_STATUS_BADGE_STYLE: Record<SagaStepStatus, string> = {
  PENDING: 'bg-slate-500/10 text-slate-300 border border-slate-500/30',
  APPLIED: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  FAILED: 'bg-rose-500/10 text-rose-300 border border-rose-500/30',
  COMPENSATED: 'bg-violet-500/10 text-violet-300 border border-violet-500/30',
  COMPENSATION_FAILED:
    'bg-orange-500/10 text-orange-300 border border-orange-500/30',
};

// Ordered options for the DLQ status-filter tablist. Drives both
// rendering and roving-tabindex keyboard navigation index math.
const DLQ_STATUS_OPTIONS: SagaDLQEntry['status'][] = [
  'PENDING',
  'RESOLVED',
  'DROPPED',
];

const DLQ_STATUS_BADGE_STYLE: Record<SagaDLQEntry['status'], string> = {
  PENDING: 'bg-amber-500/10 text-amber-400 border border-amber-500/30',
  RESOLVED: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  DROPPED: 'bg-slate-500/10 text-slate-400 border border-slate-500/30',
};

function describeApiError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    const reason = err.parameters?.reason ?? err.parameters?.error;
    return reason ? `${err.errorName}: ${reason}` : err.errorName;
  }
  if (err instanceof Error) return err.message;
  return 'Operation failed.';
}

function formatTimestamp(value: string): string {
  try {
    return new Date(value).toLocaleString();
  } catch {
    return value;
  }
}

function stringifyJson(value: unknown): string {
  if (value === undefined || value === null) return '(none)';
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

export function SagaJobsPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const activeOntology = ontology ?? '';

  const [statusFilter, setStatusFilter] = useState<SagaStatus | 'ALL'>('ALL');
  const [selectedSagaId, setSelectedSagaId] = useState<string | null>(null);
  const [dlqDrawerOpen, setDlqDrawerOpen] = useState(false);

  const listQuery = useSagaJobs(
    activeOntology,
    statusFilter === 'ALL' ? {} : { status: statusFilter },
  );

  const sagas = useMemo(
    () => listQuery.data?.data ?? [],
    [listQuery.data],
  );

  // Roving-tabindex refs for the status-filter tablist so keyboard
  // navigation can move DOM focus to the activated tab (WAI-ARIA tabs).
  const statusTabRefs = useRef<(HTMLButtonElement | null)[]>([]);

  // ArrowLeft/Right (and Up/Down mirror) move between status tabs with
  // wrap-around, Home/End jump to the ends. Activation follows focus:
  // moving focus also updates the filter, the recommended pattern for
  // tablists whose result panel re-renders cheaply.
  const handleStatusTabKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
      const last = STATUS_FILTER_OPTIONS.length - 1;
      let nextIndex: number | null = null;
      switch (event.key) {
        case 'ArrowRight':
        case 'ArrowDown':
          nextIndex = index === last ? 0 : index + 1;
          break;
        case 'ArrowLeft':
        case 'ArrowUp':
          nextIndex = index === 0 ? last : index - 1;
          break;
        case 'Home':
          nextIndex = 0;
          break;
        case 'End':
          nextIndex = last;
          break;
        default:
          return;
      }
      event.preventDefault();
      setStatusFilter(STATUS_FILTER_OPTIONS[nextIndex]);
      statusTabRefs.current[nextIndex]?.focus();
    },
    [],
  );

  if (!activeOntology) {
    return (
      <div
        data-testid="saga-jobs-empty-ontology"
        className="flex items-center justify-center h-full"
      >
        <EmptyState
          title="No Ontology Selected"
          description="Select an ontology from the Dashboard to monitor saga jobs."
        />
      </div>
    );
  }

  return (
    <div
      data-testid="saga-jobs-page"
      className="mx-auto max-w-6xl space-y-6"
    >
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
            Saga Jobs
          </h1>
          <p className="text-sm text-text-secondary">
            Multi-step actions on{' '}
            <span className="font-mono text-text-primary">{activeOntology}</span>
            . Click a row to inspect step timeline, compensation, and DLQ links.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setDlqDrawerOpen(true)}
          data-testid="saga-jobs-open-dlq-btn"
          className="rounded-md border border-amber-500/50 px-3 py-1.5 text-sm font-medium text-amber-300 hover:bg-amber-500/10"
        >
          Open DLQ
        </button>
      </header>

      <section
        className="flex flex-wrap items-center gap-3 rounded-lg border border-border/50 bg-bg-secondary/60 p-3"
        data-testid="saga-jobs-filters"
      >
        <div
          className="flex items-center gap-1"
          role="tablist"
          aria-label="Saga status filter"
        >
          {STATUS_FILTER_OPTIONS.map((opt, index) => (
            <FilterTab
              key={opt}
              value={opt}
              label={opt === 'ALL' ? 'All' : opt}
              current={statusFilter}
              index={index}
              tabRef={(el) => {
                statusTabRefs.current[index] = el;
              }}
              onSelect={setStatusFilter}
              onKeyDown={handleStatusTabKeyDown}
            />
          ))}
        </div>
      </section>

      {listQuery.isLoading ? (
        <div data-testid="saga-jobs-loading">
          <SkeletonTable rows={6} columns={4} aria-label="Loading saga jobs" />
        </div>
      ) : listQuery.isError ? (
        <div data-testid="saga-jobs-error">
          <EmptyState
            title="Failed to load saga jobs"
            description={
              listQuery.error instanceof Error
                ? listQuery.error.message
                : 'Unexpected error.'
            }
          />
        </div>
      ) : sagas.length === 0 ? (
        <div data-testid="saga-jobs-empty">
          <EmptyState
            title="No saga jobs match this filter"
            description={
              statusFilter === 'ALL'
                ? 'No multi-step actions have run on this ontology yet.'
                : `No sagas are currently in ${statusFilter} state.`
            }
          />
        </div>
      ) : (
        <ul
          data-testid="saga-jobs-list"
          aria-label="Saga jobs"
          className="space-y-2"
        >
          {sagas.map((saga) => (
            <SagaRow
              key={saga.sagaId}
              saga={saga}
              onOpen={() => setSelectedSagaId(saga.sagaId)}
            />
          ))}
        </ul>
      )}

      {selectedSagaId && (
        <SagaDetailDrawer
          ontology={activeOntology}
          sagaId={selectedSagaId}
          onClose={() => setSelectedSagaId(null)}
          onOpenDLQ={() => setDlqDrawerOpen(true)}
        />
      )}

      {dlqDrawerOpen && (
        <DLQDrawer
          ontology={activeOntology}
          onClose={() => setDlqDrawerOpen(false)}
        />
      )}
    </div>
  );
}

interface FilterTabProps {
  value: SagaStatus | 'ALL';
  label: string;
  current: SagaStatus | 'ALL';
  index: number;
  tabRef: (el: HTMLButtonElement | null) => void;
  onSelect: (v: SagaStatus | 'ALL') => void;
  onKeyDown: (event: KeyboardEvent<HTMLButtonElement>, index: number) => void;
}

function FilterTab({
  value,
  label,
  current,
  index,
  tabRef,
  onSelect,
  onKeyDown,
}: FilterTabProps) {
  const selected = current === value;
  return (
    <button
      ref={tabRef}
      type="button"
      role="tab"
      aria-selected={selected}
      tabIndex={selected ? 0 : -1}
      data-testid={`saga-jobs-filter-${value.toLowerCase()}`}
      onClick={() => onSelect(value)}
      onKeyDown={(event) => onKeyDown(event, index)}
      className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
        selected
          ? 'bg-bg-tertiary text-text-primary'
          : 'text-text-secondary hover:bg-bg-tertiary/60 hover:text-text-primary'
      }`}
    >
      {label}
    </button>
  );
}

interface SagaRowProps {
  saga: Saga;
  onOpen: () => void;
}

function SagaRow({ saga, onOpen }: SagaRowProps) {
  return (
    <li
      data-testid="saga-row"
      data-saga-id={saga.sagaId}
      data-saga-status={saga.status}
      className="rounded-lg border border-border/50 bg-bg-secondary/60 p-3 hover:bg-bg-tertiary/40"
    >
      <div className="flex flex-wrap items-center gap-3">
        <span
          data-testid="saga-row-status-badge"
          className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[saga.status]}`}
        >
          {saga.status}
        </span>
        <div className="flex-1 min-w-0">
          <div className="font-mono text-sm text-text-primary truncate">
            {saga.sagaId}
          </div>
          <div className="text-xs text-text-secondary truncate">
            requested by{' '}
            <span className="text-text-primary">
              {saga.requestedBy ?? '(unknown)'}
            </span>
            {' · '}created {formatTimestamp(saga.createdAt)}
            {saga.idempotencyKey && (
              <>
                {' · '}idempotency{' '}
                <span className="font-mono text-text-primary">
                  {saga.idempotencyKey}
                </span>
              </>
            )}
          </div>
        </div>
        <button
          type="button"
          onClick={onOpen}
          data-testid="saga-row-open-btn"
          data-saga-id={saga.sagaId}
          className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
        >
          Inspect
        </button>
      </div>
      {saga.failureMessage && (
        <div className="mt-2 text-xs text-rose-300">
          {saga.failureMessage}
        </div>
      )}
    </li>
  );
}

interface DrawerShellProps {
  testId: string;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}

// Elements that can receive keyboard focus, used by the drawer's focus trap.
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function DrawerShell({ testId, title, onClose, children }: DrawerShellProps) {
  // Focus management for this self-drawn drawer (it is NOT the shared
  // common/Modal, which already traps + restores focus). On open we move focus
  // inside, keep Tab/Shift+Tab cycling within, close on Escape, and restore
  // focus to whatever element opened the drawer (typically the Inspect / DLQ
  // trigger) when it unmounts — so keyboard users never end up behind the
  // overlay. Mirrors VertexShareLinkPanel (#229).
  const dialogRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);

  // Record the element that had focus when the drawer mounted, move focus into
  // the dialog, and restore focus to the trigger on unmount. Runs once per
  // mount — the parent conditionally mounts/unmounts this drawer on
  // open/close, so mount == open and unmount == close.
  useEffect(() => {
    triggerRef.current = document.activeElement as HTMLElement | null;
    const dialog = dialogRef.current;
    if (dialog) {
      const first = dialog.querySelector<HTMLElement>(FOCUSABLE_SELECTOR);
      // Prefer the first focusable child; fall back to the dialog itself
      // (focusable via tabIndex={-1}) so focus never sits on the page behind.
      if (first) first.focus();
      else dialog.focus();
    }
    return () => {
      const trigger = triggerRef.current;
      if (trigger && typeof trigger.focus === 'function') trigger.focus();
    };
  }, []);

  // Escape closes the drawer.
  useEffect(() => {
    function handleKey(e: globalThis.KeyboardEvent) {
      if (e.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [onClose]);

  // Focus trap: keep Tab / Shift+Tab cycling among the dialog's focusable
  // elements instead of escaping to the background page.
  const handleTrapKeyDown = useCallback(
    (e: KeyboardEvent<HTMLDivElement>) => {
      if (e.key !== 'Tab') return;
      const dialog = dialogRef.current;
      if (!dialog) return;
      const focusables = Array.from(
        dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
      );

      // Degenerate case: nothing focusable inside — keep focus on the dialog.
      if (focusables.length === 0) {
        e.preventDefault();
        dialog.focus();
        return;
      }

      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement;

      if (e.shiftKey) {
        // Shift+Tab on the first element (or focus already outside) wraps to last.
        if (active === first || !dialog.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        // Tab on the last element (or focus already outside) wraps to first.
        if (active === last || !dialog.contains(active)) {
          e.preventDefault();
          first.focus();
        }
      }
    },
    [],
  );

  return (
    <div
      ref={dialogRef}
      data-testid={testId}
      role="dialog"
      aria-modal="true"
      aria-label={title}
      tabIndex={-1}
      onKeyDown={handleTrapKeyDown}
      className="fixed inset-0 z-40 flex"
    >
      <div
        data-testid={`${testId}-overlay`}
        className="flex-1 bg-black/50"
        onClick={onClose}
      />
      <div
        className="w-[40rem] max-w-full bg-bg-primary border-l border-border/60 overflow-y-auto"
        style={{ boxShadow: '-8px 0 32px rgba(0,0,0,0.4)' }}
      >
        <header className="flex items-center justify-between border-b border-border/40 px-4 py-3">
          <h2 className="text-base font-semibold text-text-primary">
            {title}
          </h2>
          <button
            type="button"
            data-testid={`${testId}-close-btn`}
            onClick={onClose}
            aria-label="Close drawer"
            className="rounded p-1 text-text-secondary hover:bg-bg-tertiary hover:text-text-primary"
          >
            <svg viewBox="0 0 16 16" className="h-4 w-4" aria-hidden="true">
              <path
                d="M3 3 L13 13 M13 3 L3 13"
                stroke="currentColor"
                strokeWidth="1.6"
                strokeLinecap="round"
                fill="none"
              />
            </svg>
          </button>
        </header>
        <div className="p-4">{children}</div>
      </div>
    </div>
  );
}

interface SagaDetailDrawerProps {
  ontology: string;
  sagaId: string;
  onClose: () => void;
  onOpenDLQ: () => void;
}

function SagaDetailDrawer({
  ontology,
  sagaId,
  onClose,
  onOpenDLQ,
}: SagaDetailDrawerProps) {
  const query = useSagaDetail(ontology, sagaId);
  const saga = query.data?.saga;
  const steps = useMemo<SagaStep[]>(
    () => query.data?.steps ?? [],
    [query.data],
  );

  // "Retry count" semantics: count of steps that needed compensation
  // (either COMPENSATED happily or COMPENSATION_FAILED into the DLQ).
  // This is the operator-visible "how much rollback did this saga
  // need" — independent of the per-step retry budget the executor
  // tracks internally (not surfaced today).
  const compensationCount = useMemo(
    () =>
      steps.filter(
        (s) =>
          s.status === 'COMPENSATED' || s.status === 'COMPENSATION_FAILED',
      ).length,
    [steps],
  );

  const compensationFailed = useMemo(
    () => steps.some((s) => s.status === 'COMPENSATION_FAILED'),
    [steps],
  );

  return (
    <DrawerShell
      testId="saga-detail-drawer"
      title={`Saga ${sagaId}`}
      onClose={onClose}
    >
      {query.isLoading ? (
        <div data-testid="saga-detail-loading">
          <SkeletonTable rows={5} columns={3} aria-label="Loading saga detail" />
        </div>
      ) : query.isError || !saga ? (
        <div data-testid="saga-detail-error">
          <EmptyState
            title="Failed to load saga"
            description={
              query.error instanceof Error
                ? query.error.message
                : 'Unexpected error.'
            }
          />
        </div>
      ) : (
        <div className="space-y-4 text-sm">
          <section
            data-testid="saga-detail-header"
            data-saga-id={saga.sagaId}
            data-saga-status={saga.status}
            className="rounded-md border border-border/40 bg-bg-secondary/60 p-3"
          >
            <div className="flex flex-wrap items-center gap-2">
              <span
                data-testid="saga-detail-status-badge"
                className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[saga.status]}`}
              >
                {saga.status}
              </span>
              <span
                data-testid="saga-detail-compensation-count"
                data-count={compensationCount}
                className="text-xs text-text-secondary"
              >
                {compensationCount} step
                {compensationCount === 1 ? '' : 's'} compensated
              </span>
            </div>
            <dl className="mt-2 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-xs text-text-secondary">
              <dt>Saga</dt>
              <dd className="font-mono text-text-primary">{saga.sagaId}</dd>
              <dt>Requested by</dt>
              <dd className="text-text-primary">
                {saga.requestedBy ?? '(unknown)'}
              </dd>
              <dt>Created</dt>
              <dd className="text-text-primary">
                {formatTimestamp(saga.createdAt)}
              </dd>
              <dt>Updated</dt>
              <dd className="text-text-primary">
                {formatTimestamp(saga.updatedAt)}
              </dd>
              {saga.idempotencyKey && (
                <>
                  <dt>Idempotency</dt>
                  <dd className="font-mono text-text-primary">
                    {saga.idempotencyKey}
                  </dd>
                </>
              )}
              {saga.failureMessage && (
                <>
                  <dt>Failure</dt>
                  <dd className="text-rose-300">{saga.failureMessage}</dd>
                </>
              )}
            </dl>
            {compensationFailed && (
              <button
                type="button"
                onClick={onOpenDLQ}
                data-testid="saga-detail-dlq-link"
                className="mt-3 inline-flex items-center gap-1 rounded-md border border-amber-500/50 px-3 py-1 text-xs font-medium text-amber-300 hover:bg-amber-500/10"
              >
                View DLQ entries →
              </button>
            )}
          </section>

          <section data-testid="saga-detail-timeline" className="space-y-2">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-text-secondary">
              Step timeline
            </h3>
            {steps.length === 0 ? (
              <EmptyState
                title="No steps recorded"
                description="The saga did not persist any step rows."
              />
            ) : (
              <ol className="space-y-2">
                {steps.map((step) => (
                  <SagaStepRow key={step.stepId} step={step} />
                ))}
              </ol>
            )}
          </section>
        </div>
      )}
    </DrawerShell>
  );
}

interface SagaStepRowProps {
  step: SagaStep;
}

function SagaStepRow({ step }: SagaStepRowProps) {
  const isCompensated =
    step.status === 'COMPENSATED' || step.status === 'COMPENSATION_FAILED';
  return (
    <li
      data-testid="saga-step-row"
      data-step-id={step.stepId}
      data-step-status={step.status}
      data-step-index={step.stepIndex}
      data-step-compensated={isCompensated ? 'true' : 'false'}
      className="rounded-md border border-border/40 bg-bg-secondary/40 p-3"
    >
      <div className="flex flex-wrap items-center gap-2">
        <span
          data-testid="saga-step-index"
          className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-bg-tertiary text-[10px] font-semibold text-text-primary"
        >
          {step.stepIndex + 1}
        </span>
        <span
          data-testid="saga-step-status-badge"
          className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STEP_STATUS_BADGE_STYLE[step.status]}`}
        >
          {step.status}
        </span>
        {isCompensated && (
          <span
            data-testid="saga-step-compensation-marker"
            className="inline-flex rounded-full bg-violet-500/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-violet-300 border border-violet-500/30"
          >
            compensated
          </span>
        )}
        <span className="font-mono text-xs text-text-primary truncate">
          {step.actionType}
        </span>
      </div>
      <details className="mt-2 text-xs">
        <summary className="cursor-pointer text-text-secondary hover:text-text-primary">
          Edits
        </summary>
        <pre
          data-testid="saga-step-edits"
          className="mt-1 max-h-48 overflow-auto rounded-md border border-border/40 bg-bg-primary/70 p-2 font-mono text-[11px] text-text-primary"
        >
          {stringifyJson(step.editsJson)}
        </pre>
      </details>
      {isCompensated && (
        <details className="mt-1 text-xs">
          <summary className="cursor-pointer text-text-secondary hover:text-text-primary">
            Inverse edits
          </summary>
          <pre
            data-testid="saga-step-inverse-edits"
            className="mt-1 max-h-48 overflow-auto rounded-md border border-border/40 bg-bg-primary/70 p-2 font-mono text-[11px] text-text-primary"
          >
            {stringifyJson(step.inverseEditsJson)}
          </pre>
        </details>
      )}
    </li>
  );
}

interface DLQDrawerProps {
  ontology: string;
  onClose: () => void;
}

function DLQDrawer({ ontology, onClose }: DLQDrawerProps) {
  const [status, setStatus] = useState<SagaDLQEntry['status']>('PENDING');
  // Two-step confirmation: storing the kind ('retry' | 'discard') +
  // the targeted dlqId lets a single inline confirmation banner serve
  // both flows, and the banner re-renders inside the row so the
  // operator visually sees what they're about to act on.
  const [confirmKind, setConfirmKind] = useState<
    null | 'retry' | 'discard'
  >(null);
  const [confirmDlqId, setConfirmDlqId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [pendingDlqId, setPendingDlqId] = useState<string | null>(null);
  const pushToast = useToastStore((s) => s.push);

  const query = useSagaDLQ(ontology, { status });
  const retryMutation = useRetrySagaDLQ(ontology);
  const dropMutation = useDropSagaDLQ(ontology);

  const entries = useMemo(
    () => query.data?.entries ?? [],
    [query.data],
  );

  // Roving-tabindex refs + handler for the DLQ status-filter tablist.
  // Kept independent from the page's status tablist so arrow keys here
  // never move focus or selection over there (WAI-ARIA tabs pattern).
  const dlqTabRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const handleDlqTabKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
      const last = DLQ_STATUS_OPTIONS.length - 1;
      let nextIndex: number | null = null;
      switch (event.key) {
        case 'ArrowRight':
        case 'ArrowDown':
          nextIndex = index === last ? 0 : index + 1;
          break;
        case 'ArrowLeft':
        case 'ArrowUp':
          nextIndex = index === 0 ? last : index - 1;
          break;
        case 'Home':
          nextIndex = 0;
          break;
        case 'End':
          nextIndex = last;
          break;
        default:
          return;
      }
      event.preventDefault();
      setStatus(DLQ_STATUS_OPTIONS[nextIndex]);
      dlqTabRefs.current[nextIndex]?.focus();
    },
    [],
  );

  const closeConfirm = () => {
    setConfirmKind(null);
    setConfirmDlqId(null);
  };

  const beginConfirm = (kind: 'retry' | 'discard', dlqId: string) => {
    setActionError(null);
    setConfirmKind(kind);
    setConfirmDlqId(dlqId);
  };

  const performConfirmed = () => {
    if (!confirmKind || !confirmDlqId) return;
    const dlqId = confirmDlqId;
    setPendingDlqId(dlqId);
    const onSuccess = () => {
      pushToast({
        message:
          confirmKind === 'retry'
            ? `Replayed DLQ entry ${dlqId}.`
            : `Discarded DLQ entry ${dlqId}.`,
        severity: 'info',
      });
      closeConfirm();
    };
    const onError = (err: unknown) => {
      const msg = describeApiError(err);
      setActionError(msg);
      pushToast({ message: msg, severity: 'error' });
    };
    const onSettled = () => setPendingDlqId(null);
    if (confirmKind === 'retry') {
      retryMutation.mutate(dlqId, { onSuccess, onError, onSettled });
    } else {
      dropMutation.mutate(dlqId, { onSuccess, onError, onSettled });
    }
  };

  return (
    <DrawerShell
      testId="saga-dlq-drawer"
      title="Saga DLQ"
      onClose={onClose}
    >
      <div className="space-y-3 text-sm">
        <div
          className="flex items-center gap-1"
          role="tablist"
          aria-label="DLQ status filter"
          data-testid="saga-dlq-drawer-filters"
        >
          {DLQ_STATUS_OPTIONS.map((opt, index) => (
            <button
              key={opt}
              ref={(el) => {
                dlqTabRefs.current[index] = el;
              }}
              type="button"
              role="tab"
              aria-selected={status === opt}
              tabIndex={status === opt ? 0 : -1}
              data-testid={`saga-dlq-drawer-filter-${opt.toLowerCase()}`}
              onClick={() => setStatus(opt)}
              onKeyDown={(event) => handleDlqTabKeyDown(event, index)}
              className={`rounded-md px-3 py-1 text-xs font-medium transition-colors ${
                status === opt
                  ? 'bg-bg-tertiary text-text-primary'
                  : 'text-text-secondary hover:bg-bg-tertiary/60 hover:text-text-primary'
              }`}
            >
              {opt}
            </button>
          ))}
        </div>

        {actionError && (
          <div
            role="alert"
            data-testid="saga-dlq-drawer-error"
            className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
          >
            {actionError}
          </div>
        )}

        {query.isLoading ? (
          <div data-testid="saga-dlq-drawer-loading">
            <SkeletonTable rows={3} columns={3} aria-label="Loading DLQ" />
          </div>
        ) : query.isError ? (
          <div data-testid="saga-dlq-drawer-error-state">
            <EmptyState
              title="Failed to load DLQ"
              description={
                query.error instanceof Error
                  ? query.error.message
                  : 'Unexpected error.'
              }
            />
          </div>
        ) : entries.length === 0 ? (
          <div data-testid="saga-dlq-drawer-empty">
            <EmptyState
              title="No DLQ entries"
              description={
                status === 'PENDING'
                  ? 'No failed compensations are awaiting retry.'
                  : 'No entries match this filter.'
              }
            />
          </div>
        ) : (
          <ul
            data-testid="saga-dlq-drawer-list"
            aria-label="Saga DLQ entries"
            className="space-y-2"
          >
            {entries.map((entry) => (
              <li
                key={entry.dlqId}
                data-testid="saga-dlq-drawer-row"
                data-dlq-id={entry.dlqId}
                data-dlq-status={entry.status}
                className="rounded-md border border-border/40 bg-bg-secondary/40 p-3"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span
                    className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${DLQ_STATUS_BADGE_STYLE[entry.status]}`}
                  >
                    {entry.status}
                  </span>
                  <span className="font-mono text-xs text-text-primary truncate">
                    {entry.dlqId}
                  </span>
                  <span
                    data-testid="saga-dlq-drawer-attempts"
                    data-attempts={entry.attempts}
                    className="text-xs text-text-secondary"
                  >
                    {entry.attempts} attempt
                    {entry.attempts === 1 ? '' : 's'}
                  </span>
                </div>
                <dl className="mt-1 grid grid-cols-[max-content_1fr] gap-x-2 gap-y-0.5 text-xs text-text-secondary">
                  <dt>Saga</dt>
                  <dd className="font-mono text-text-primary">
                    {entry.sagaId}
                  </dd>
                  <dt>Step</dt>
                  <dd className="font-mono text-text-primary">
                    {entry.stepId}
                  </dd>
                  {entry.failureMessage && (
                    <>
                      <dt>Failure</dt>
                      <dd className="text-rose-300">{entry.failureMessage}</dd>
                    </>
                  )}
                </dl>
                {entry.status === 'PENDING' && (
                  <div className="mt-2 flex gap-2">
                    <button
                      type="button"
                      data-testid="saga-dlq-drawer-replay-btn"
                      data-dlq-id={entry.dlqId}
                      onClick={() => beginConfirm('retry', entry.dlqId)}
                      disabled={pendingDlqId === entry.dlqId}
                      className="rounded-md bg-amber-600 px-3 py-1 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
                    >
                      Replay
                    </button>
                    <button
                      type="button"
                      data-testid="saga-dlq-drawer-discard-btn"
                      data-dlq-id={entry.dlqId}
                      onClick={() => beginConfirm('discard', entry.dlqId)}
                      disabled={pendingDlqId === entry.dlqId}
                      className="rounded-md border border-rose-500/50 px-3 py-1 text-xs font-semibold text-rose-300 hover:bg-rose-500/10 disabled:opacity-60"
                    >
                      Discard
                    </button>
                  </div>
                )}
                {confirmDlqId === entry.dlqId && confirmKind !== null && (
                  <div
                    data-testid="saga-dlq-drawer-confirm"
                    data-confirm-kind={confirmKind}
                    data-dlq-id={entry.dlqId}
                    role="alertdialog"
                    aria-modal="true"
                    aria-label="Confirm DLQ action"
                    className="mt-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-100"
                  >
                    <p className="mb-2">
                      {confirmKind === 'retry'
                        ? `Replay this entry? The inverse-edits will be republished and the row will transition to RESOLVED on success.`
                        : `Discard this entry? The compensation will not be replayed and the row will transition to DROPPED.`}
                    </p>
                    <div className="flex gap-2">
                      <button
                        type="button"
                        data-testid="saga-dlq-drawer-confirm-yes-btn"
                        onClick={performConfirmed}
                        disabled={pendingDlqId === entry.dlqId}
                        className="rounded-md bg-amber-600 px-3 py-1 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
                      >
                        {pendingDlqId === entry.dlqId
                          ? 'Working…'
                          : confirmKind === 'retry'
                            ? 'Yes, replay'
                            : 'Yes, discard'}
                      </button>
                      <button
                        type="button"
                        data-testid="saga-dlq-drawer-confirm-no-btn"
                        onClick={closeConfirm}
                        disabled={pendingDlqId === entry.dlqId}
                        className="rounded-md border border-border/60 px-3 py-1 text-xs font-medium text-text-primary hover:bg-bg-tertiary disabled:opacity-60"
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </DrawerShell>
  );
}
