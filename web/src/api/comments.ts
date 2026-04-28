import { request } from './client';

// Comment mirrors pkg/comments.Comment. Soft-deleted rows return Body as
// the empty string with deletedAt populated; the SPA renders a tombstone
// in place so reply chains keep their parent reference.
export interface Comment {
  id: string;
  targetRid: string;
  body: string;
  author: string;
  parentId?: string;
  createdAt: string;
  updatedAt: string;
  deletedAt?: string;
}

export interface ListCommentsResponse {
  comments: Comment[];
  total: number;
  limit: number;
  offset: number;
}

export interface ListCommentsParams {
  targetRid: string;
  parentId?: string;
  limit?: number;
  offset?: number;
}

export function listComments(params: ListCommentsParams): Promise<ListCommentsResponse> {
  const qs = new URLSearchParams({ targetRid: params.targetRid });
  if (params.parentId !== undefined) qs.set('parentId', params.parentId);
  if (params.limit !== undefined) qs.set('limit', String(params.limit));
  if (params.offset !== undefined) qs.set('offset', String(params.offset));
  return request<ListCommentsResponse>('GET', `/api/v2/comments?${qs.toString()}`);
}

export interface CreateCommentInput {
  targetRid: string;
  body: string;
  parentId?: string;
}

export function createComment(input: CreateCommentInput): Promise<Comment> {
  return request<Comment>('POST', '/api/v2/comments', input);
}

export interface UpdateCommentInput {
  id: string;
  body: string;
}

export function updateComment(input: UpdateCommentInput): Promise<Comment> {
  return request<Comment>('PUT', `/api/v2/comments/${encodeURIComponent(input.id)}`, {
    body: input.body,
  });
}

export function deleteComment(id: string): Promise<void> {
  return request<void>('DELETE', `/api/v2/comments/${encodeURIComponent(id)}`);
}
