import type { ActionType } from '../../api/types';

// Locate an ActionType whose rules contain a modifyObject rule for
// `objectTypeApiName` whose propertyBindings include `propertyApiName`.
// Returns the action plus the parameter ID bound to that property and the
// parameter ID treated as the primary key.
//
// The rule shape mirrors `pkg/actions/rules.go`:
//   { type: "modifyObject", objectType, propertyBindings: { <prop>: {type: "parameter", value: <paramId>} } }
//
// Primary-key resolution follows `findPrimaryKey` server-side (`primaryKey`,
// `<objectType>Id`, `id`); we additionally match any bound `*[Pp]rimaryKey*`
// parameter as a fallback so well-named custom params still work.
export interface ModifyActionMatch {
  action: ActionType;
  // Map from property apiName -> parameter id bound to that property.
  propertyParams: Record<string, string>;
  primaryKeyParam: string;
}

export function findModifyActionForProperty(
  actions: ActionType[],
  objectTypeApiName: string,
  propertyApiName: string,
): ModifyActionMatch | null {
  for (const action of actions) {
    const match = matchModifyAction(action, objectTypeApiName);
    if (!match) continue;
    if (Object.prototype.hasOwnProperty.call(match.propertyParams, propertyApiName)) {
      return match;
    }
  }
  return null;
}

function matchModifyAction(
  action: ActionType,
  objectTypeApiName: string,
): ModifyActionMatch | null {
  const rules = action.rules;
  if (!Array.isArray(rules)) return null;
  for (const rule of rules) {
    if (!rule || typeof rule !== 'object') continue;
    const r = rule as Record<string, unknown>;
    if (r.type !== 'modifyObject') continue;
    if (typeof r.objectType !== 'string' || r.objectType !== objectTypeApiName) {
      continue;
    }
    const bindings =
      r.propertyBindings && typeof r.propertyBindings === 'object'
        ? (r.propertyBindings as Record<string, unknown>)
        : {};
    const propertyParams: Record<string, string> = {};
    for (const [propName, binding] of Object.entries(bindings)) {
      const paramId = bindingParamId(binding);
      if (paramId) propertyParams[propName] = paramId;
    }
    const primaryKeyParam = pickPrimaryKeyParam(action, objectTypeApiName);
    if (!primaryKeyParam) continue;
    return { action, propertyParams, primaryKeyParam };
  }
  return null;
}

function bindingParamId(binding: unknown): string | null {
  if (!binding || typeof binding !== 'object') return null;
  const b = binding as Record<string, unknown>;
  if (b.type === 'parameter' && typeof b.value === 'string') return b.value;
  return null;
}

function pickPrimaryKeyParam(
  action: ActionType,
  objectTypeApiName: string,
): string | null {
  const params = Object.keys(action.parameters ?? {});
  if (params.length === 0) return null;
  const lowered = objectTypeApiName.toLowerCase();
  // Server resolves in this order — keep the same precedence.
  const exactCandidates = ['primaryKey', `${objectTypeApiName}Id`, 'id'];
  for (const name of exactCandidates) {
    if (params.includes(name)) return name;
  }
  // Fallback: any param whose name contains "primarykey" (case-insensitive)
  // or the objectType + "id" suffix in any casing.
  const fuzzy = params.find((p) => {
    const low = p.toLowerCase();
    return low.includes('primarykey') || low === `${lowered}id`;
  });
  return fuzzy ?? null;
}
