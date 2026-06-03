import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listProperties,
  listSharedPropertyTypes,
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

// useSharedPropertyTypes lists the ontology's reusable SharedPropertyType
// definitions, used by the Add-property form to offer an optional SPT
// binding. Failures degrade gracefully (the selector hides) — no retry so
// a missing endpoint surfaces immediately rather than spinning.
export function useSharedPropertyTypes(ontologyApiName: string) {
  return useQuery({
    queryKey: ['sharedPropertyTypes', ontologyApiName],
    queryFn: () => listSharedPropertyTypes(ontologyApiName),
    enabled: !!ontologyApiName,
    retry: false,
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
