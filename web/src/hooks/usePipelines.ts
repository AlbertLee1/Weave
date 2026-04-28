import { useQuery } from '@tanstack/react-query';
import { getPipeline, listPipelines } from '../api/pipelines';

export const pipelineQueryKeys = {
  pipelines: ['pipelines'] as const,
  pipeline: (id: string) => ['pipeline', id] as const,
};

export function usePipelines(enabled = true) {
  return useQuery({
    queryKey: pipelineQueryKeys.pipelines,
    queryFn: () => listPipelines(),
    enabled,
  });
}

export function usePipeline(pipelineId: string | null) {
  return useQuery({
    queryKey: pipelineId
      ? pipelineQueryKeys.pipeline(pipelineId)
      : ['pipeline', '__none__'],
    queryFn: () => getPipeline(pipelineId as string),
    enabled: !!pipelineId,
  });
}
