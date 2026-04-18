import { request } from './client';

// ActionApproval mirrors pkg/actions/approval.go. Parameters is the caller's
// snapshotted apply body — opaque JSON so the UI can render it as-is.
export interface ActionApproval {
  id: string;
  actionTypeRid?: string;
  ontologyApiName: string;
  actionType: string;
  parameters?: unknown;
  approvers?: string[];
  status: 'PENDING' | 'APPROVED' | 'REJECTED';
  requestedBy?: string;
  reviewedBy?: string;
  reason?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ListApprovalsResponse {
  data: ActionApproval[];
}

export interface ListApprovalsParams {
  status?: 'PENDING' | 'APPROVED' | 'REJECTED' | 'ALL';
  mine?: boolean;
  limit?: number;
}

export function listApprovals(
  ontologyApiName: string,
  params: ListApprovalsParams = {},
): Promise<ListApprovalsResponse> {
  const query = new URLSearchParams();
  if (params.status) query.set('status', params.status);
  if (params.mine !== undefined) query.set('mine', params.mine ? 'true' : 'false');
  if (params.limit !== undefined) query.set('limit', String(params.limit));
  const qs = query.toString();
  return request<ListApprovalsResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/approvals${qs ? `?${qs}` : ''}`,
  );
}

export interface ApprovalReviewResponse {
  approvalId: string;
  status: 'APPROVED' | 'REJECTED';
}

export function approveAction(
  ontologyApiName: string,
  approvalId: string,
  reason?: string,
): Promise<ApprovalReviewResponse> {
  return request<ApprovalReviewResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/approvals/${encodeURIComponent(approvalId)}/approve`,
    reason ? { reason } : {},
  );
}

export function rejectAction(
  ontologyApiName: string,
  approvalId: string,
  reason?: string,
): Promise<ApprovalReviewResponse> {
  return request<ApprovalReviewResponse>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actions/approvals/${encodeURIComponent(approvalId)}/reject`,
    reason ? { reason } : {},
  );
}
