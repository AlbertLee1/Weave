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

// US-053: rollback wire shape, mirrors `rollbackResponse` in
// cmd/server/dataset_rollback_handler.go. The handler walks every
// dataset_transactions row strictly newer than `?to=` and reports the
// audited counts (how many objects restored vs deleted) plus the
// bookkeeping transaction stamped as the new chain head.
export interface DatasetRollbackResponse {
  rolledBackTxIds: string[];
  restoredObjects: number;
  deletedObjects: number;
  newTransaction?: DatasetTransaction;
  targetTx?: DatasetTransaction;
}

// rollbackDataset POSTs /api/v2/datasets/{rid}/rollback?to=tx-... and
// resolves with the summary the handler returns. The caller is responsible
// for confirming intent (the wizard's typed dataset name) — the server
// only validates that the target tx exists and belongs to the same
// ontology as `{rid}`.
export function rollbackDataset(
  ontologyRidOrApiName: string,
  targetTxId: string,
): Promise<DatasetRollbackResponse> {
  const path =
    `/api/v2/datasets/${encodeURIComponent(ontologyRidOrApiName)}/rollback` +
    `?to=${encodeURIComponent(targetTxId)}`;
  return request<DatasetRollbackResponse>('POST', path);
}
