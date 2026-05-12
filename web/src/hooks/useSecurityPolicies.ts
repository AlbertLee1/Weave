import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createCellMask,
  createColumnMask,
  createRowPolicy,
  deleteCellMask,
  deleteColumnMask,
  deleteRowPolicy,
  listCellMasks,
  listColumnMasks,
  listRowPolicies,
  updateCellMask,
  updateColumnMask,
  updateRowPolicy,
  type CreateCellMaskRequest,
  type CreateColumnMaskRequest,
  type CreateRowPolicyRequest,
  type UpdateCellMaskRequest,
  type UpdateColumnMaskRequest,
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

// US-042 (PC-A07b): column-mask hooks. Same query-key shape as
// row-policies so the cache invalidation prefix-matches across both
// the unfiltered list and any per-objectType filtered list.
const COLUMN_MASKS_KEY = ['columnMasks'] as const;

export function useColumnMasks(params: { objectTypeRid?: string } = {}) {
  return useQuery({
    queryKey: [...COLUMN_MASKS_KEY, params.objectTypeRid ?? '__all__'],
    queryFn: () => listColumnMasks(params),
  });
}

export function useCreateColumnMask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateColumnMaskRequest) => createColumnMask(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: COLUMN_MASKS_KEY });
    },
  });
}

export function useUpdateColumnMask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateColumnMaskRequest }) =>
      updateColumnMask(vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: COLUMN_MASKS_KEY });
    },
  });
}

export function useDeleteColumnMask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteColumnMask(rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: COLUMN_MASKS_KEY });
    },
  });
}

// US-043 (PC-A07c): cell-mask hooks. Same query-key shape as
// column-masks so the cache invalidation prefix-matches across both
// the unfiltered list and any per-objectType filtered list.
const CELL_MASKS_KEY = ['cellMasks'] as const;

export function useCellMasks(params: { objectTypeRid?: string } = {}) {
  return useQuery({
    queryKey: [...CELL_MASKS_KEY, params.objectTypeRid ?? '__all__'],
    queryFn: () => listCellMasks(params),
  });
}

export function useCreateCellMask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateCellMaskRequest) => createCellMask(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: CELL_MASKS_KEY });
    },
  });
}

export function useUpdateCellMask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateCellMaskRequest }) =>
      updateCellMask(vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: CELL_MASKS_KEY });
    },
  });
}

export function useDeleteCellMask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteCellMask(rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: CELL_MASKS_KEY });
    },
  });
}
