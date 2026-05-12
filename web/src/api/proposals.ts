import { request } from './client';

// OntologyProposal mirrors pkg/oms.OntologyProposal (models.go:686). The
// merge UI surfaces the proposal lifecycle: pending → approved/rejected,
// then approved → merged via the MergeProposal endpoint.
export interface OntologyProposal {
  id: string;
  branchId: string;
  ontologyRid: string;
  title: string;
  description?: string;
  // status drives the page filter chips + which action buttons surface.
  // Server emits "pending" | "approved" | "rejected" | "merged".
  status: 'pending' | 'approved' | 'rejected' | 'merged';
  author: string;
  createdAt: string;
  updatedAt: string;
}

// ProposalReview mirrors pkg/oms.ProposalReview (models.go:699).
export interface ProposalReview {
  id: string;
  proposalId: string;
  reviewer: string;
  decision: 'approve' | 'reject';
  reason?: string;
  createdAt: string;
}

// ProposalDetailResponse extends OntologyProposal with the embedded
// reviews array (pkg/oms.ProposalDetailResponse).
export interface ProposalDetail extends OntologyProposal {
  reviews: ProposalReview[];
}

export interface ListProposalsResponse {
  data: OntologyProposal[];
}

export interface ListProposalsParams {
  status?: OntologyProposal['status'];
}

export function listProposals(
  ontology: string,
  params: ListProposalsParams = {},
): Promise<ListProposalsResponse> {
  const qs = new URLSearchParams();
  if (params.status) qs.set('status', params.status);
  const search = qs.toString();
  return request<ListProposalsResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/proposals${search ? `?${search}` : ''}`,
  );
}

export function getProposal(
  ontology: string,
  proposalId: string,
): Promise<ProposalDetail> {
  return request<ProposalDetail>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/proposals/${encodeURIComponent(proposalId)}`,
  );
}

export interface CreateProposalRequest {
  branchId: string;
  title: string;
  description?: string;
  author?: string;
}

export function createProposal(
  ontology: string,
  body: CreateProposalRequest,
): Promise<OntologyProposal> {
  return request<OntologyProposal>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/proposals`,
    body,
  );
}

export interface ReviewRequest {
  reviewer: string;
  reason?: string;
}

export function approveProposal(
  ontology: string,
  proposalId: string,
  body: ReviewRequest,
): Promise<OntologyProposal> {
  return request<OntologyProposal>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/proposals/${encodeURIComponent(proposalId)}/approve`,
    body,
  );
}

export function rejectProposal(
  ontology: string,
  proposalId: string,
  body: ReviewRequest,
): Promise<OntologyProposal> {
  return request<OntologyProposal>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/proposals/${encodeURIComponent(proposalId)}/reject`,
    body,
  );
}

export function mergeProposal(
  ontology: string,
  proposalId: string,
): Promise<OntologyProposal> {
  return request<OntologyProposal>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/proposals/${encodeURIComponent(proposalId)}/merge`,
    {},
  );
}

// BranchDiffEntry mirrors pkg/oms.BranchDiffEntry (handlers_branch.go:304).
// The Diff view groups entries by ChangeType (ADDED / MODIFIED / DELETED)
// and colors them — entityType is sub-grouped underneath for navigability.
export interface BranchDiffEntry {
  entityType: string;
  entityRid: string;
  changeType: 'ADDED' | 'MODIFIED' | 'DELETED';
  before: unknown;
  after: unknown;
}

export interface ListBranchDiffResponse {
  data: BranchDiffEntry[];
}

export function getBranchDiff(
  ontology: string,
  branchId: string,
): Promise<ListBranchDiffResponse> {
  return request<ListBranchDiffResponse>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/branches/${encodeURIComponent(branchId)}/diff`,
  );
}

// BreakingChange mirrors pkg/oms.BreakingChange (breaking_changes.go:22).
// The proposal detail page reads /breaking-changes to surface a warning
// banner when the branch overlay introduces risky schema changes.
export interface BreakingChange {
  kind:
    | 'PROPERTY_DELETED'
    | 'PROPERTY_TYPE_NARROWED'
    | 'PROPERTY_REQUIRED_ADDED'
    | 'PRIMARY_KEY_CHANGED'
    | string;
  objectTypeRid?: string;
  objectTypeApiName?: string;
  propertyApiName?: string;
  detail?: string;
  affectedActionTypes?: string[];
  affectedSavedObjectSets?: string[];
}

export interface BreakingChangesReport {
  branchId: string;
  changes: BreakingChange[];
}

export function getBranchBreakingChanges(
  ontology: string,
  branchId: string,
): Promise<BreakingChangesReport> {
  return request<BreakingChangesReport>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontology)}/branches/${encodeURIComponent(branchId)}/breaking-changes`,
  );
}
