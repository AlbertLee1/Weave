import yaml from 'js-yaml';

export interface OpenApiSchema {
  type?: string;
  format?: string;
  enum?: unknown[];
  $ref?: string;
  items?: OpenApiSchema;
  properties?: Record<string, OpenApiSchema>;
  required?: string[];
  description?: string;
  example?: unknown;
  default?: unknown;
  oneOf?: OpenApiSchema[];
  anyOf?: OpenApiSchema[];
  allOf?: OpenApiSchema[];
  additionalProperties?: boolean | OpenApiSchema;
}

export interface OpenApiParameter {
  name: string;
  in: 'path' | 'query' | 'header' | 'cookie';
  required?: boolean;
  description?: string;
  schema?: OpenApiSchema;
}

export interface OpenApiRequestBody {
  required?: boolean;
  description?: string;
  content?: Record<string, { schema?: OpenApiSchema }>;
}

export interface OpenApiOperation {
  tags?: string[];
  operationId?: string;
  summary?: string;
  description?: string;
  parameters?: OpenApiParameter[];
  requestBody?: OpenApiRequestBody;
  responses?: Record<string, unknown>;
}

export interface OpenApiPathItem {
  parameters?: OpenApiParameter[];
  get?: OpenApiOperation;
  put?: OpenApiOperation;
  post?: OpenApiOperation;
  delete?: OpenApiOperation;
  patch?: OpenApiOperation;
  options?: OpenApiOperation;
  head?: OpenApiOperation;
}

export interface OpenApiSpec {
  openapi?: string;
  info?: { title?: string; version?: string; description?: string };
  servers?: { url: string; description?: string }[];
  tags?: { name: string; description?: string }[];
  paths?: Record<string, OpenApiPathItem>;
  components?: {
    schemas?: Record<string, OpenApiSchema>;
    parameters?: Record<string, OpenApiParameter>;
    requestBodies?: Record<string, OpenApiRequestBody>;
  };
}

export interface Endpoint {
  method: string;
  path: string;
  operationId: string;
  summary: string;
  description: string;
  tags: string[];
  parameters: OpenApiParameter[];
  requestBody?: OpenApiRequestBody;
  hasBody: boolean;
}

const METHODS: (keyof OpenApiPathItem)[] = [
  'get',
  'post',
  'put',
  'patch',
  'delete',
  'options',
  'head',
];

export function parseOpenApiYaml(text: string): OpenApiSpec {
  const parsed = yaml.load(text);
  if (!parsed || typeof parsed !== 'object') {
    throw new Error('Invalid OpenAPI document');
  }
  return parsed as OpenApiSpec;
}

export function extractEndpoints(spec: OpenApiSpec): Endpoint[] {
  const out: Endpoint[] = [];
  const paths = spec.paths ?? {};
  for (const [path, item] of Object.entries(paths)) {
    if (!item) continue;
    const pathParams = item.parameters ?? [];
    for (const method of METHODS) {
      const op = item[method] as OpenApiOperation | undefined;
      if (!op) continue;
      const params = [...pathParams, ...(op.parameters ?? [])];
      out.push({
        method: method.toUpperCase(),
        path,
        operationId: op.operationId ?? `${method.toUpperCase()} ${path}`,
        summary: op.summary ?? '',
        description: op.description ?? '',
        tags: op.tags ?? [],
        parameters: params,
        requestBody: op.requestBody,
        hasBody: Boolean(op.requestBody),
      });
    }
  }
  return out;
}

export function groupByTag(endpoints: Endpoint[]): Record<string, Endpoint[]> {
  const groups: Record<string, Endpoint[]> = {};
  for (const ep of endpoints) {
    const tag = ep.tags[0] ?? 'Other';
    (groups[tag] ??= []).push(ep);
  }
  for (const tag of Object.keys(groups)) {
    groups[tag].sort((a, b) =>
      `${a.path} ${a.method}`.localeCompare(`${b.path} ${b.method}`),
    );
  }
  return groups;
}

export function buildRequestUrl(
  path: string,
  pathParams: Record<string, string>,
  queryParams: Record<string, string>,
): string {
  let url = path.replace(/\{([^}]+)\}/g, (_match, name: string) => {
    const v = pathParams[name] ?? '';
    return encodeURIComponent(v);
  });
  const qs = Object.entries(queryParams)
    .filter(([, v]) => v !== '' && v !== undefined && v !== null)
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join('&');
  if (qs) url += `?${qs}`;
  return url;
}

export function resolveRef(
  spec: OpenApiSpec,
  ref: string,
): OpenApiSchema | undefined {
  if (!ref.startsWith('#/')) return undefined;
  const segments = ref.slice(2).split('/');
  let cur: unknown = spec;
  for (const seg of segments) {
    if (!cur || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[seg];
  }
  return (cur ?? undefined) as OpenApiSchema | undefined;
}

export function exampleForSchema(
  schema: OpenApiSchema | undefined,
  spec: OpenApiSpec,
  seen: Set<string> = new Set(),
): unknown {
  if (!schema) return null;
  if (schema.$ref) {
    if (seen.has(schema.$ref)) return null;
    seen.add(schema.$ref);
    const resolved = resolveRef(spec, schema.$ref);
    return exampleForSchema(resolved, spec, seen);
  }
  if (schema.example !== undefined) return schema.example;
  if (schema.default !== undefined) return schema.default;
  if (schema.enum && schema.enum.length > 0) return schema.enum[0];
  if (schema.allOf?.length) {
    const merged: Record<string, unknown> = {};
    for (const part of schema.allOf) {
      const v = exampleForSchema(part, spec, seen);
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        Object.assign(merged, v);
      }
    }
    return merged;
  }
  if (schema.oneOf?.length) return exampleForSchema(schema.oneOf[0], spec, seen);
  if (schema.anyOf?.length) return exampleForSchema(schema.anyOf[0], spec, seen);

  switch (schema.type) {
    case 'string':
      return schema.format === 'date-time' ? '1970-01-01T00:00:00Z' : '';
    case 'integer':
    case 'number':
      return 0;
    case 'boolean':
      return false;
    case 'array':
      return [exampleForSchema(schema.items, spec, seen)].filter((v) => v !== null);
    case 'object':
    default: {
      if (schema.properties) {
        const obj: Record<string, unknown> = {};
        for (const [key, sub] of Object.entries(schema.properties)) {
          obj[key] = exampleForSchema(sub, spec, seen);
        }
        return obj;
      }
      return {};
    }
  }
}
