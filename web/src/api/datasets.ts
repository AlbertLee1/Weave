import { request } from './client';

// US-048: dataset_transactions chain row, mirrors the JSON shape served
// by GET /api/v2/datasets/{rid}/history (cmd/server/dataset_transaction_handler.go).
// Fields are 1:1 with pkg/oms.DatasetTransaction's JSON tags so SDK
// callers can walk the audit chain without re-mapping.
export interface DatasetTransaction {
  txId: string;
  parentTxId?: string;
  ontologyApiName: string;
  committedAt: string;
  editsCount: number;
  userId?: string;
  rolledBackAt?: string;
  rolledBackToTxId?: string;
}

export interface DatasetHistoryResponse {
  transactions: DatasetTransaction[];
}

// listDatasetHistory fetches the committed-at-DESC transaction chain for
// the given ontology (accepts either an apiName or a RID — the server
// resolves through GetOntology). The response cap is 1000 rows; we do
// not paginate yet because single-machine deployments rarely exceed
// that horizon (the server-side TODO is to add ?pageToken= when needed).
export function listDatasetHistory(
  ontologyRidOrApiName: string,
): Promise<DatasetHistoryResponse> {
  return request<DatasetHistoryResponse>(
    'GET',
    `/api/v2/datasets/${encodeURIComponent(ontologyRidOrApiName)}/history`,
  );
}
