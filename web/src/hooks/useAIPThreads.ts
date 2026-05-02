import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createThread,
  deleteThread,
  getThreadTree,
  listMessages,
  listThreads,
  sendMessage,
  updateThread,
  type CreateThreadRequest,
  type SendMessageRequest,
  type UpdateThreadRequest,
} from '../api/aip';

export const aipQueryKeys = {
  threads: ['aip', 'threads'] as const,
  messages: (threadId: string) => ['aip', 'messages', threadId] as const,
  tree: (threadId: string) => ['aip', 'tree', threadId] as const,
};

export function useAIPThreads(enabled = true) {
  return useQuery({
    queryKey: aipQueryKeys.threads,
    queryFn: () => listThreads(),
    enabled,
  });
}

export function useAIPMessages(threadId: string | null) {
  return useQuery({
    queryKey: threadId ? aipQueryKeys.messages(threadId) : ['aip', 'messages', '__none__'],
    queryFn: () => listMessages(threadId as string),
    enabled: !!threadId,
  });
}

export function useAIPThreadTree(threadId: string | null) {
  return useQuery({
    queryKey: threadId ? aipQueryKeys.tree(threadId) : ['aip', 'tree', '__none__'],
    queryFn: () => getThreadTree(threadId as string),
    enabled: !!threadId,
  });
}

export function useCreateAIPThread() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateThreadRequest) => createThread(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipQueryKeys.threads });
    },
  });
}

export function useUpdateAIPThread() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { threadId: string; body: UpdateThreadRequest }) =>
      updateThread(vars.threadId, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipQueryKeys.threads });
    },
  });
}

export function useDeleteAIPThread() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (threadId: string) => deleteThread(threadId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipQueryKeys.threads });
    },
  });
}

export function useSendAIPMessage(threadId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SendMessageRequest) => sendMessage(threadId, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipQueryKeys.messages(threadId) });
      qc.invalidateQueries({ queryKey: aipQueryKeys.tree(threadId) });
      qc.invalidateQueries({ queryKey: aipQueryKeys.threads });
    },
  });
}
