import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listLinkTypes,
  createLinkType,
  updateLinkType,
  deleteLinkType,
  type CreateLinkTypeRequest,
  type UpdateLinkTypeRequest,
} from '../api/ontologies';

export function useLinkTypes(ontologyApiName: string) {
  return useQuery({
    queryKey: ['linkTypes', ontologyApiName, '__all__'],
    queryFn: () => listLinkTypes(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

export function useCreateLinkType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateLinkTypeRequest) =>
      createLinkType(ontologyApiName, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['linkTypes', ontologyApiName] });
    },
  });
}

export function useUpdateLinkType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateLinkTypeRequest }) =>
      updateLinkType(ontologyApiName, vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['linkTypes', ontologyApiName] });
    },
  });
}

export function useDeleteLinkType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteLinkType(ontologyApiName, rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['linkTypes', ontologyApiName] });
    },
  });
}
