import { describe, it, expect } from 'vitest';
import {
  shouldShowApplyButton,
  buildConfirmText,
  parseApplyErrorResponse,
  type ApplyButtonState,
} from './applyButton';

describe('VTX-091 shouldShowApplyButton', () => {
  it('given_StatusSuccess_then_True', () => {
    const state: ApplyButtonState = { status: 'success' };
    expect(shouldShowApplyButton(state)).toBe(true);
  });

  it('given_StatusRunning_then_False', () => {
    expect(shouldShowApplyButton({ status: 'running' })).toBe(false);
  });

  it('given_StatusDraft_then_False', () => {
    expect(shouldShowApplyButton({ status: 'draft' })).toBe(false);
  });

  it('given_StatusApplied_then_False', () => {
    expect(shouldShowApplyButton({ status: 'applied' })).toBe(false);
  });

  it('given_StatusFailed_then_False', () => {
    expect(shouldShowApplyButton({ status: 'failed' })).toBe(false);
  });
});

describe('VTX-091 buildConfirmText', () => {
  it('given_1Edit_then_Singular', () => {
    expect(buildConfirmText(1)).toBe(
      'This will write 1 edit to Main ontology. Continue?',
    );
  });

  it('given_5Edits_then_Plural', () => {
    expect(buildConfirmText(5)).toBe(
      'This will write 5 edits to Main ontology. Continue?',
    );
  });

  it('given_0Edits_then_StillReadable', () => {
    expect(buildConfirmText(0)).toBe(
      'This will write 0 edits to Main ontology. Continue?',
    );
  });
});

describe('VTX-091 parseApplyErrorResponse', () => {
  it('given_409_when_Parse_then_ConflictWithEditIds', () => {
    const r = parseApplyErrorResponse({
      status: 409,
      body: {
        error: 'ScenarioApplyConflict',
        conflicts: [{ editSeq: 3, message: 'object missing' }],
      },
    });
    expect(r.kind).toBe('conflict');
    if (r.kind !== 'conflict') return;
    expect(r.highlightedEditSeqs).toEqual([3]);
  });

  it('given_400AlreadyApplied_when_Parse_then_AlreadyApplied', () => {
    const r = parseApplyErrorResponse({
      status: 400,
      body: { error: 'AlreadyApplied' },
    });
    expect(r.kind).toBe('alreadyApplied');
  });

  it('given_500_when_Parse_then_GenericError', () => {
    const r = parseApplyErrorResponse({ status: 500, body: { error: 'oops' } });
    expect(r.kind).toBe('error');
  });

  it('given_PermissionDenied_when_Parse_then_PermissionKind', () => {
    const r = parseApplyErrorResponse({
      status: 403,
      body: { error: 'PermissionDenied', op: 'modifyProperty' },
    });
    expect(r.kind).toBe('permissionDenied');
  });
});
