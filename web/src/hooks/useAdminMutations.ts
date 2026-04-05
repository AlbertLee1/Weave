import { useMutation, useQueryClient } from '@tanstack/react-query';
import {
  updateOntology,
  updateObjectType,
  deleteObjectType,
  createProperty,
  updateProperty,
  deleteProperty,
  updateLinkType,
  deleteLinkType,
  updateActionType,
  deleteActionType,
  type UpdateOntologyInput,
  type UpdateObjectTypeInput,
  type CreatePropertyInput,
  type UpdatePropertyInput,
  type UpdateLinkTypeInput,
  type CreateActionTypeInput,
} from '../api/admin';

const onError = (err: Error) => { console.error('Mutation failed:', err.message); };

export function useUpdateOntology(ontologyRid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateOntologyInput) => updateOntology(ontologyRid, input),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['ontologies'] }); },
    onError,
  });
}

export function useUpdateObjectType(objectTypeRid: string, ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateObjectTypeInput) => updateObjectType(objectTypeRid, input),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] }); },
    onError,
  });
}

export function useDeleteObjectType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteObjectType(rid),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] }); },
    onError,
  });
}

export function useCreateProperty(objectTypeRid: string, ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreatePropertyInput) => createProperty(objectTypeRid, input),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] }); },
    onError,
  });
}

export function useUpdateProperty(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ rid, input }: { rid: string; input: UpdatePropertyInput }) => updateProperty(rid, input),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] }); },
    onError,
  });
}

export function useDeleteProperty(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteProperty(rid),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['objectTypes', ontologyApiName] }); },
    onError,
  });
}

export function useUpdateLinkType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ rid, input }: { rid: string; input: UpdateLinkTypeInput }) => updateLinkType(rid, input),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['linkTypes', ontologyApiName] }); },
    onError,
  });
}

export function useDeleteLinkType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteLinkType(rid),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['linkTypes', ontologyApiName] }); },
    onError,
  });
}

export function useUpdateActionType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ rid, input }: { rid: string; input: Partial<CreateActionTypeInput> }) => updateActionType(rid, input),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['actionTypes', ontologyApiName] }); },
    onError,
  });
}

export function useDeleteActionType(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteActionType(rid),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['actionTypes', ontologyApiName] }); },
    onError,
  });
}
