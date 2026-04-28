import type { WireObject } from '../api/types';

export interface ObjectSetDiff {
  onlyInA: WireObject[];
  onlyInB: WireObject[];
  changed: ChangedRow[];
}

export interface ChangedRow {
  primaryKey: string;
  rowA: WireObject;
  rowB: WireObject;
  fieldChanges: FieldChange[];
}

export interface FieldChange {
  field: string;
  valueA: unknown;
  valueB: unknown;
}

const ENVELOPE_KEYS = new Set(['__rid', '__primaryKey', '__apiName']);

export function diffObjectSets(
  a: WireObject[],
  b: WireObject[],
): ObjectSetDiff {
  const aByKey = new Map<string, WireObject>();
  for (const row of a) aByKey.set(String(row.__primaryKey), row);
  const bByKey = new Map<string, WireObject>();
  for (const row of b) bByKey.set(String(row.__primaryKey), row);

  const onlyInA: WireObject[] = [];
  const onlyInB: WireObject[] = [];
  const changed: ChangedRow[] = [];

  for (const row of a) {
    const key = String(row.__primaryKey);
    if (!bByKey.has(key)) {
      onlyInA.push(row);
      continue;
    }
    const counterpart = bByKey.get(key)!;
    const fieldChanges = diffProperties(row, counterpart);
    if (fieldChanges.length > 0) {
      changed.push({ primaryKey: key, rowA: row, rowB: counterpart, fieldChanges });
    }
  }

  for (const row of b) {
    const key = String(row.__primaryKey);
    if (!aByKey.has(key)) onlyInB.push(row);
  }

  return { onlyInA, onlyInB, changed };
}

function diffProperties(a: WireObject, b: WireObject): FieldChange[] {
  const fields = new Set<string>();
  for (const k of Object.keys(a)) if (!ENVELOPE_KEYS.has(k)) fields.add(k);
  for (const k of Object.keys(b)) if (!ENVELOPE_KEYS.has(k)) fields.add(k);

  const out: FieldChange[] = [];
  for (const field of fields) {
    const va = a[field];
    const vb = b[field];
    if (!deepEqual(va, vb)) {
      out.push({ field, valueA: va, valueB: vb });
    }
  }
  out.sort((x, y) => x.field.localeCompare(y.field));
  return out;
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a === null || b === null) return false;
  if (typeof a !== typeof b) return false;
  if (typeof a !== 'object') return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (!deepEqual(a[i], b[i])) return false;
    }
    return true;
  }
  const oa = a as Record<string, unknown>;
  const ob = b as Record<string, unknown>;
  const ka = Object.keys(oa);
  const kb = Object.keys(ob);
  if (ka.length !== kb.length) return false;
  for (const k of ka) {
    if (!Object.prototype.hasOwnProperty.call(ob, k)) return false;
    if (!deepEqual(oa[k], ob[k])) return false;
  }
  return true;
}
