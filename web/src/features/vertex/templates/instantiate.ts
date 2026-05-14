export type TemplateParameter =
  | { name: string; type: 'rid'; required: boolean }
  | { name: string; type: 'string'; required: boolean }
  | { name: string; type: 'number'; required: boolean; min?: number; max?: number };

export interface TemplateSchema {
  rid: string;
  name: string;
  parameters: TemplateParameter[];
}

export type ParamValues = Record<string, unknown>;

export type ValidationResult =
  | { valid: true }
  | { valid: false; errors: Record<string, string> };

export interface InstantiatePayload {
  templateRid: string;
  args: Record<string, unknown>;
}

const RID_RE = /^ri\.[a-z][a-z0-9_-]*\.[a-z0-9_-]+\.[a-z0-9_-]+\.[A-Za-z0-9_.-]+$/;

export function validateTemplateParams(
  schema: TemplateSchema,
  values: ParamValues,
): ValidationResult {
  const errors: Record<string, string> = {};
  for (const param of schema.parameters) {
    const v = values[param.name];
    const missing = v === undefined || v === null || v === '';
    if (missing) {
      if (param.required) errors[param.name] = `${param.name} is required`;
      continue;
    }
    if (param.type === 'rid') {
      if (typeof v !== 'string' || !RID_RE.test(v)) {
        errors[param.name] = `${param.name} must be a RID`;
      }
    } else if (param.type === 'number') {
      if (typeof v !== 'number' || !Number.isFinite(v)) {
        errors[param.name] = `${param.name} must be a number`;
      } else {
        if (param.min !== undefined && v < param.min) {
          errors[param.name] = `${param.name} below min ${param.min}`;
        }
        if (param.max !== undefined && v > param.max) {
          errors[param.name] = `${param.name} above max ${param.max}`;
        }
      }
    } else if (param.type === 'string') {
      if (typeof v !== 'string') errors[param.name] = `${param.name} must be a string`;
    }
  }
  if (Object.keys(errors).length === 0) return { valid: true };
  return { valid: false, errors };
}

export function buildInstantiatePayload(
  schema: TemplateSchema,
  values: ParamValues,
): InstantiatePayload {
  const allowed = new Set(schema.parameters.map((p) => p.name));
  const args: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(values)) {
    if (allowed.has(k) && v !== undefined && v !== '') args[k] = v;
  }
  return { templateRid: schema.rid, args };
}
