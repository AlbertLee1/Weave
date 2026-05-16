import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createInterfaceMethod,
  deleteInterfaceMethod,
  invokeInterfaceMethod,
  listInterfaceMethods,
  updateInterfaceMethod,
  type CreateInterfaceMethodRequest,
  type InterfaceMethod,
  type InvokeInterfaceMethodRequest,
  type InvokeInterfaceMethodResponse,
  type UpdateInterfaceMethodRequest,
} from '../api/interfaceMethods';

// US-047 (PC-A04): React Query hooks for the Interface Methods Console.
// US-498: extended with admin CRUD hooks (create/update/delete) used by the
// Interface admin "Methods" tab.
//
// Cache keying note (mirrors the US-046 cross-page prefix pattern): the
// queryKey prefix `interface-methods` is owned by the console page;
// other surfaces that may later consume the same endpoint should pick a
// page-scoped prefix so an invoke mutation on the console does not
// cascade-refetch unrelated views.

export function useInterfaceMethods(
  ontologyApiName: string,
  interfaceRid: string | null,
) {
  return useQuery({
    queryKey: ['interface-methods', ontologyApiName, interfaceRid],
    queryFn: () => listInterfaceMethods(ontologyApiName, interfaceRid ?? ''),
    enabled: !!ontologyApiName && !!interfaceRid,
  });
}

export function useInvokeInterfaceMethod(ontologyApiName: string) {
  const qc = useQueryClient();
  return useMutation<
    InvokeInterfaceMethodResponse,
    Error,
    { methodRid: string; body: InvokeInterfaceMethodRequest }
  >({
    mutationFn: (vars) =>
      invokeInterfaceMethod(ontologyApiName, vars.methodRid, vars.body),
    onSuccess: () => {
      // Invoke dispatches through an ActionType which writes an
      // action_logs row — invalidate the action history list so the
      // user-visible audit trail picks the new entry up immediately.
      qc.invalidateQueries({
        queryKey: ['actionHistory', ontologyApiName],
      });
    },
  });
}

export function useCreateInterfaceMethod(
  ontologyApiName: string,
  interfaceRid: string,
) {
  const qc = useQueryClient();
  return useMutation<InterfaceMethod, Error, CreateInterfaceMethodRequest>({
    mutationFn: (body) =>
      createInterfaceMethod(ontologyApiName, interfaceRid, body),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['interface-methods', ontologyApiName, interfaceRid],
      });
    },
  });
}

export function useUpdateInterfaceMethod(
  ontologyApiName: string,
  interfaceRid: string,
) {
  const qc = useQueryClient();
  return useMutation<
    InterfaceMethod,
    Error,
    { methodRid: string; body: UpdateInterfaceMethodRequest }
  >({
    mutationFn: (vars) =>
      updateInterfaceMethod(ontologyApiName, vars.methodRid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['interface-methods', ontologyApiName, interfaceRid],
      });
    },
  });
}

export function useDeleteInterfaceMethod(
  ontologyApiName: string,
  interfaceRid: string,
) {
  const qc = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: (methodRid) =>
      deleteInterfaceMethod(ontologyApiName, methodRid),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['interface-methods', ontologyApiName, interfaceRid],
      });
    },
  });
}
