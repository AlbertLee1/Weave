import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createComment,
  deleteComment,
  listComments,
  updateComment,
  type CreateCommentInput,
  type ListCommentsParams,
  type ListCommentsResponse,
  type UpdateCommentInput,
} from '../api/comments';

const ROOT_KEY = ['comments'] as const;

function listKey(targetRid: string) {
  return [...ROOT_KEY, 'list', targetRid] as const;
}

export function useComments(
  params: ListCommentsParams & { enabled?: boolean },
) {
  const { enabled = true, targetRid, parentId, limit, offset } = params;
  return useQuery<ListCommentsResponse>({
    queryKey: [...listKey(targetRid), { parentId: parentId ?? null, limit: limit ?? null, offset: offset ?? null }],
    queryFn: () => listComments({ targetRid, parentId, limit, offset }),
    enabled: enabled && !!targetRid,
  });
}

export function useCreateComment(targetRid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateCommentInput) => createComment(input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: listKey(targetRid) });
    },
  });
}

export function useUpdateComment(targetRid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateCommentInput) => updateComment(input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: listKey(targetRid) });
    },
  });
}

export function useDeleteComment(targetRid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteComment(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: listKey(targetRid) });
    },
  });
}
