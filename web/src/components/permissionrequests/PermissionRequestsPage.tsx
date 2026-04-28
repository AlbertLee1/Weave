import { useMemo, useState } from 'react';
import { ApiRequestError } from '../../api/client';
import type {
  PermissionRequest,
  PermissionRequestStatus,
} from '../../api/permissionRequests';
import {
  useApprovePermissionRequest,
  useCreatePermissionRequest,
  usePermissionRequests,
  useRejectPermissionRequest,
} from '../../hooks/usePermissionRequests';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Modal } from '../common/Modal';

type StatusFilter = PermissionRequestStatus | 'ALL';

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'PENDING', label: 'Pending' },
  { value: 'APPROVED', label: 'Approved' },
  { value: 'REJECTED', label: 'Rejected' },
  { value: 'ALL', label: 'All' },
];

const STATUS_BADGE_STYLE: Record<PermissionRequestStatus, string> = {
  PENDING: 'bg-amber-500/10 text-amber-400 border border-amber-500/30',
  APPROVED: 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30',
  REJECTED: 'bg-rose-500/10 text-rose-400 border border-rose-500/30',
};

// PermissionRequestsPage renders the share-link permission inbox
// (US-339). Approvers see every PENDING request; non-approvers see only
// their own. Both can submit a new request via the "Request access"
// button, which opens a small dialog accepting a target RID + reason.
// Approvers also see Approve / Reject buttons inline on each PENDING row.
export function PermissionRequestsPage() {
  const [status, setStatus] = useState<StatusFilter>('PENDING');
  const [mineOnly, setMineOnly] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [createTarget, setCreateTarget] = useState('');
  const [createReason, setCreateReason] = useState('');
  const [createError, setCreateError] = useState<string | null>(null);
  const [reviewTarget, setReviewTarget] = useState<
    { request: PermissionRequest; decision: 'APPROVE' | 'REJECT' } | null
  >(null);
  const [reviewNote, setReviewNote] = useState('');
  const [reviewError, setReviewError] = useState<string | null>(null);

  const listQuery = usePermissionRequests({
    status: status === 'ALL' ? undefined : status,
    mine: mineOnly || undefined,
  });
  const createMutation = useCreatePermissionRequest();
  const approveMutation = useApprovePermissionRequest();
  const rejectMutation = useRejectPermissionRequest();

  const requests = useMemo(
    () => listQuery.data?.requests ?? [],
    [listQuery.data?.requests],
  );
  const available = listQuery.data?.available ?? true;

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
              onClick={() => setStatus(opt.value)}
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
            checked={mineOnly}
            onChange={(e) => setMineOnly(e.target.checked)}
            data-testid="permission-requests-mine-toggle"
            className="h-3.5 w-3.5 rounded border-border/60 bg-bg-primary text-amber-500"
          />
          Only my requests
        </label>
      </section>

      {listQuery.isLoading ? (
        <div className="flex items-center justify-center py-20">
          <LoadingSpinner />
        </div>
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
            />
          ))}
        </ul>
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
}

function PermissionRequestCard({
  request,
  onApprove,
  onReject,
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
          </div>
        )}
      </div>
    </li>
  );
}
