import { request } from './client';
import type { LinkEdge, LinkProperty } from './types';

// US-210 / US-497 — LinkProperty schema + per-edge value PUT. The schema
// list comes from GET /links/{rid}/properties; per-edge values are
// replaced wholesale by PUT /links/{rid}/edges/{src}/{tgt}/properties.

export async function listLinkProperties(
  ontologyApiName: string,
  linkTypeRid: string,
): Promise<LinkProperty[]> {
  const resp = await request<{ data: LinkProperty[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/links/${encodeURIComponent(linkTypeRid)}/properties`,
  );
  return resp.data ?? [];
}

export function putLinkEdgeProperties(
  ontologyApiName: string,
  linkTypeRid: string,
  sourcePk: string,
  targetPk: string,
  values: Record<string, unknown>,
): Promise<LinkEdge> {
  return request<LinkEdge>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/links/${encodeURIComponent(linkTypeRid)}/edges/${encodeURIComponent(sourcePk)}/${encodeURIComponent(targetPk)}/properties`,
    { values },
  );
}
