import { request } from './client';

// AIPLogicNode mirrors pkg/aip/logic.Node.
export interface AIPLogicNode {
  id: string;
  type: string;
  config?: Record<string, unknown>;
}

// AIPLogicEdge mirrors pkg/aip/logic.Edge.
export interface AIPLogicEdge {
  from: string;
  to: string;
  branch?: string;
}

// AIPLogicFlow mirrors pkg/aip/logic.Flow on the wire.
export interface AIPLogicFlow {
  id: string;
  name: string;
  description?: string;
  nodes: AIPLogicNode[];
  edges: AIPLogicEdge[];
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}

export interface AIPLogicTraceEntry {
  nodeId: string;
  type: string;
  status: 'success' | 'skipped' | 'failed';
  output?: Record<string, unknown>;
  error?: string;
}

export interface AIPLogicRun {
  id: number;
  flowId: string;
  status: 'success' | 'failed';
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  trace?: AIPLogicTraceEntry[];
  error?: string;
  createdBy?: string;
  createdAt: string;
}

export interface ListLogicFlowsResponse {
  flows: AIPLogicFlow[];
}

export interface ListLogicRunsResponse {
  runs: AIPLogicRun[];
}

export interface CreateLogicFlowRequest {
  id?: string;
  name?: string;
  description?: string;
  nodes: AIPLogicNode[];
  edges: AIPLogicEdge[];
}

export interface UpdateLogicFlowRequest {
  name?: string;
  description?: string;
  nodes?: AIPLogicNode[];
  edges?: AIPLogicEdge[];
}

export interface ExecuteLogicFlowRequest {
  input?: Record<string, unknown>;
}

export const KNOWN_LOGIC_NODE_TYPES = ['llm', 'tool', 'if', 'output'] as const;
export type LogicNodeType = (typeof KNOWN_LOGIC_NODE_TYPES)[number];

export function listLogicFlows(): Promise<ListLogicFlowsResponse> {
  return request<ListLogicFlowsResponse>('GET', '/api/v2/aip/logic-flows');
}

export function getLogicFlow(flowId: string): Promise<AIPLogicFlow> {
  return request<AIPLogicFlow>(
    'GET',
    `/api/v2/aip/logic-flows/${encodeURIComponent(flowId)}`,
  );
}

export function createLogicFlow(
  body: CreateLogicFlowRequest,
): Promise<AIPLogicFlow> {
  return request<AIPLogicFlow>('POST', '/api/v2/aip/logic-flows', body);
}

export function updateLogicFlow(
  flowId: string,
  body: UpdateLogicFlowRequest,
): Promise<AIPLogicFlow> {
  return request<AIPLogicFlow>(
    'PUT',
    `/api/v2/aip/logic-flows/${encodeURIComponent(flowId)}`,
    body,
  );
}

export function deleteLogicFlow(flowId: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/aip/logic-flows/${encodeURIComponent(flowId)}`,
  );
}

export function executeLogicFlow(
  flowId: string,
  body: ExecuteLogicFlowRequest,
): Promise<AIPLogicRun> {
  return request<AIPLogicRun>(
    'POST',
    `/api/v2/aip/logic-flows/${encodeURIComponent(flowId)}/execute`,
    body,
  );
}

export function listLogicRuns(
  flowId: string,
): Promise<ListLogicRunsResponse> {
  return request<ListLogicRunsResponse>(
    'GET',
    `/api/v2/aip/logic-flows/${encodeURIComponent(flowId)}/runs`,
  );
}
