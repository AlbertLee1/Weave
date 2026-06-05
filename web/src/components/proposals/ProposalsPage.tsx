import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react';
import { useParams } from 'react-router';
import {
  useApproveProposal,
  useBranchBreakingChanges,
  useBranchDiff,
  useMergeProposal,
  useProposal,
  useProposals,
  useRejectProposal,
} from '../../hooks/useProposals';
import type {
  BranchDiffEntry,
  BreakingChange,
  OntologyProposal,
  ProposalReview,
} from '../../api/proposals';
import { ApiRequestError } from '../../api/client';
import { useToastStore } from '../../stores/toastStore';
import { EmptyState } from '../common/EmptyState';
import { SkeletonTable } from '../common/Skeleton';

// US-040 (PC-A02): Ontology Proposals & Merge UI.
//
// The page renders three pieces inside `/proposals/:ontology`:
//
//   1. Status-filter chips (open=pending / approved / rejected / merged / all).
//   2. Master list of proposals scoped to the active ontology.
//   3. Detail panel for the selected proposal: branch diff (ADDED /
//      MODIFIED / DELETED color-banded), breaking-changes warning banner
//      (driven by `/branches/{id}/breaking-changes`), reviewer history,
//      and the trio of mutating buttons — Approve, Reject, Merge.
//
// Merge is gated behind a typed-confirm dialog: the operator has to type
// the proposal title exactly before the Confirm button enables. This
// matches the PRD AC ("Merge 弹二次确认 (要求输入 proposal 名)").

type FilterStatus = OntologyProposal['status'] | 'all';

const STATUS_BADGE_STYLE: Record<OntologyProposal['status'], string> = {
  pending: 'bg-amber-500/10 text-amber-300 border border-amber-500/30',
  approved: 'bg-sky-500/10 text-sky-300 border border-sky-500/30',
  rejected: 'bg-rose-500/10 text-rose-300 border border-rose-500/30',
  merged: 'bg-emerald-500/10 text-emerald-300 border border-emerald-500/30',
};

const CHANGE_TYPE_STYLE: Record<BranchDiffEntry['changeType'], string> = {
  ADDED: 'bg-emerald-900/40 text-emerald-300 border border-emerald-700/40',
  MODIFIED: 'bg-amber-900/40 text-amber-300 border border-amber-700/40',
  DELETED: 'bg-rose-900/40 text-rose-300 border border-rose-700/40',
};

const FILTERS: Array<{ value: FilterStatus; label: string }> = [
  { value: 'all', label: 'All' },
  { value: 'pending', label: 'Open' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'merged', label: 'Merged' },
];

function describeApiError(err: unknown): string {
  if (err instanceof ApiRequestError) {
    const reason = err.parameters?.reason ?? err.parameters?.error;
    return reason ? `${err.errorName}: ${reason}` : err.errorName;
  }
  if (err instanceof Error) return err.message;
  return 'Operation failed.';
}

function formatTimestamp(value: string | undefined): string {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toLocaleString();
}

export function ProposalsPage() {
  const { ontology } = useParams<{ ontology: string }>();
  const activeOntology = ontology ?? '';

  const [filter, setFilter] = useState<FilterStatus>('all');
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // The list query passes status only when not "all"; computeProposalStatus
  // on the backend means the four discrete states map 1:1 to filter chips.
  const listQuery = useProposals(
    activeOntology,
    filter === 'all' ? {} : { status: filter },
  );

  const proposals = useMemo(
    () => listQuery.data?.data ?? [],
    [listQuery.data],
  );

  // Roving-tabindex refs for the status-filter tablist so keyboard navigation
  // can move DOM focus to the activated tab (WAI-ARIA tabs pattern).
  const filterTabRefs = useRef<(HTMLButtonElement | null)[]>([]);

  // ArrowLeft/Right (and Up/Down) move between tabs (wrapping), Home/End jump
  // to the ends. Activation is automatic: moving focus also selects (setFilter),
  // which is the recommended pattern for tablists whose panels render cheaply.
  const handleFilterTabKeyDown = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
      const last = FILTERS.length - 1;
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
      setFilter(FILTERS[nextIndex].value);
      filterTabRefs.current[nextIndex]?.focus();
    },
    [],
  );

  if (!activeOntology) {
    return (
      <div
        data-testid="proposals-empty-ontology"
        className="flex items-center justify-center h-full"
      >
        <EmptyState
          title="No Ontology Selected"
          description="Select an ontology from the Dashboard to manage proposals."
        />
      </div>
    );
  }

  return (
    <div
      data-testid="proposals-page"
      className="mx-auto max-w-7xl space-y-6"
    >
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
          Proposals
        </h1>
        <p className="text-sm text-text-secondary">
          Review and merge schema proposals for{' '}
          <span className="font-mono text-text-primary">{activeOntology}</span>
          . Each proposal wraps a branch overlay against main.
        </p>
      </header>

      <div
        data-testid="proposals-filters"
        className="flex flex-wrap gap-2"
        role="tablist"
        aria-label="Proposal status filter"
      >
        {FILTERS.map((f, index) => (
          <button
            key={f.value}
            ref={(el) => {
              filterTabRefs.current[index] = el;
            }}
            type="button"
            role="tab"
            aria-selected={filter === f.value}
            tabIndex={filter === f.value ? 0 : -1}
            data-testid="proposals-filter-btn"
            data-filter={f.value}
            data-active={filter === f.value ? 'true' : 'false'}
            onClick={() => setFilter(f.value)}
            onKeyDown={(event) => handleFilterTabKeyDown(event, index)}
            className={`rounded-full border px-3 py-1 text-xs font-medium transition-colors ${
              filter === f.value
                ? 'border-amber-500/50 bg-amber-500/10 text-amber-300'
                : 'border-border/60 text-text-secondary hover:bg-bg-tertiary'
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      <div className="grid gap-6 lg:grid-cols-[24rem_1fr]">
        <section
          data-testid="proposals-list-section"
          className="space-y-3"
          aria-label="Proposals"
        >
          {listQuery.isLoading ? (
            <div data-testid="proposals-loading">
              <SkeletonTable
                rows={4}
                columns={2}
                aria-label="Loading proposals"
              />
            </div>
          ) : listQuery.isError ? (
            <div data-testid="proposals-error">
              <EmptyState
                title="Failed to load proposals"
                description={
                  listQuery.error instanceof Error
                    ? listQuery.error.message
                    : 'Unexpected error.'
                }
              />
            </div>
          ) : proposals.length === 0 ? (
            <div data-testid="proposals-empty">
              <EmptyState
                title="No proposals match this filter"
                description="Create a proposal from a branch to surface it for review."
              />
            </div>
          ) : (
            <ul
              data-testid="proposals-list"
              className="space-y-2"
            >
              {proposals.map((p) => (
                <li key={p.id}>
                  <button
                    type="button"
                    data-testid="proposals-row"
                    data-proposal-id={p.id}
                    data-status={p.status}
                    data-selected={selectedId === p.id ? 'true' : 'false'}
                    onClick={() => setSelectedId(p.id)}
                    className={`w-full rounded-lg border px-3 py-2 text-left transition-colors ${
                      selectedId === p.id
                        ? 'border-amber-500/60 bg-bg-tertiary'
                        : 'border-border/40 bg-bg-secondary/60 hover:bg-bg-tertiary'
                    }`}
                  >
                    <div className="flex items-center gap-2">
                      <span
                        data-testid="proposals-row-status-badge"
                        className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[p.status]}`}
                      >
                        {p.status}
                      </span>
                      <span
                        data-testid="proposals-row-title"
                        className="text-sm font-medium text-text-primary truncate"
                      >
                        {p.title}
                      </span>
                    </div>
                    <p className="mt-1 text-xs text-text-secondary truncate">
                      branch{' '}
                      <span className="font-mono text-text-primary/80">
                        {p.branchId}
                      </span>
                      {p.author && (
                        <>
                          {' · '}
                          <span data-testid="proposals-row-author">
                            {p.author}
                          </span>
                        </>
                      )}
                    </p>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>

        <section
          data-testid="proposals-detail-section"
          className="min-h-[24rem]"
          aria-label="Proposal detail"
        >
          {selectedId ? (
            <ProposalDetail
              ontology={activeOntology}
              proposalId={selectedId}
              onCloseAfterMerge={() => setSelectedId(null)}
            />
          ) : (
            <div data-testid="proposals-detail-empty">
              <EmptyState
                title="Select a proposal"
                description="Pick a proposal from the list to see its diff, breaking changes, and review controls."
              />
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

interface ProposalDetailProps {
  ontology: string;
  proposalId: string;
  onCloseAfterMerge: () => void;
}

function ProposalDetail({
  ontology,
  proposalId,
  onCloseAfterMerge,
}: ProposalDetailProps) {
  const proposalQuery = useProposal(ontology, proposalId);
  const proposal = proposalQuery.data;
  const branchId = proposal?.branchId ?? null;

  const diffQuery = useBranchDiff(ontology, branchId);
  const breakingQuery = useBranchBreakingChanges(ontology, branchId);

  const approveMutation = useApproveProposal(ontology);
  const rejectMutation = useRejectProposal(ontology);
  const mergeMutation = useMergeProposal(ontology);
  const pushToast = useToastStore((s) => s.push);

  const [reviewer, setReviewer] = useState('admin@test');
  const [reviewReason, setReviewReason] = useState('');
  const [mergeDialogOpen, setMergeDialogOpen] = useState(false);
  const [mergeConfirmInput, setMergeConfirmInput] = useState('');

  if (proposalQuery.isLoading) {
    return (
      <div data-testid="proposals-detail-loading">
        <SkeletonTable rows={5} columns={2} aria-label="Loading proposal" />
      </div>
    );
  }
  if (proposalQuery.isError || !proposal) {
    return (
      <div data-testid="proposals-detail-error">
        <EmptyState
          title="Failed to load proposal"
          description={
            proposalQuery.error instanceof Error
              ? proposalQuery.error.message
              : 'Unexpected error.'
          }
        />
      </div>
    );
  }

  const canReview = proposal.status === 'pending';
  const canApprove = proposal.status === 'pending';
  const canReject = proposal.status === 'pending';
  const canMerge = proposal.status === 'approved';

  const onApprove = () => {
    if (!reviewer.trim()) {
      pushToast({
        message: 'Reviewer is required.',
        severity: 'error',
      });
      return;
    }
    approveMutation.mutate(
      {
        proposalId,
        body: { reviewer: reviewer.trim(), reason: reviewReason },
      },
      {
        onSuccess: () => {
          pushToast({
            message: `Approved "${proposal.title}".`,
            severity: 'success',
          });
          setReviewReason('');
        },
        onError: (err) =>
          pushToast({ message: describeApiError(err), severity: 'error' }),
      },
    );
  };

  const onReject = () => {
    if (!reviewer.trim()) {
      pushToast({
        message: 'Reviewer is required.',
        severity: 'error',
      });
      return;
    }
    rejectMutation.mutate(
      {
        proposalId,
        body: { reviewer: reviewer.trim(), reason: reviewReason },
      },
      {
        onSuccess: () => {
          pushToast({
            message: `Rejected "${proposal.title}".`,
            severity: 'info',
          });
          setReviewReason('');
        },
        onError: (err) =>
          pushToast({ message: describeApiError(err), severity: 'error' }),
      },
    );
  };

  const onMergeConfirmed = () => {
    mergeMutation.mutate(proposalId, {
      onSuccess: () => {
        pushToast({
          message: `Merged "${proposal.title}".`,
          severity: 'success',
        });
        setMergeDialogOpen(false);
        setMergeConfirmInput('');
        // Clear the detail panel so the operator can navigate to the
        // next proposal without seeing stale state.
        onCloseAfterMerge();
      },
      onError: (err) => {
        pushToast({ message: describeApiError(err), severity: 'error' });
      },
    });
  };

  return (
    <div
      data-testid="proposals-detail"
      data-proposal-id={proposal.id}
      data-status={proposal.status}
      className="space-y-5 rounded-lg border border-border/40 bg-bg-secondary/60 p-5"
    >
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <span
              data-testid="proposals-detail-status-badge"
              className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[proposal.status]}`}
            >
              {proposal.status}
            </span>
            <h2
              data-testid="proposals-detail-title"
              className="text-base font-semibold text-text-primary"
            >
              {proposal.title}
            </h2>
          </div>
          <p className="text-xs text-text-secondary">
            branch{' '}
            <span className="font-mono text-text-primary/80">
              {proposal.branchId}
            </span>
            {proposal.author ? (
              <>
                {' · author '}
                <span data-testid="proposals-detail-author">
                  {proposal.author}
                </span>
              </>
            ) : null}
            {proposal.createdAt ? (
              <>
                {' · '}
                {formatTimestamp(proposal.createdAt)}
              </>
            ) : null}
          </p>
          {proposal.description ? (
            <p
              data-testid="proposals-detail-description"
              className="text-sm text-text-secondary"
            >
              {proposal.description}
            </p>
          ) : null}
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          <button
            type="button"
            data-testid="proposals-approve-btn"
            disabled={!canApprove || approveMutation.isPending}
            onClick={onApprove}
            className="rounded-md border border-emerald-500/40 bg-emerald-500/10 px-3 py-1.5 text-xs font-medium text-emerald-300 hover:bg-emerald-500/20 disabled:opacity-50"
          >
            Approve
          </button>
          <button
            type="button"
            data-testid="proposals-reject-btn"
            disabled={!canReject || rejectMutation.isPending}
            onClick={onReject}
            className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-1.5 text-xs font-medium text-rose-300 hover:bg-rose-500/20 disabled:opacity-50"
          >
            Reject
          </button>
          <button
            type="button"
            data-testid="proposals-merge-btn"
            disabled={!canMerge || mergeMutation.isPending}
            onClick={() => {
              setMergeConfirmInput('');
              setMergeDialogOpen(true);
            }}
            className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-50"
          >
            Merge
          </button>
        </div>
      </header>

      <BreakingChangesBanner query={breakingQuery} />

      {canReview && (
        <div
          data-testid="proposals-review-form"
          className="space-y-2 rounded-md border border-border/40 bg-bg-tertiary/40 p-3"
        >
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Reviewer
            </span>
            <input
              type="text"
              data-testid="proposals-reviewer-input"
              value={reviewer}
              onChange={(e) => setReviewer(e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
            />
          </label>
          <label className="block">
            <span className="block text-xs font-medium text-text-secondary mb-1">
              Reason (optional)
            </span>
            <input
              type="text"
              data-testid="proposals-reason-input"
              value={reviewReason}
              onChange={(e) => setReviewReason(e.target.value)}
              className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm text-text-primary"
            />
          </label>
        </div>
      )}

      <BranchDiffSection query={diffQuery} />

      <ReviewHistorySection reviews={proposal.reviews} />

      {mergeDialogOpen && (
        <MergeConfirmDialog
          proposalTitle={proposal.title}
          inputValue={mergeConfirmInput}
          setInputValue={setMergeConfirmInput}
          onCancel={() => {
            setMergeDialogOpen(false);
            setMergeConfirmInput('');
          }}
          onConfirm={onMergeConfirmed}
          merging={mergeMutation.isPending}
        />
      )}
    </div>
  );
}

function BreakingChangesBanner({
  query,
}: {
  query: ReturnType<typeof useBranchBreakingChanges>;
}) {
  if (query.isLoading) {
    return (
      <div
        data-testid="proposals-breaking-loading"
        className="text-xs text-text-secondary"
      >
        Checking for breaking changes…
      </div>
    );
  }
  if (query.isError) {
    return (
      <div
        data-testid="proposals-breaking-error"
        className="rounded-md border border-rose-500/30 bg-rose-500/10 p-3 text-xs text-rose-200"
      >
        Failed to load breaking-change analysis.
      </div>
    );
  }
  const report = query.data;
  if (!report) return null;
  if (report.changes.length === 0) {
    return (
      <div
        data-testid="proposals-breaking-clean"
        className="rounded-md border border-emerald-500/30 bg-emerald-500/10 p-3 text-xs text-emerald-200"
      >
        No breaking changes detected on this branch.
      </div>
    );
  }
  return (
    <div
      data-testid="proposals-breaking-banner"
      data-changes-count={report.changes.length}
      role="alert"
      className="rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-xs text-amber-200"
    >
      <div className="font-semibold text-amber-100">
        ⚠ {report.changes.length} breaking change
        {report.changes.length === 1 ? '' : 's'} detected
      </div>
      <ul className="mt-2 space-y-1" data-testid="proposals-breaking-list">
        {report.changes.map((c, idx) => (
          <li
            key={`${c.kind}-${c.propertyApiName ?? c.objectTypeApiName ?? idx}`}
            data-testid="proposals-breaking-item"
            data-kind={c.kind}
          >
            <BreakingChangeRow change={c} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function BreakingChangeRow({ change }: { change: BreakingChange }) {
  const ot = change.objectTypeApiName ?? change.objectTypeRid ?? '';
  const prop = change.propertyApiName ? `.${change.propertyApiName}` : '';
  return (
    <>
      <span className="font-mono text-amber-100">{change.kind}</span>
      {ot ? (
        <>
          {' on '}
          <span className="font-mono text-amber-100">
            {ot}
            {prop}
          </span>
        </>
      ) : null}
      {change.detail ? <> — {change.detail}</> : null}
    </>
  );
}

function BranchDiffSection({
  query,
}: {
  query: ReturnType<typeof useBranchDiff>;
}) {
  if (query.isLoading) {
    return (
      <div data-testid="proposals-diff-loading">
        <SkeletonTable rows={4} columns={2} aria-label="Loading diff" />
      </div>
    );
  }
  if (query.isError) {
    return (
      <div data-testid="proposals-diff-error">
        <EmptyState
          title="Failed to load diff"
          description={
            query.error instanceof Error
              ? query.error.message
              : 'Unexpected error.'
          }
        />
      </div>
    );
  }
  const entries: BranchDiffEntry[] = query.data?.data ?? [];
  if (entries.length === 0) {
    return (
      <div
        data-testid="proposals-diff-empty"
        className="text-xs text-text-secondary"
      >
        This branch has no recorded changes.
      </div>
    );
  }

  const added = entries.filter((e) => e.changeType === 'ADDED');
  const modified = entries.filter((e) => e.changeType === 'MODIFIED');
  const deleted = entries.filter((e) => e.changeType === 'DELETED');

  return (
    <div data-testid="proposals-diff" className="space-y-3">
      <div className="flex flex-wrap gap-2 text-[11px]">
        <DiffSummaryPill
          label="Added"
          count={added.length}
          variant="ADDED"
        />
        <DiffSummaryPill
          label="Modified"
          count={modified.length}
          variant="MODIFIED"
        />
        <DiffSummaryPill
          label="Deleted"
          count={deleted.length}
          variant="DELETED"
        />
      </div>
      <ul className="space-y-2" data-testid="proposals-diff-list">
        {entries.map((entry) => (
          <li
            key={`${entry.changeType}:${entry.entityType}:${entry.entityRid}`}
            data-testid="proposals-diff-row"
            data-change-type={entry.changeType}
            data-entity-type={entry.entityType}
            className={`rounded-md border px-3 py-2 text-xs ${CHANGE_TYPE_STYLE[entry.changeType]}`}
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono uppercase tracking-wider">
                {entry.changeType}
              </span>
              <span className="font-mono">{entry.entityType}</span>
              <span className="font-mono text-text-secondary truncate">
                {entry.entityRid}
              </span>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function DiffSummaryPill({
  label,
  count,
  variant,
}: {
  label: string;
  count: number;
  variant: BranchDiffEntry['changeType'];
}) {
  return (
    <span
      data-testid="proposals-diff-summary"
      data-variant={variant}
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-semibold uppercase tracking-wider ${CHANGE_TYPE_STYLE[variant]}`}
    >
      {label}
      <span className="font-mono">{count}</span>
    </span>
  );
}

function ReviewHistorySection({ reviews }: { reviews: ProposalReview[] }) {
  if (!reviews || reviews.length === 0) {
    return (
      <div
        data-testid="proposals-reviews-empty"
        className="text-xs text-text-secondary"
      >
        No reviews yet.
      </div>
    );
  }
  return (
    <div data-testid="proposals-reviews" className="space-y-2">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-text-secondary">
        Review history
      </h3>
      <ul className="space-y-1">
        {reviews.map((r) => (
          <li
            key={r.id}
            data-testid="proposals-review-row"
            data-decision={r.decision}
            className="flex items-baseline gap-2 text-xs"
          >
            <span
              data-testid="proposals-review-decision"
              className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${
                r.decision === 'approve'
                  ? 'bg-emerald-500/10 text-emerald-300 border border-emerald-500/30'
                  : 'bg-rose-500/10 text-rose-300 border border-rose-500/30'
              }`}
            >
              {r.decision}
            </span>
            <span className="font-mono text-text-primary">{r.reviewer}</span>
            {r.reason ? (
              <span className="text-text-secondary">— {r.reason}</span>
            ) : null}
            <span className="ml-auto text-text-secondary">
              {formatTimestamp(r.createdAt)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

interface MergeConfirmDialogProps {
  proposalTitle: string;
  inputValue: string;
  setInputValue: (v: string) => void;
  onCancel: () => void;
  onConfirm: () => void;
  merging: boolean;
}

// Elements that can receive keyboard focus, used by the merge dialog's focus
// trap (mirrors VertexShareLinkPanel — see its FOCUSABLE_SELECTOR).
const MERGE_DIALOG_FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function MergeConfirmDialog({
  proposalTitle,
  inputValue,
  setInputValue,
  onCancel,
  onConfirm,
  merging,
}: MergeConfirmDialogProps) {
  // Typed-confirm gate: the Confirm button only enables when the operator
  // types the proposal title exactly. Strict equality (not lower-case
  // comparison) matches GitHub's typed-confirm UX — the wrong case stays
  // disabled to keep the gesture deliberate.
  const matches = inputValue === proposalTitle;

  // Focus management for this self-drawn dialog (it is NOT the shared
  // common/Modal, which already traps + restores focus). Mirrors
  // VertexShareLinkPanel (#229): on open we restore focus to the trigger on
  // close, keep Tab/Shift+Tab cycling within the dialog (degrades safely), and
  // close on Escape via the existing cancel callback. The input's existing
  // autoFocus still provides the initial focus inside the dialog.
  const dialogRef = useRef<HTMLDivElement>(null);
  // Record the element that opened the dialog (typically the Merge button) so
  // we can restore focus to it on close. We capture it lazily during the first
  // render — before the dialog's children commit — because the title input's
  // autoFocus steals document.activeElement by the time a mount effect runs,
  // which would otherwise leave us pointing at the input instead of the
  // trigger. useRef's initializer runs exactly once.
  const triggerRef = useRef<HTMLElement | null>(
    typeof document !== 'undefined'
      ? (document.activeElement as HTMLElement | null)
      : null,
  );
  // Keep the latest onCancel in a ref so the Escape listener (registered once)
  // always invokes the current callback without re-subscribing.
  const onCancelRef = useRef(onCancel);
  onCancelRef.current = onCancel;

  // Restore focus to the trigger when the dialog unmounts. The parent
  // conditionally mounts/unmounts this component on open/close, so
  // mount == open and unmount == close.
  useEffect(() => {
    return () => {
      const trigger = triggerRef.current;
      if (trigger && typeof trigger.focus === 'function') trigger.focus();
    };
  }, []);

  // Escape closes the dialog through the existing cancel path.
  useEffect(() => {
    function handleKey(e: globalThis.KeyboardEvent) {
      if (e.key === 'Escape') onCancelRef.current();
    }
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, []);

  // Focus trap: keep Tab / Shift+Tab cycling among the dialog's focusable
  // elements instead of escaping to the background page.
  const handleTrapKeyDown = useCallback((e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Tab') return;
    const dialog = dialogRef.current;
    if (!dialog) return;
    const focusables = Array.from(
      dialog.querySelectorAll<HTMLElement>(MERGE_DIALOG_FOCUSABLE_SELECTOR),
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
  }, []);

  return (
    <div
      ref={dialogRef}
      data-testid="proposals-merge-dialog"
      role="dialog"
      aria-modal="true"
      aria-label="Confirm merge"
      tabIndex={-1}
      onKeyDown={handleTrapKeyDown}
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/60"
    >
      <div className="w-[28rem] max-w-full space-y-4 rounded-lg border border-border/60 bg-bg-primary p-5 shadow-xl">
        <header className="space-y-1">
          <h2 className="text-base font-semibold text-text-primary">
            Confirm merge
          </h2>
          <p className="text-xs text-text-secondary">
            This will apply every branch change to main and bump the
            ontology version. Type the proposal title to confirm.
          </p>
        </header>
        <label className="block">
          <span className="block text-[11px] font-medium text-text-secondary mb-1">
            Proposal title to confirm:{' '}
            <span
              data-testid="proposals-merge-dialog-expected"
              className="font-mono text-text-primary"
            >
              {proposalTitle}
            </span>
          </span>
          <input
            type="text"
            data-testid="proposals-merge-dialog-input"
            autoFocus
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            className="block w-full rounded-md border border-border/60 bg-bg-secondary/60 px-3 py-1.5 text-sm font-mono text-text-primary"
          />
        </label>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            data-testid="proposals-merge-dialog-cancel-btn"
            onClick={onCancel}
            className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-medium text-text-primary hover:bg-bg-tertiary"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="proposals-merge-dialog-confirm-btn"
            disabled={!matches || merging}
            onClick={onConfirm}
            className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-50"
          >
            {merging ? 'Merging…' : 'Confirm merge'}
          </button>
        </div>
      </div>
    </div>
  );
}
