export interface VertexDeepLink {
  objectRid?: string;
  objectSetRid?: string;
  searchAroundFnRid?: string;
  selectObjectRid?: string;
  selectedTimeMs?: number;
  timeWindow?: { from: number; to: number };
}

const RID_RE = /^ri\.[a-z][a-z0-9_-]*\.[a-z0-9_-]+\.[a-z0-9_-]+\.[A-Za-z0-9_.-]+$/;

function ridParam(p: URLSearchParams, key: string): string | undefined {
  const v = p.get(key);
  if (!v) return undefined;
  return RID_RE.test(v) ? v : undefined;
}

function isoParam(p: URLSearchParams, key: string): number | undefined {
  const v = p.get(key);
  if (!v) return undefined;
  const n = Date.parse(v);
  return Number.isFinite(n) ? n : undefined;
}

export function parseDeepLink(p: URLSearchParams): VertexDeepLink {
  const out: VertexDeepLink = {};
  const objectRid = ridParam(p, 'objectRid');
  if (objectRid) out.objectRid = objectRid;
  const objectSetRid = ridParam(p, 'objectSetRid');
  if (objectSetRid) out.objectSetRid = objectSetRid;
  const searchAroundFnRid = ridParam(p, 'searchAroundFnRid');
  if (searchAroundFnRid) out.searchAroundFnRid = searchAroundFnRid;
  const selectObjectRid = ridParam(p, 'selectObjectRid');
  if (selectObjectRid) out.selectObjectRid = selectObjectRid;

  const selectedTimeMs = isoParam(p, 'selectedTime');
  if (selectedTimeMs !== undefined) out.selectedTimeMs = selectedTimeMs;

  const from = isoParam(p, 'startTime');
  const to = isoParam(p, 'endTime');
  if (from !== undefined && to !== undefined) {
    out.timeWindow = { from, to };
  }
  return out;
}
