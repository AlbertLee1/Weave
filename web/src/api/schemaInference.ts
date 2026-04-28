import { request } from './client';

// SchemaField mirrors pkg/pipeline/schema.Field on the wire.
export interface SchemaField {
  name: string;
  baseType: string;
  nullable: boolean;
  samples?: string[];
  nonNullCount: number;
  nullCount: number;
}

// SchemaInferenceResult mirrors pkg/pipeline/schema.Result on the wire.
export interface SchemaInferenceResult {
  format: 'csv' | 'json' | 'ndjson';
  rowsScanned: number;
  fields: SchemaField[];
  sampleRows: number;
  hasHeader?: boolean;
  truncated?: boolean;
  warningCount?: number;
}

export interface SchemaInferenceOptions {
  sampleRows?: number;
  hasHeader?: boolean;
  delimiter?: string;
}

export interface InferSchemaRequest {
  format: 'csv' | 'json' | 'ndjson';
  sample: string;
  options?: SchemaInferenceOptions;
}

// inferSchema posts an inline sample (CSV / JSON / NDJSON) and gets
// back inferred per-column types. The endpoint is gated on PG-mode on
// the server: degraded-mode deployments will 404 until a pipeline
// store is wired (US-287).
export function inferSchema(req: InferSchemaRequest): Promise<SchemaInferenceResult> {
  return request<SchemaInferenceResult>('POST', '/api/v2/pipelines/schema/infer', req);
}

// SUPPORTED_BASE_TYPES lists the override choices the UI surfaces in
// the type-confirmation dropdown. Mirrors the v1 inference cascade
// from pkg/pipeline/schema.
export const SUPPORTED_BASE_TYPES = [
  'string',
  'integer',
  'long',
  'double',
  'boolean',
  'date',
  'timestamp',
] as const;

export type SupportedBaseType = (typeof SUPPORTED_BASE_TYPES)[number];
