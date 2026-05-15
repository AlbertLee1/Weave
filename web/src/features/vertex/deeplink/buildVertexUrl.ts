export interface VertexUrlInput {
  objectRids?: string[];
  objectSetRid?: string;
}

const BASE = '/vertex/new';

export function buildVertexUrlFromSelection(input: VertexUrlInput): string {
  if (input.objectSetRid) {
    return `${BASE}?${new URLSearchParams({ objectSetRid: input.objectSetRid }).toString()}`;
  }
  const rids = input.objectRids ?? [];
  if (rids.length === 0) return BASE;
  if (rids.length === 1) {
    return `${BASE}?${new URLSearchParams({ objectRid: rids[0] }).toString()}`;
  }
  return `${BASE}?${new URLSearchParams({ objectRids: rids.join(',') }).toString()}`;
}
