import { z } from 'zod';
import type { ActionParameterV2 } from '../../api/types';

// buildParameterZodSchema constructs a Zod schema mirroring the action's
// declared parameters. It powers client-side field validation in
// ParameterForm/ActionConsolePage and is intentionally permissive about
// non-required fields — empty inputs collapse to `undefined` so the wire
// payload stays clean.
export function buildParameterZodSchema(
  parameters: Record<string, ActionParameterV2>,
): z.ZodObject<Record<string, z.ZodTypeAny>> {
  const shape: Record<string, z.ZodTypeAny> = {};
  for (const [key, def] of Object.entries(parameters ?? {})) {
    shape[key] = buildFieldSchema(def);
  }
  return z.object(shape);
}

function buildFieldSchema(def: ActionParameterV2): z.ZodTypeAny {
  const type = def.dataType?.type ?? 'string';

  if (type === 'boolean') {
    // Checkbox always carries a value; required is meaningless.
    return z.boolean().optional();
  }

  if (type === 'integer' || type === 'double') {
    let field: z.ZodTypeAny = z.number({ message: 'Must be a number' });
    if (type === 'integer') {
      field = (field as z.ZodNumber).int('Must be an integer');
    }
    return def.required ? field : field.optional();
  }

  if (type === 'array') {
    let field: z.ZodTypeAny = z.array(z.string());
    if (def.required) {
      field = (field as z.ZodArray<z.ZodString>).min(1, 'Required');
    } else {
      field = field.optional();
    }
    return field;
  }

  // Default: string-like
  let field: z.ZodTypeAny = z.string();
  if (def.required) {
    field = (field as z.ZodString).min(1, 'Required');
  } else {
    field = field.optional();
  }
  return field;
}

// Default form values for a fresh action selection. Required fields default
// to empty so the form has a defined shape; optional fields stay undefined.
export function buildParameterDefaults(
  parameters: Record<string, ActionParameterV2>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, def] of Object.entries(parameters ?? {})) {
    const type = def.dataType?.type ?? 'string';
    if (type === 'boolean') {
      out[key] = false;
    } else if (type === 'array') {
      out[key] = [];
    } else if (type === 'integer' || type === 'double') {
      out[key] = undefined;
    } else {
      out[key] = '';
    }
  }
  return out;
}

export interface SchemaViolation {
  field: string;
  reason: string;
  keyword?: string;
}

// parseSchemaViolations extracts field-level violations from an API error's
// `parameters` map. Mirrors pkg/actions/parameter_schema.go::APIError():
// `violations` is JSON-encoded; falls back to the single-field `field`/`reason`
// pair when the bulk envelope is absent.
export function parseSchemaViolations(
  params: Record<string, string> | undefined,
): SchemaViolation[] {
  if (!params) return [];
  const raw = params.violations;
  if (raw) {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (Array.isArray(parsed)) {
        return parsed
          .filter(
            (v): v is SchemaViolation =>
              !!v && typeof v === 'object' && typeof (v as SchemaViolation).field === 'string',
          )
          .map((v) => ({
            field: v.field,
            reason: typeof v.reason === 'string' ? v.reason : 'Invalid value',
            keyword: typeof v.keyword === 'string' ? v.keyword : undefined,
          }));
      }
    } catch {
      // fall through to single-field shape
    }
  }
  if (typeof params.field === 'string' && typeof params.reason === 'string') {
    return [{ field: params.field, reason: params.reason, keyword: params.keyword }];
  }
  return [];
}
