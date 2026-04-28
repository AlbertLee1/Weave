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

export interface ListPipelinesResponse {
  pipelines: Pipeline[];
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
