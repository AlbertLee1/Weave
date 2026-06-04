import { useMemo, useState } from 'react';
import { ApiRequestError } from '../../api/client';
import type {
  PermissionRequest,
  PermissionRequestStatus,
} from '../../api/permissionRequests';
import {
  useApprovePermissionRequest,
  useCancelPermissionRequest,
  useCreatePermissionRequest,
  usePermissionRequests,
  useRejectPermissionRequest,
} from '../../hooks/usePermissionRequests';
import { SkeletonTable } from '../common/Skeleton';
import { EmptyState } from '../common/EmptyState';
import { Modal } from '../common/Modal';

type StatusFilter = PermissionRequestStatus | 'ALL';

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'PENDING', label: 'Pending' },
  { value: 'APPROVED', label: 'Approved' },
  { value: 'REJECTED', label: 'Rejected' },
  { value: 'CANCELLED', label: 'Cancelled' },
  { value: 'ALL', label: 'All' },
];

const STATUS_BADGE_STYLE: Record<PermissionRequestStatus, string> = {
  PENDING: 'bg-amber-500/10 text-amber-400 border border-amber-500/30',
  APPROVED: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  REJECTED: 'bg-rose-500/10 text-rose-400 border border-rose-500/30',
  CANCELLED: 'bg-slate-500/10 text-slate-400 border border-slate-500/30',
};

// PAGE_SIZE bounds each list page. The backend clamps limit to
// [1, MaxPageLimit=200]; 25 keeps the inbox scannable while exercising
// the limit/offset paging the API exposes.
const PAGE_SIZE = 25;

// PermissionRequestsPage renders the share-link permission inbox
// (US-339). Approvers see every PENDING request; non-approvers see only
// their own. Both can submit a new request via the "Request access"
// button, which opens a small dialog accepting a target RID + reason.
// Approvers also see Approve / Reject buttons inline on each PENDING row.
export function PermissionRequestsPage() {
  const [status, setStatus] = useState<StatusFilter>('PENDING');
  const [mineOnly, setMineOnly] = useState(false);
  const [targetRid, setTargetRid] = useState('');
  const [offset, setOffset] = useState(0);
  const [createOpen, setCreateOpen] = useState(false);
  const [createTarget, setCreateTarget] = useState('');
  const [createReason, setCreateReason] = useState('');
  const [createError, setCreateError] = useState<string | null>(null);
  const [reviewTarget, setReviewTarget] = useState<
    { request: PermissionRequest; decision: 'APPROVE' | 'REJECT' } | null
  >(null);
  const [reviewNote, setReviewNote] = useState('');
  const [reviewError, setReviewError] = useState<string | null>(null);

  const trimmedTargetRid = targetRid.trim();

  // Changing any server-side filter invalidates the current page window,
  // so snap back to the first page to avoid landing on an empty offset.
  // Resetting in the setter (rather than a post-render effect) keeps the
  // offset and the filter that produced it in a single render pass.
  const changeStatus = (next: StatusFilter) => {
    setStatus(next);
    setOffset(0);
  };
  const changeMineOnly = (next: boolean) => {
    setMineOnly(next);
    setOffset(0);
  };
  const changeTargetRid = (next: string) => {
    setTargetRid(next);
    setOffset(0);
  };

  const listQuery = usePermissionRequests({
    status: status === 'ALL' ? undefined : status,
    mine: mineOnly || undefined,
    targetRid: trimmedTargetRid || undefined,
    limit: PAGE_SIZE,
    offset,
  });
  const createMutation = useCreatePermissionRequest();
  const approveMutation = useApprovePermissionRequest();
  const rejectMutation = useRejectPermissionRequest();
  const cancelMutation = useCancelPermissionRequest();

  const requests = useMemo(
    () => listQuery.data?.requests ?? [],
    [listQuery.data?.requests],
  );
  const available = listQuery.data?.available ?? true;
  const total = listQuery.data?.total ?? 0;
  const hasPrev = offset > 0;
  const hasNext = offset + requests.length < total;
  const pageStart = total === 0 ? 0 : offset + 1;
  const pageEnd = offset + requests.length;

  const openCreate = () => {
    setCreateTarget('');
    setCreateReason('');
    setCreateError(null);
    setCreateOpen(true);
  };

  const closeCreate = () => {
    setCreateOpen(false);
    setCreateError(null);
  };

  const submitCreate = () => {
    setCreateError(null);
    if (!createTarget.trim()) {
      setCreateError('Target RID is required.');
      return;
    }
    createMutation.mutate(
      { targetRid: createTarget.trim(), reason: createReason.trim() || undefined },
      {
        onSuccess: () => {
          closeCreate();
        },
        onError: (err) => {
          if (err instanceof ApiRequestError) {
            setCreateError(`${err.errorName}: ${err.message}`);
          } else if (err instanceof Error) {
            setCreateError(err.message);
          } else {
            setCreateError('Request failed.');
          }
        },
      },
    );
  };

  const openReview = (req: PermissionRequest, decision: 'APPROVE' | 'REJECT') => {
    setReviewTarget({ request: req, decision });
    setReviewNote('');
    setReviewError(null);
  };

  const closeReview = () => {
    setReviewTarget(null);
    setReviewNote('');
    setReviewError(null);
  };

  const submitReview = () => {
    if (!reviewTarget) return;
    setReviewError(null);
    const variables = {
      id: reviewTarget.request.id,
      note: reviewNote.trim() || undefined,
    };
    const onErr = (err: unknown) => {
      if (err instanceof ApiRequestError) {
        setReviewError(`${err.errorName}: ${err.message}`);
      } else if (err instanceof Error) {
        setReviewError(err.message);
      } else {
        setReviewError('Review failed.');
      }
    };
    if (reviewTarget.decision === 'APPROVE') {
      approveMutation.mutate(variables, { onSuccess: closeReview, onError: onErr });
    } else {
      rejectMutation.mutate(variables, { onSuccess: closeReview, onError: onErr });
    }
  };

  const reviewInFlight = approveMutation.isPending || rejectMutation.isPending;

  return (
    <div className="mx-auto max-w-6xl space-y-6" data-testid="permission-requests-page">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold text-text-primary tracking-tight">
            Permission Requests
          </h1>
          <p className="text-sm text-text-secondary">
            Share-link access workflow — request access to a resource, approve
            or reject incoming requests.
          </p>
        </div>
        <button
          type="button"
          onClick={openCreate}
          data-testid="permission-request-create"
          className="rounded-md bg-amber-600 px-3 py-2 text-xs font-semibold text-white hover:bg-amber-500"
        >
          Request access
        </button>
      </header>

      {!available && (
        <EmptyState
          title="Permission Requests unavailable"
          description="This deployment does not have a permission_requests store wired. Requests cannot be submitted or reviewed."
        />
      )}

      <section
        className="flex flex-wrap items-center gap-3 rounded-lg border border-border/50 bg-bg-secondary/60 p-3"
        data-testid="permission-requests-filters"
      >
        <div className="flex items-center gap-1" role="tablist" aria-label="Status filter">
          {STATUS_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type="button"
              role="tab"
              aria-selected={status === opt.value}
              onClick={() => changeStatus(opt.value)}
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
        <label className="flex items-center gap-2 text-xs text-text-secondary">
          <span className="sr-only">Filter by target RID</span>
          <input
            type="text"
            value={targetRid}
            onChange={(e) => changeTargetRid(e.target.value)}
            placeholder="Filter by target RID…"
            aria-label="Filter by target RID"
            data-testid="permission-requests-targetrid-input"
            className="w-64 rounded-md border border-border/50 bg-bg-primary px-2.5 py-1.5 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
          />
          {trimmedTargetRid && (
            <button
              type="button"
              onClick={() => changeTargetRid('')}
              data-testid="permission-requests-targetrid-clear"
              className="rounded-md border border-border/60 px-2 py-1 text-[11px] text-text-secondary hover:bg-bg-tertiary"
            >
              Clear
            </button>
          )}
        </label>
        <label className="ml-auto flex items-center gap-2 text-xs text-text-secondary">
          <input
            type="checkbox"
            checked={mineOnly}
            onChange={(e) => changeMineOnly(e.target.checked)}
            data-testid="permission-requests-mine-toggle"
            className="h-3.5 w-3.5 rounded border-border/60 bg-bg-primary text-amber-500"
          />
          Only my requests
        </label>
      </section>

      {listQuery.isLoading ? (
        <SkeletonTable rows={6} columns={4} aria-label="Loading permission requests" />
      ) : listQuery.isError ? (
        <EmptyState
          title="Failed to load permission requests"
          description={
            listQuery.error instanceof Error
              ? listQuery.error.message
              : 'Unexpected error.'
          }
        />
      ) : requests.length === 0 ? (
        <EmptyState
          title="No permission requests"
          description={
            status === 'PENDING'
              ? 'Nothing is waiting for review right now.'
              : 'Try a different status filter.'
          }
        />
      ) : (
        <ul
          className="space-y-3"
          data-testid="permission-requests-list"
          aria-label="Permission requests"
        >
          {requests.map((req) => (
            <PermissionRequestCard
              key={req.id}
              request={req}
              onApprove={() => openReview(req, 'APPROVE')}
              onReject={() => openReview(req, 'REJECT')}
              // Withdraw is the requester's prerogative; only the backend's
              // original requester may cancel. Surface it only in the
              // scoped "my requests" view so approvers browsing every row
              // don't see a button that would 403.
              onWithdraw={
                mineOnly ? () => cancelMutation.mutate({ id: req.id }) : undefined
              }
              withdrawPending={cancelMutation.isPending}
            />
          ))}
        </ul>
      )}

      {!listQuery.isLoading && !listQuery.isError && (hasPrev || hasNext) && (
        <nav
          className="flex items-center justify-between gap-3 rounded-lg border border-border/50 bg-bg-secondary/60 px-3 py-2 text-xs text-text-secondary"
          data-testid="permission-requests-pagination"
          aria-label="Pagination"
        >
          <span data-testid="permission-requests-page-range">
            {total > 0
              ? `Showing ${pageStart}–${pageEnd} of ${total}`
              : 'No results'}
          </span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
              disabled={!hasPrev}
              data-testid="permission-requests-prev-page"
              className="rounded-md border border-border/60 px-3 py-1.5 font-medium text-text-secondary hover:bg-bg-tertiary disabled:cursor-not-allowed disabled:opacity-50"
            >
              Previous
            </button>
            <button
              type="button"
              onClick={() => setOffset((o) => o + PAGE_SIZE)}
              disabled={!hasNext}
              data-testid="permission-requests-next-page"
              className="rounded-md border border-border/60 px-3 py-1.5 font-medium text-text-secondary hover:bg-bg-tertiary disabled:cursor-not-allowed disabled:opacity-50"
            >
              Next
            </button>
          </div>
        </nav>
      )}

      <Modal open={createOpen} onClose={closeCreate} title="Request access">
        <div className="space-y-4">
          <label
            className="flex flex-col gap-1.5 text-xs text-text-secondary"
            htmlFor="permission-request-target"
          >
            Resource RID
            <input
              id="permission-request-target"
              type="text"
              value={createTarget}
              onChange={(e) => setCreateTarget(e.target.value)}
              placeholder="ri.ontology.main.object.…"
              data-testid="permission-request-target-input"
              className="w-full rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          <label
            className="flex flex-col gap-1.5 text-xs text-text-secondary"
            htmlFor="permission-request-reason"
          >
            Reason (optional)
            <textarea
              id="permission-request-reason"
              value={createReason}
              onChange={(e) => setCreateReason(e.target.value)}
              rows={3}
              placeholder="Why do you need access?"
              data-testid="permission-request-reason-input"
              className="w-full rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
            />
          </label>
          {createError && (
            <div
              role="alert"
              className="rounded-md border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-300"
            >
              {createError}
            </div>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={closeCreate}
              className="rounded-md border border-border/60 px-3 py-1.5 text-xs text-text-secondary hover:bg-bg-tertiary"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={submitCreate}
              disabled={createMutation.isPending}
              data-testid="permission-request-submit"
              className="rounded-md bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 disabled:opacity-60"
            >
              {createMutation.isPending ? 'Submitting…' : 'Submit'}
            </button>
          </div>
        </div>
      </Modal>

      <Modal
        open={reviewTarget !== null}
        onClose={closeReview}
        title={
          reviewTarget?.decision === 'APPROVE'
            ? `Approve access to ${reviewTarget.request.targetRid}`
            : reviewTarget
              ? `Reject access to ${reviewTarget.request.targetRid}`
              : ''
        }
      >
        {reviewTarget && (
          <div className="space-y-4">
            <div className="rounded-md border border-border/40 bg-bg-primary/80 p-3 text-xs">
              <div className="text-text-secondary">
                Requested by
                <span className="ml-2 text-text-primary">
                  {reviewTarget.request.requestedBy}
                </span>
              </div>
              {reviewTarget.request.reason && (
                <div className="mt-1 text-text-secondary">
                  Reason
                  <span className="ml-2 text-text-primary">
                    {reviewTarget.request.reason}
                  </span>
                </div>
              )}
            </div>
            <label
              className="flex flex-col gap-1.5 text-xs text-text-secondary"
              htmlFor="permission-review-note"
            >
              Decision note (optional)
              <textarea
                id="permission-review-note"
                value={reviewNote}
                onChange={(e) => setReviewNote(e.target.value)}
                rows={3}
                placeholder={
                  reviewTarget.decision === 'APPROVE'
                    ? 'Approval note…'
                    : 'Why is this request being rejected?'
                }
                data-testid="permission-review-note-input"
                className="w-full rounded-md border border-border/50 bg-bg-primary px-2.5 py-2 font-mono text-xs text-text-primary outline-none focus:border-amber-500/60"
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
                onClick={submitReview}
                disabled={reviewInFlight}
                data-testid="permission-review-submit"
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

interface PermissionRequestCardProps {
  request: PermissionRequest;
  onApprove: () => void;
  onReject: () => void;
  onWithdraw?: () => void;
  withdrawPending?: boolean;
}

function PermissionRequestCard({
  request,
  onApprove,
  onReject,
  onWithdraw,
  withdrawPending,
}: PermissionRequestCardProps) {
  const isPending = request.status === 'PENDING';
  return (
    <li
      className="rounded-lg border border-border/50 bg-bg-secondary/60 p-4"
      data-testid="permission-request-card"
      data-request-id={request.id}
    >
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-1 space-y-1">
          <div className="flex items-center gap-2">
            <span
              className={`inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${STATUS_BADGE_STYLE[request.status]}`}
            >
              {request.status}
            </span>
            <h2 className="font-mono text-sm text-text-primary">
              {request.targetRid}
            </h2>
          </div>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-0.5 text-xs text-text-secondary">
            <dt>Requested by</dt>
            <dd className="text-text-primary">{request.requestedBy}</dd>
            <dt>Requested at</dt>
            <dd className="text-text-primary">
              {new Date(request.createdAt).toLocaleString()}
            </dd>
            {request.reason && (
              <>
                <dt>Reason</dt>
                <dd className="text-text-primary">{request.reason}</dd>
              </>
            )}
            {request.decidedBy && (
              <>
                <dt>Decided by</dt>
                <dd className="text-text-primary">{request.decidedBy}</dd>
              </>
            )}
            {request.decisionNote && (
              <>
                <dt>Decision note</dt>
                <dd className="text-text-primary">{request.decisionNote}</dd>
              </>
            )}
          </dl>
        </div>
        {isPending && (
          <div className="flex shrink-0 gap-2">
            {onWithdraw ? (
              <button
                type="button"
                onClick={onWithdraw}
                disabled={withdrawPending}
                data-testid="permission-request-withdraw-btn"
                className="rounded-md border border-border/60 px-3 py-1.5 text-xs font-semibold text-text-secondary hover:bg-bg-tertiary disabled:opacity-60"
              >
                {withdrawPending ? 'Withdrawing…' : 'Withdraw'}
              </button>
            ) : (
              <>
                <button
                  type="button"
                  onClick={onApprove}
                  data-testid="permission-request-approve-btn"
                  className="rounded-md bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500"
                >
                  Approve
                </button>
                <button
                  type="button"
                  onClick={onReject}
                  data-testid="permission-request-reject-btn"
                  className="rounded-md border border-rose-500/50 px-3 py-1.5 text-xs font-semibold text-rose-300 hover:bg-rose-500/10"
                >
                  Reject
                </button>
              </>
            )}
          </div>
        )}
      </div>
    </li>
  );
}
