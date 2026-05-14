export type ScenarioStatus = 'draft' | 'running' | 'success' | 'failed' | 'applied';

export interface ApplyButtonState {
  status: ScenarioStatus;
}

export function shouldShowApplyButton(state: ApplyButtonState): boolean {
  return state.status === 'success';
}

export function buildConfirmText(editCount: number): string {
  const word = editCount === 1 ? 'edit' : 'edits';
  return `This will write ${editCount} ${word} to Main ontology. Continue?`;
}

export interface ApplyErrorResponse {
  status: number;
  body: {
    error?: string;
    conflicts?: Array<{ editSeq: number; message: string }>;
    op?: string;
  };
}

export type ApplyErrorResult =
  | { kind: 'conflict'; highlightedEditSeqs: number[]; messages: string[] }
  | { kind: 'alreadyApplied' }
  | { kind: 'permissionDenied'; op?: string }
  | { kind: 'error'; status: number; message: string };

export function parseApplyErrorResponse(res: ApplyErrorResponse): ApplyErrorResult {
  if (res.status === 409 && res.body.conflicts) {
    return {
      kind: 'conflict',
      highlightedEditSeqs: res.body.conflicts.map((c) => c.editSeq),
      messages: res.body.conflicts.map((c) => c.message),
    };
  }
  if (res.status === 400 && res.body.error === 'AlreadyApplied') {
    return { kind: 'alreadyApplied' };
  }
  if (res.status === 403 && res.body.error === 'PermissionDenied') {
    return { kind: 'permissionDenied', op: res.body.op };
  }
  return {
    kind: 'error',
    status: res.status,
    message: res.body.error ?? 'Unknown error',
  };
}
