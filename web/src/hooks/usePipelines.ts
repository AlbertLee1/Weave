import { useQuery } from '@tanstack/react-query';
import { getPipeline, listPipelineRuns, listPipelines } from '../api/pipelines';

export const pipelineQueryKeys = {
  pipelines: ['pipelines'] as const,
  pipeline: (id: string) => ['pipeline', id] as const,
  pipelineRuns: (id: string, limit: number) =>
    ['pipeline', id, 'runs', limit] as const,
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

export function usePipelineRuns(pipelineId: string | null, limit = 10) {
  return useQuery({
    queryKey: pipelineId
      ? pipelineQueryKeys.pipelineRuns(pipelineId, limit)
      : ['pipeline', '__none__', 'runs', limit],
    queryFn: () => listPipelineRuns(pipelineId as string, { limit }),
    enabled: !!pipelineId,
  });
}
