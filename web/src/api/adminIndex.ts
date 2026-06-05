import { request } from './client';

// Admin Bleve index management surface (US-461). Mirrors the two coexisting
// endpoints in cmd/server/admin_index.go:
//
//   POST /api/admin/indexes/rebuild        body { ontology, objectType }
//   POST /api/admin/index/reindex/{objectType}?ontology=<o>   (path-style)
//
// Both rebuild a single ObjectType's Bleve full-text index from the latest
// document source and return the same { scopedKey, indexedCount } shape. The
// rebuild is a heavyweight ops operation (full re-index of the scope), so the
// UI gates it behind an explicit confirmation; this module only carries the
// wire contract. The server returns 503 when no Bleve backend is configured
// (e.g. WEAVE_DATA_DIR wiped / DLQ not ready) and 404 when the
// ontology/objectType pair is unknown — both surface through ApiRequestError.

// IndexRebuildResponse is the success shape returned on 200 by both endpoints.
// scopedKey is the internal `<ontology>:<objectType>` index partition key the
// documents were written under; indexedCount is the number of documents
// re-indexed in this run.
export interface IndexRebuildResponse {
  scopedKey: string;
  indexedCount: number;
}

export interface RebuildIndexRequest {
  ontology: string;
  objectType: string;
}

// rebuildIndex calls the body-style endpoint. Both ontology and objectType
// travel in the JSON body.
export function rebuildIndex(
  req: RebuildIndexRequest,
): Promise<IndexRebuildResponse> {
  return request<IndexRebuildResponse>(
    'POST',
    '/api/admin/indexes/rebuild',
    req,
  );
}

// reindexObjectType calls the REST/path-style endpoint: the objectType travels
// in the URL path and the ontology in the `ontology` query string (the backend
// reads it from r.URL.Query().Get("ontology") and 400s when it is missing).
export function reindexObjectType(
  objectType: string,
  ontology: string,
): Promise<IndexRebuildResponse> {
  const path = `/api/admin/index/reindex/${encodeURIComponent(
    objectType,
  )}?ontology=${encodeURIComponent(ontology)}`;
  return request<IndexRebuildResponse>('POST', path);
}
