import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  grantMarking,
  listGrantsByMarking,
  listGrantsByUser,
  listMarkings,
  revokeMarking,
} from '../api/markings';

export function useMarkings() {
  return useQuery({
    queryKey: ['markings'],
    queryFn: listMarkings,
  });
}

export function useGrantsByMarking(name: string | null) {
  return useQuery({
    queryKey: ['markings', 'grantsByMarking', name],
    queryFn: () => listGrantsByMarking(name ?? ''),
    enabled: !!name,
  });
}

export function useGrantsByUser(userId: string | null) {
  return useQuery({
    queryKey: ['markings', 'grantsByUser', userId],
    queryFn: () => listGrantsByUser(userId ?? ''),
    enabled: !!userId,
  });
}

export function useGrantMarking() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, marking }: { userId: string; marking: string }) =>
      grantMarking(userId, marking),
    onSuccess: (_, variables) => {
      qc.invalidateQueries({ queryKey: ['markings', 'grantsByMarking'] });
      qc.invalidateQueries({
        queryKey: ['markings', 'grantsByUser', variables.userId],
      });
    },
  });
}

export function useRevokeMarking() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, marking }: { userId: string; marking: string }) =>
      revokeMarking(userId, marking),
    onSuccess: (_, variables) => {
      qc.invalidateQueries({ queryKey: ['markings', 'grantsByMarking'] });
      qc.invalidateQueries({
        queryKey: ['markings', 'grantsByUser', variables.userId],
      });
    },
  });
}
