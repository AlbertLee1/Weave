import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createRowPolicy,
  deleteRowPolicy,
  listRowPolicies,
  updateRowPolicy,
  type CreateRowPolicyRequest,
  type UpdateRowPolicyRequest,
} from '../api/securityPolicies';

// Row-policies are scoped admin-wide (no ?ontology= filter — the listing is
// indexed by ObjectType RID), so the query key only carries the optional
// objectType filter. Mutation invalidation matches both the unfiltered
// list and any per-objectType filtered list at the prefix level.
const ROW_POLICIES_KEY = ['rowPolicies'] as const;

export function useRowPolicies(params: { objectTypeRid?: string } = {}) {
  return useQuery({
    queryKey: [...ROW_POLICIES_KEY, params.objectTypeRid ?? '__all__'],
    queryFn: () => listRowPolicies(params),
  });
}

export function useCreateRowPolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateRowPolicyRequest) => createRowPolicy(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ROW_POLICIES_KEY });
    },
  });
}

export function useUpdateRowPolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateRowPolicyRequest }) =>
      updateRowPolicy(vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ROW_POLICIES_KEY });
    },
  });
}

export function useDeleteRowPolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteRowPolicy(rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ROW_POLICIES_KEY });
    },
  });
}
