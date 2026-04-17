import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listProperties,
  createProperty,
  updateProperty,
  deleteProperty,
  type CreatePropertyRequest,
  type UpdatePropertyRequest,
} from '../api/ontologies';

export function useProperties(
  ontologyApiName: string,
  objectTypeRid: string,
) {
  return useQuery({
    queryKey: ['properties', ontologyApiName, objectTypeRid],
    queryFn: () => listProperties(ontologyApiName, objectTypeRid),
    enabled: !!ontologyApiName && !!objectTypeRid,
  });
}

export function useCreateProperty(
  ontologyApiName: string,
  objectTypeRid: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreatePropertyRequest) =>
      createProperty(ontologyApiName, objectTypeRid, body),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['properties', ontologyApiName, objectTypeRid],
      });
      qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] });
    },
  });
}

export function useUpdateProperty(
  ontologyApiName: string,
  objectTypeRid: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdatePropertyRequest }) =>
      updateProperty(ontologyApiName, vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['properties', ontologyApiName, objectTypeRid],
      });
      qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] });
    },
  });
}

export function useDeleteProperty(
  ontologyApiName: string,
  objectTypeRid: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteProperty(ontologyApiName, rid),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['properties', ontologyApiName, objectTypeRid],
      });
      qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] });
    },
  });
}
