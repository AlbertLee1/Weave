import { request } from './client';

// PipelineInput mirrors pkg/pipeline.Input.
export interface PipelineInput {
  name: string;
  type: string;
  config?: Record<string, unknown>;
}

// PipelineTransform mirrors pkg/pipeline.Transform.
export interface PipelineTransform {
  name: string;
  type: string;
  inputs?: string[];
  config?: Record<string, unknown>;
}

// PipelineOutput mirrors pkg/pipeline.Output.
export interface PipelineOutput {
  name: string;
  type: string;
  input?: string;
  config?: Record<string, unknown>;
}

// Pipeline mirrors pkg/pipeline.Pipeline on the wire.
export interface Pipeline {
  id: string;
  name: string;
  description?: string;
  inputs: PipelineInput[];
  transforms: PipelineTransform[];
  outputs: PipelineOutput[];
  schedule?: string;
  enabled: boolean;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}

// PipelineRun mirrors pkg/pipeline.PipelineRun on the wire.
export interface PipelineRun {
  id: number;
  pipelineId: string;
  status: string;
  startedAt: string;
  finishedAt?: string;
  errorMessage?: string;
  result?: Record<string, unknown>;
  triggeredBy?: string;
  lastCommittedOffset?: number;
  createdAt: string;
}

export interface ListPipelinesResponse {
  pipelines: Pipeline[];
}

export interface ListPipelineRunsResponse {
  runs: PipelineRun[];
  nextCursor?: string;
}

export interface ListPipelineRunsOptions {
  limit?: number;
  cursor?: string;
}

export function listPipelines(): Promise<ListPipelinesResponse> {
  return request<ListPipelinesResponse>('GET', '/api/v2/pipelines');
}

export function getPipeline(pipelineId: string): Promise<Pipeline> {
  return request<Pipeline>(
    'GET',
    `/api/v2/pipelines/${encodeURIComponent(pipelineId)}`,
  );
}

export function listPipelineRuns(
  pipelineId: string,
  opts: ListPipelineRunsOptions = {},
): Promise<ListPipelineRunsResponse> {
  const params = new URLSearchParams();
  if (opts.limit !== undefined) {
    params.set('limit', String(opts.limit));
  }
  if (opts.cursor) {
    params.set('cursor', opts.cursor);
  }
  const query = params.toString();
  const suffix = query.length > 0 ? `?${query}` : '';
  return request<ListPipelineRunsResponse>(
    'GET',
    `/api/v2/pipelines/${encodeURIComponent(pipelineId)}/runs${suffix}`,
  );
}
