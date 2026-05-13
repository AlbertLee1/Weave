import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listValueTypesAdmin,
  createValueType,
  updateValueType,
  deleteValueType,
  listValueTypeUsages,
  type CreateValueTypeRequest,
  type UpdateValueTypeRequest,
} from '../api/ontologies';

export function useValueTypesAdmin(ontologyApiName: string) {
  return useQuery({
    queryKey: ['valueTypesAdmin', ontologyApiName],
    queryFn: () => listValueTypesAdmin(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

export function useCreateValueType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateValueTypeRequest) =>
      createValueType(ontologyApiName, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['valueTypesAdmin', ontologyApiName] });
    },
  });
}

export function useUpdateValueType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateValueTypeRequest }) =>
      updateValueType(ontologyApiName, vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['valueTypesAdmin', ontologyApiName] });
    },
  });
}

export function useDeleteValueType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteValueType(ontologyApiName, rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['valueTypesAdmin', ontologyApiName] });
    },
  });
}

export function useValueTypeUsages(
  ontologyApiName: string,
  valueTypeRid: string | undefined,
) {
  return useQuery({
    queryKey: ['valueTypeUsages', ontologyApiName, valueTypeRid],
    queryFn: () => listValueTypeUsages(ontologyApiName, valueTypeRid ?? ''),
    enabled: !!ontologyApiName && !!valueTypeRid,
  });
}
