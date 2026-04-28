import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createActionTemplate,
  deleteActionTemplate,
  listActionTemplates,
  updateActionTemplate,
  type ActionTemplate,
  type CreateActionTemplateInput,
  type ListActionTemplatesParams,
  type UpdateActionTemplateInput,
} from '../api/actionTemplates';
import { ApiRequestError } from '../api/client';

const STABLE_DISABLED_KEY = ['action-templates', '__none__'] as const;

export function useActionTemplates(
  params: Partial<ListActionTemplatesParams> & { enabled?: boolean } = {},
) {
  const { ontology, actionType, enabled = true } = params;
  const ready = !!ontology && !!actionType;
  return useQuery<ActionTemplate[]>({
    queryKey: ready
      ? ['action-templates', ontology, actionType]
      : (STABLE_DISABLED_KEY as unknown as string[]),
    queryFn: async () => {
      try {
        const resp = await listActionTemplates({
          ontology: ontology as string,
          actionType: actionType as string,
        });
        return resp.actionTemplates ?? [];
      } catch (e) {
        // Degraded-mode deployments leave the route unmounted; surface
        // an empty list so the panel hides itself instead of erroring.
        if (
          e instanceof ApiRequestError &&
          (e.statusCode === 404 || e.errorName === 'ActionTemplatesUnavailable')
        ) {
          return [];
        }
        throw e;
      }
    },
    enabled: ready && enabled,
  });
}

export function useCreateActionTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateActionTemplateInput) => createActionTemplate(input),
    onSuccess: (saved) => {
      qc.invalidateQueries({
        queryKey: ['action-templates', saved.ontology, saved.actionType],
      });
    },
  });
}

export function useUpdateActionTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateActionTemplateInput) => updateActionTemplate(input),
    onSuccess: (saved) => {
      qc.invalidateQueries({
        queryKey: ['action-templates', saved.ontology, saved.actionType],
      });
    },
  });
}

export function useDeleteActionTemplate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteActionTemplate(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['action-templates'] });
    },
  });
}
