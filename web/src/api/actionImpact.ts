import { request } from './client';

// ImpactObject mirrors the backend's per-object impact row. objectType /
// primaryKey are optional because non-object resources (e.g. links, edits on
// derived state) may not carry them. operation is the edit verb the action
// produced (CREATE / MODIFY / DELETE). See GET /api/v2/actions/{rid}/impact
// in cmd/server/main.go (mounted ~line 1204).
export interface ImpactObject {
  rid: string;
  objectType?: string;
  primaryKey?: string;
  operation?: string;
  timestamp: string;
}

export interface ActionImpactResponse {
  actionRid: string;
  // actionLog is opaque JSON the UI renders as-is when present.
  actionLog?: unknown;
  objects: ImpactObject[];
  // truncated signals the server capped the impact list — the UI shows a
  // "partial impact" warning so callers know the set is incomplete.
  truncated: boolean;
}

// actionLogRid builds the action-log RID from a numeric action-history entry
// id. The impact endpoint is keyed by this RID, not the raw id.
export function actionLogRid(id: number): string {
  return `ri.actions.main.action-log.${id}`;
}

// getActionImpact fetches the impact analysis for one already-executed action,
// addressed by its action-log RID. The endpoint lives outside the
// /api/v2/ontologies/ surface, so no branch / asOf query params are injected.
export function getActionImpact(
  actionLogRid: string,
): Promise<ActionImpactResponse> {
  return request<ActionImpactResponse>(
    'GET',
    `/api/v2/actions/${encodeURIComponent(actionLogRid)}/impact`,
  );
}
