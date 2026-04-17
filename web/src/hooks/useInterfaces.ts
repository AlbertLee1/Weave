import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listInterfacesAdmin,
  createInterface,
  updateInterface,
  deleteInterface,
  listObjectTypeInterfaces,
  attachInterfaceToObjectType,
  detachInterfaceFromObjectType,
  type CreateInterfaceRequest,
  type UpdateInterfaceRequest,
} from '../api/ontologies';

export function useInterfacesAdmin(ontologyApiName: string) {
  return useQuery({
    queryKey: ['interfacesAdmin', ontologyApiName],
    queryFn: () => listInterfacesAdmin(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

export function useCreateInterface(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateInterfaceRequest) =>
      createInterface(ontologyApiName, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['interfacesAdmin', ontologyApiName] });
    },
  });
}

export function useUpdateInterface(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateInterfaceRequest }) =>
      updateInterface(ontologyApiName, vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['interfacesAdmin', ontologyApiName] });
    },
  });
}

export function useDeleteInterface(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteInterface(ontologyApiName, rid),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['interfacesAdmin', ontologyApiName] });
    },
  });
}

export function useObjectTypeInterfaces(
  ontologyApiName: string,
  objectTypeRid: string | undefined,
) {
  return useQuery({
    queryKey: ['objectTypeInterfaces', ontologyApiName, objectTypeRid],
    queryFn: () => listObjectTypeInterfaces(ontologyApiName, objectTypeRid ?? ''),
    enabled: !!ontologyApiName && !!objectTypeRid,
  });
}

export function useAttachInterface(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: {
      objectTypeRid: string;
      interfaceRid: string;
      propertyMapping?: Record<string, string>;
    }) =>
      attachInterfaceToObjectType(ontologyApiName, vars.objectTypeRid, {
        interfaceRid: vars.interfaceRid,
        propertyMapping: vars.propertyMapping,
      }),
    onSuccess: (_data, vars) => {
      qc.invalidateQueries({
        queryKey: ['objectTypeInterfaces', ontologyApiName, vars.objectTypeRid],
      });
      qc.invalidateQueries({ queryKey: ['interfacesAdmin', ontologyApiName] });
    },
  });
}

export function useDetachInterface(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { objectTypeRid: string; interfaceRid: string }) =>
      detachInterfaceFromObjectType(
        ontologyApiName,
        vars.objectTypeRid,
        vars.interfaceRid,
      ),
    onSuccess: (_data, vars) => {
      qc.invalidateQueries({
        queryKey: ['objectTypeInterfaces', ontologyApiName, vars.objectTypeRid],
      });
      qc.invalidateQueries({ queryKey: ['interfacesAdmin', ontologyApiName] });
    },
  });
}
