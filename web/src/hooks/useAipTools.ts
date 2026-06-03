import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createAipTool,
  deleteAipTool,
  listAipTools,
  updateAipTool,
  type CreateAipToolRequest,
  type UpdateAipToolRequest,
} from '../api/aipTools';

export const aipToolsQueryKeys = {
  list: ['aip', 'tools'] as const,
};

export function useAipTools(enabled = true) {
  return useQuery({
    queryKey: aipToolsQueryKeys.list,
    queryFn: () => listAipTools(),
    enabled,
  });
}

export function useCreateAipTool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateAipToolRequest) => createAipTool(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipToolsQueryKeys.list });
    },
  });
}

export function useUpdateAipTool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: { name: string; body: UpdateAipToolRequest }) =>
      updateAipTool(vars.name, vars.body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipToolsQueryKeys.list });
    },
  });
}

export function useDeleteAipTool() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => deleteAipTool(name),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: aipToolsQueryKeys.list });
    },
  });
}
