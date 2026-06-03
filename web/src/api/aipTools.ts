import { request } from './client';

// AIP Tool Catalog API (US-285).
//
// Mirrors pkg/aip.{ToolRecord,ToolCatalogHandler,createToolRequest,
// updateToolRequest,listToolsResponse}. The catalog is the persisted set of
// custom LLM-visible tools: the (name, description, parameters) triple is the
// OpenAI / Anthropic JSON-schema tool descriptor; handlerFunctionRid is the
// optional handle of a stored Function the runtime dispatches Execute calls
// to. When handlerFunctionRid is empty the tool is "definition-only" — the
// LLM can see it but invocation surfaces an unconfigured error.
//
// Routes are mounted under /api/v2/aip/tools and gated on auth presence
// (tool_handlers.go:45-50). The path lives outside
// /api/(v2|admin)/ontologies/{name}/... so client.ts does NOT branch-rewrite
// these calls — the tool catalog is global.
//
// `parameters` rides the wire as json.RawMessage on the Go side; here we
// surface it as parsed JSON (a Draft-07-ish JSON Schema object) so the UI can
// edit it directly. The backend treats it opaquely.

export interface ToolRecord {
  name: string;
  description?: string;
  parameters?: unknown;
  handlerFunctionRid?: string;
  enabled: boolean;
  createdBy?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ListToolsResponse {
  tools: ToolRecord[];
}

// CreateAipToolRequest mirrors pkg/aip.createToolRequest. enabled is optional
// (defaults to true server-side when omitted).
export interface CreateAipToolRequest {
  name: string;
  description?: string;
  parameters?: unknown;
  handlerFunctionRid?: string;
  enabled?: boolean;
}

// UpdateAipToolRequest mirrors pkg/aip.updateToolRequest — every field is a
// three-state pointer on the Go side (omit = preserve). The UI sends the full
// editable set on save, which the backend applies as a partial update.
export interface UpdateAipToolRequest {
  description?: string;
  parameters?: unknown;
  handlerFunctionRid?: string;
  enabled?: boolean;
}

const TOOLS_PREFIX = '/api/v2/aip/tools';

export async function listAipTools(): Promise<ToolRecord[]> {
  const resp = await request<ListToolsResponse>('GET', TOOLS_PREFIX);
  return resp.tools ?? [];
}

export function getAipTool(name: string): Promise<ToolRecord> {
  return request<ToolRecord>(
    'GET',
    `${TOOLS_PREFIX}/${encodeURIComponent(name)}`,
  );
}

export function createAipTool(body: CreateAipToolRequest): Promise<ToolRecord> {
  return request<ToolRecord>('POST', TOOLS_PREFIX, body);
}

export function updateAipTool(
  name: string,
  body: UpdateAipToolRequest,
): Promise<ToolRecord> {
  return request<ToolRecord>(
    'PUT',
    `${TOOLS_PREFIX}/${encodeURIComponent(name)}`,
    body,
  );
}

export function deleteAipTool(name: string): Promise<void> {
  return request<void>(
    'DELETE',
    `${TOOLS_PREFIX}/${encodeURIComponent(name)}`,
  );
}

// TOOL_NAME_RE mirrors the aip_tools_name_format CHECK constraint and
// pkg/aip.ValidateToolName: a letter start, then alphanumerics/underscores,
// 1..64 chars. Surfacing it client-side lets the create form fast-fail
// before the round trip.
const TOOL_NAME_RE = /^[A-Za-z][A-Za-z0-9_]{0,63}$/;

export function validateToolName(name: string): string | null {
  if (name.trim() === '') return 'Tool name is required.';
  if (!TOOL_NAME_RE.test(name)) {
    return 'Name must start with a letter and contain only letters, digits, or underscores (max 64).';
  }
  return null;
}
