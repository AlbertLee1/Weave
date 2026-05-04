// OpenAPI wire-format types — the canonical shapes the Weave server
// emits and accepts on its REST API. These are deliberately decoupled
// from the runtime client so they can be regenerated from `api/openapi.yaml`
// without rewriting transport code.
//
// Hand-curated subset covering the endpoints exercised by the
// Object/Action/Function/Subscribe clients in this OSDK. Mirror the Go
// structs in `pkg/oms`, `pkg/actions`, `pkg/oss/objectset`,
// `pkg/subscriptions`.

export interface Ontology {
  apiName: string;
  displayName: string;
  description?: string;
  rid?: string;
}

export interface PropertyDefinition {
  apiName: string;
  displayName?: string;
  baseType: string;
  required?: boolean;
  isPrimaryKey?: boolean;
}

export interface ObjectType {
  apiName: string;
  displayName: string;
  description?: string;
  primaryKey?: string;
  properties?: PropertyDefinition[];
  rid?: string;
}

export interface LinkType {
  apiName: string;
  displayName?: string;
  sourceObjectType: string;
  targetObjectType: string;
  cardinality?: 'ONE_TO_ONE' | 'ONE_TO_MANY' | 'MANY_TO_ONE' | 'MANY_TO_MANY';
}

export interface ActionParameter {
  name: string;
  baseType: string;
  required?: boolean;
}

export interface ActionType {
  apiName: string;
  displayName?: string;
  description?: string;
  parameters?: ActionParameter[];
  rid?: string;
}

export interface FunctionDefinition {
  apiName: string;
  displayName?: string;
  rid?: string;
  version?: string;
  language?: string;
}

export interface ListResponse<T> {
  data: T[];
  nextPageToken?: string;
}

export type ObjectRow = Record<string, unknown> & {
  __primaryKey?: string;
  __apiName?: string;
};

export interface ObjectPage {
  data: ObjectRow[];
  nextPageToken?: string;
}

export interface APIError {
  errorCode: string;
  errorName: string;
  message?: string;
  parameters?: Record<string, unknown>;
}

export interface ApplyActionRequest {
  parameters: Record<string, unknown>;
  options?: {
    returnEdits?: 'NONE' | 'CHANGES' | 'ALL';
  };
}

export interface ApplyActionResponse {
  edits?: Array<Record<string, unknown>>;
  validation?: { result: 'VALID' | 'INVALID'; submissionCriteria?: unknown[] };
}

export interface ApplyBatchRequest {
  requests: ApplyActionRequest[];
  options?: { returnEdits?: 'NONE' | 'CHANGES' | 'ALL' };
}

export interface ApplyBatchResponse {
  edits?: Array<Record<string, unknown>>;
  results?: ApplyActionResponse[];
}

export interface ExecuteFunctionRequest {
  parameters: Record<string, unknown>;
}

export interface ExecuteFunctionResponse {
  result: unknown;
}

export type ChangeState = 'ADDED_OR_UPDATED' | 'DELETED';

export interface ObjectChangeEvent {
  state: ChangeState;
  object: ObjectRow;
}

export interface SubscribeMessage {
  type: string;
  connectionId?: string;
  subscriptionId?: string;
  cursor?: number;
  lastEventId?: number;
  data?: unknown;
  error?: string;
}

export interface SubscribeRequestPayload {
  objectType: string;
  where?: Record<string, unknown>;
  select?: string[];
}
