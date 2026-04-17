import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listObjectTypes,
  getObjectType,
  listOutgoingLinkTypes,
  createObjectType,
  updateObjectType,
  deleteObjectType,
  type CreateObjectTypeRequest,
  type UpdateObjectTypeRequest,
} from '../api/ontologies';

export function useObjectTypes(ontologyApiName: string) {
  return useQuery({
    queryKey: ['objectTypes', ontologyApiName],
    queryFn: () => listObjectTypes(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

export function useObjectType(
  ontologyApiName: string,
  objectTypeApiName: string,
) {
  return useQuery({
    queryKey: ['objectTypes', ontologyApiName, objectTypeApiName],
    queryFn: () => getObjectType(ontologyApiName, objectTypeApiName),
    enabled: !!ontologyApiName && !!objectTypeApiName,
  });
}

export function useOutgoingLinkTypes(
  ontologyApiName: string,
  objectTypeApiName: string,
) {
  return useQuery({
    queryKey: ['linkTypes', ontologyApiName, objectTypeApiName],
    queryFn: () => listOutgoingLinkTypes(ontologyApiName, objectTypeApiName),
    enabled: !!ontologyApiName && !!objectTypeApiName,
  });
}

export function useCreateObjectType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateObjectTypeRequest) =>
      createObjectType(ontologyApiName, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] });
    },
  });
}

export function useUpdateObjectType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateObjectTypeRequest }) =>
      updateObjectType(ontologyApiName, vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] });
    },
  });
}

export function useDeleteObjectType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteObjectType(ontologyApiName, rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] });
    },
  });
}
