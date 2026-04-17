import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listActionTypesAdmin,
  createActionType,
  updateActionType,
  deleteActionType,
  type CreateActionTypeRequest,
  type UpdateActionTypeRequest,
} from '../api/ontologies';

export function useActionTypesAdmin(ontologyApiName: string) {
  return useQuery({
    queryKey: ['actionTypesAdmin', ontologyApiName],
    queryFn: () => listActionTypesAdmin(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

export function useCreateActionType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateActionTypeRequest) =>
      createActionType(ontologyApiName, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['actionTypesAdmin', ontologyApiName] });
      qc.invalidateQueries({ queryKey: ['actionTypes', ontologyApiName] });
    },
  });
}

export function useUpdateActionType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateActionTypeRequest }) =>
      updateActionType(ontologyApiName, vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['actionTypesAdmin', ontologyApiName] });
      qc.invalidateQueries({ queryKey: ['actionTypes', ontologyApiName] });
    },
  });
}

export function useDeleteActionType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteActionType(ontologyApiName, rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['actionTypesAdmin', ontologyApiName] });
      qc.invalidateQueries({ queryKey: ['actionTypes', ontologyApiName] });
    },
  });
}
