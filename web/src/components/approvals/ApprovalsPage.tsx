import { useMemo, useState } from 'react';
import { useParams } from 'react-router';
import type { ActionApproval } from '../../api/approvals';
import { ApiRequestError } from '../../api/client';
import {
  useApprovals,
  useApproveAction,
  useRejectAction,
} from '../../hooks/useApprovals';
import { SkeletonTable } from '../common/Skeleton';
import { EmptyState } from '../common/EmptyState';
import { Modal } from '../common/Modal';
import { useOntologyStore } from '../../stores/ontologyStore';

type StatusFilter = 'PENDING' | 'APPROVED' | 'REJECTED' | 'ALL';

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'PENDING', label: 'Pending' },
  { value: 'APPROVED', label: 'Approved' },
  { value: 'REJECTED', label: 'Rejected' },
  { value: 'ALL', label: 'All' },
];

const STATUS_BADGE_STYLE: Record<ActionApproval['status'], string> = {
  PENDING:
    'bg-amber-500/10 text-amber-400 border border-amber-500/30',
  APPROVED:
    'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  REJECTED: 'bg-rose-500/10 text-rose-400 border border-rose-500/30',
};

export function ApprovalsPage() {
  const { ontology } = useParams<{ ontology?: string }>();
  const selectedOntology = useOntologyStore((s) => s.selectedOntology);
  const activeOntology = ontology ?? selectedOntology ?? '';

  const [status, setStatus] = useState<StatusFilter>('PENDING');
  const [mine, setMine] = useState(true);
  const [reviewTarget, setReviewTarget] = useState<
    | { approval: ActionApproval; decision: 'APPROVE' | 'REJECT' }
    | null
  >(null);
  const [reason, setReason] = useState('');
  const [reviewError, setReviewError] = useState<string | null>(null);

  const listQuery = useApprovals(activeOntology, {
    status,
    mine,
  });
  const approveMutation = useApproveAction(activeOntology);
  const rejectMutation = useRejectAction(activeOntology);

  const approvals = useMemo(() => listQuery.data?.data ?? [], [listQuery.data]);

  if (!activeOntology) {
    return (
      <div
        className="flex items-center justify-center h-full"
        data-testid="approvals-no-ontology"
      >
        <EmptyState
          title="No Ontology Selected"
          description="Select an ontology from the Dashboard to view its approval queue."
        />
      </div>
    );
  }

  const openReview = (approval: ActionApproval, decision: 'APPROVE' | 'REJECT') => {
    setReviewTarget({ approval, decision });
    setReason('');
    setReviewError(null);
  };

  const closeReview = () => {
    setReviewTarget(null);
    setReason('');
    setReviewError(null);
  };

  const handleSubmit = () => {
    if (!reviewTarget) return;
    setReviewError(null);
    const variables = {
      approvalId: reviewTarget.approval.id,
      reason: reason.trim() || undefined,
    };
    const onErr = (err: unknown) => {
      if (err instanceof ApiRequestError) {
        setReviewError(`${err.errorName}: ${err.parameters?.error ?? err.message}`);
      } else if (err instanceof Error) {
        setReviewError(err.message);
      } else {
        setReviewError('Review failed.');
      }
    };
    if (reviewTarget.decision === 'APPROVE') {
      approveMutation.mutate(variables, {
        onSuccess: closeReview,
        onError: onErr,
      });
    } else {
      rejectMutation.mutate(variables, {
        onSuccess: closeReview,
        onError: onErr,
      });
    }
  };

  const reviewInFlight =
    approveMutation.isPending || rejectMutation.isPending;

  return (
    <div className="mx-auto max-w-6xl space-y-6" data-testid="approvals-page">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
          Approval Queue
        </h1>
        <p className="text-sm text-text-secondary">
          Review actions awaiting approval for{' '}
          <span className="font-mono text-text-primary">{activeOntology}</span>.
        </p>
      </header>

      <section
        className="flex flex-wrap items-center gap-3 rounded-lg border border-border/50 bg-bg-secondary/60 p-3"
        data-testid="approvals-filters"
      >
        <div className="flex items-center gap-1" role="tablist" aria-label="Status filter">
          {STATUS_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type="button"
              role="tab"
              aria-selected={status === opt.value}
              onClick={() => setStatus(opt.value)}
              data-testid={`filter-status-${opt.value.toLowerCase()}`}
              className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                status === opt.value
                  ? 'bg-bg-tertiary text-text-primary'
                  : 'text-text-secondary hover:bg-bg-tertiary/60 hover:text-text-primary'
              }`}
            >
              {opt.label}
            </button>
          ))}
        </div>
        <label className="ml-auto flex items-center gap-2 text-xs text-text-secondary">
          <input
            type="checkbox"
            checked={mine}
            onChange={(e) => setMine(e.target.checked)}
            data-testid="approvals-mine-toggle"
            className="h-3.5 w-3.5 rounded border-border/60 bg-bg-primary text-amber-500"
          />
          Only rows I can review
        </label>
      </section>

      {listQuery.isLoading ? (
        <div data-testid="approvals-loading">
          <SkeletonTable rows={6} columns={4} aria-label="Loading approvals" />
        </div>
      ) : listQuery.isError ? (
        <div data-testid="approvals-error">
          <EmptyState
            title="Failed to load approvals"
            description={
              listQuery.error instanceof Error
                ? listQuery.error.message
                : 'Unexpected error.'
            }
          />
        </div>
      ) : approvals.length === 0 ? (
        <div data-testid="approvals-empty">
          <EmptyState
            title="No approvals match"
            description={
              status === 'PENDING'
                ? 'Nothing is waiting for review right now.'
                : 'Try a different status filter.'
            }
          />
        </div>
      ) : (
        <ul
          className="space-y-3"
          data-testid="approvals-list"
          aria-label="Approval queue"
        >
          {approvals.map((approval) => (
            <ApprovalCard
              key={approval.id}
              approval={approval}
              onApprove={() => openReview(approval, 'APPROVE')}
              onReject={() => openReview(approval, 'REJECT')}
            />
          ))}
        </ul>
      )}

      <Modal
        open={reviewTarget !== null}
        onClose={closeReview}
        title={
          reviewTarget?.decision === 'APPROVE'
            ? `Approve: ${reviewTarget.approval.actionType}`
            : reviewTarget
            ? `Reject: ${reviewTarget.approval.actionType}`
            : ''
        }
      >
        {reviewTarget && (
          <div className="space-y-4">
            <div className="rounded-md border border-border/40 bg-bg-primary/80 p-3 text-xs">
              <div className="mb-2 text-text-secondary">
                Action
                <span className="ml-2 font-mono text-text-primary">
                  {reviewTarget.approval.actionType}
                </span>
              </div>
              <div className="text-text-secondary">
                Requested by
                <span className="ml-2 text-text-primary">
                  {reviewTarget.approval.requestedBy || '—'}
                </span>
              </div>
            </div>
            <label
              className="flex flex-col gap-1.5 text-xs text-text-secondary"
              htmlFor="review-reason"
            >
              Reason (optional)
              <textarea
                id="review-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                rows={3}
                placeholder={
                  reviewTarget.decision === 'APPROVE'
                    ? 'LGTM / notes for audit trail'
                    : 'Why is this request being rejected?'
                }
                className="w-full rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
                data-testid="review-reason-input"
              />
            </label>
            {reviewError && (
              <div
                role="alert"
                className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
              >
                {reviewError}
              </div>
            )}
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={closeReview}
                className="rounded-md border border-border/60 px-3 py-1.5 text-xs text-text-secondary hover:bg-bg-tertiary"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={handleSubmit}
                disabled={reviewInFlight}
                data-testid="review-submit"
                className={`rounded-md px-3 py-1.5 text-xs font-semibold text-white disabled:opacity-60 ${
                  reviewTarget.decision === 'APPROVE'
                    ? 'bg-emerald-600 hover:bg-emerald-500'
                    : 'bg-rose-600 hover:bg-rose-500'
                }`}
              >
                {reviewInFlight
                  ? 'Submitting…'
                  : reviewTarget.decision === 'APPROVE'
                  ? 'Approve'
                  : 'Reject'}
              </button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}

interface ApprovalCardProps {
  approval: ActionApproval;
  onApprove: () => void;
  onReject: () => void;
}

function ApprovalCard({ approval, onApprove, onReject }: ApprovalCardProps) {
  const paramsText = useMemo(() => {
    if (approval.parameters === undefined || approval.parameters === null) {
      return '(no parameters)';
    }
    try {
      return JSON.stringify(approval.parameters, null, 2);
    } catch {
      return String(approval.parameters);
    }
  }, [approval.parameters]);

  const isPending = approval.status === 'PENDING';

  return (
    <li
      className="rounded-lg border border-border/50 bg-bg-secondary/60 p-4"
      data-testid="approval-card"
      data-approval-id={approval.id}
    >
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-1 space-y-1">
          <div className="flex items-center gap-2">
            <span
              className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[approval.status]}`}
            >
              {approval.status}
            </span>
            <h2 className="font-mono text-sm text-text-primary">
              {approval.actionType}
            </h2>
          </div>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-xs text-text-secondary">
            <dt>Requested by</dt>
            <dd className="text-text-primary">{approval.requestedBy || '—'}</dd>
            <dt>Approvers</dt>
            <dd className="font-mono text-text-primary">
              {(approval.approvers ?? []).join(', ') || '—'}
            </dd>
            <dt>Requested at</dt>
            <dd className="text-text-primary">
              {new Date(approval.createdAt).toLocaleString()}
            </dd>
            {approval.reviewedBy && (
              <>
                <dt>Reviewed by</dt>
                <dd className="text-text-primary">{approval.reviewedBy}</dd>
              </>
            )}
            {approval.reason && (
              <>
                <dt>Reason</dt>
                <dd className="text-text-primary">{approval.reason}</dd>
              </>
            )}
          </dl>
        </div>
        {isPending && (
          <div className="flex shrink-0 gap-2">
            <button
              type="button"
              onClick={onApprove}
              data-testid="approval-approve-btn"
              className="rounded-md bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500"
            >
              Approve
            </button>
            <button
              type="button"
              onClick={onReject}
              data-testid="approval-reject-btn"
              className="rounded-md border border-rose-500/50 px-3 py-1.5 text-xs font-semibold text-rose-300 hover:bg-rose-500/10"
            >
              Reject
            </button>
          </div>
        )}
      </div>
      <details className="mt-3 text-xs">
        <summary className="cursor-pointer text-text-secondary hover:text-text-primary">
          Parameters
        </summary>
        <pre
          className="mt-2 max-h-64 overflow-auto rounded-md border border-border/40 bg-bg-primary/70 p-3 font-mono text-[11px] text-text-primary"
          data-testid="approval-parameters"
        >
          {paramsText}
        </pre>
      </details>
    </li>
  );
}
