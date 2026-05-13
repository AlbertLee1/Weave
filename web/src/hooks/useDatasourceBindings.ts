import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listDatasourceBindings,
  createDatasourceBinding,
  updateDatasourceBinding,
  deleteDatasourceBinding,
  type CreateDatasourceBindingRequest,
  type UpdateDatasourceBindingRequest,
} from '../api/ontologies';

// queryKey prefix is scoped by (ontology, objectTypeRid) so a binding edit
// for ObjectType A doesn't trigger a refetch on ObjectType B's bindings.
export function useDatasourceBindings(
  ontologyApiName: string,
  objectTypeRid: string | undefined,
) {
  return useQuery({
    queryKey: ['datasourceBindings', ontologyApiName, objectTypeRid],
    queryFn: () =>
      listDatasourceBindings(ontologyApiName, objectTypeRid ?? ''),
    enabled: !!ontologyApiName && !!objectTypeRid,
  });
}

export function useCreateDatasourceBinding(
  ontologyApiName: string,
  objectTypeRid: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateDatasourceBindingRequest) =>
      createDatasourceBinding(ontologyApiName, objectTypeRid, body),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['datasourceBindings', ontologyApiName, objectTypeRid],
      });
    },
  });
}

export function useUpdateDatasourceBinding(
  ontologyApiName: string,
  objectTypeRid: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { rid: string; body: UpdateDatasourceBindingRequest }) =>
      updateDatasourceBinding(ontologyApiName, vars.rid, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['datasourceBindings', ontologyApiName, objectTypeRid],
      });
    },
  });
}

export function useDeleteDatasourceBinding(
  ontologyApiName: string,
  objectTypeRid: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rid: string) => deleteDatasourceBinding(ontologyApiName, rid),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ['datasourceBindings', ontologyApiName, objectTypeRid],
      });
    },
  });
}
