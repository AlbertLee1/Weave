import { describe, it, expect } from 'vitest';
import {
  enterReadOnlyMode,
  exitReadOnlyMode,
  initialHistoryState,
  type HistoryState,
  type VersionEntry,
} from './historyView';

const versions: VersionEntry[] = [
  { version: 1, createdAt: 1000, createdBy: 'alice', diffSummary: 'init' },
  { version: 2, createdAt: 2000, createdBy: 'bob', diffSummary: '+5 nodes' },
  { version: 3, createdAt: 3000, createdBy: 'carol', diffSummary: '+1 edge' },
];

describe('VTX-083 history state machine', () => {
  it('given_InitialState_when_Created_then_NotReadOnly', () => {
    const state = initialHistoryState(versions);
    expect(state.readOnly).toBe(false);
    expect(state.viewingVersion).toBeNull();
    expect(state.versions).toHaveLength(3);
  });

  it('given_LiveState_when_EnterReadOnly_then_ViewingVersionSet', () => {
    const s0 = initialHistoryState(versions);
    const s1 = enterReadOnlyMode(s0, 2);
    expect(s1.readOnly).toBe(true);
    expect(s1.viewingVersion).toBe(2);
  });

  it('given_EnterUnknownVersion_then_Throws', () => {
    const s0 = initialHistoryState(versions);
    expect(() => enterReadOnlyMode(s0, 99)).toThrow(/version/);
  });

  it('given_ReadOnly_when_ExitReadOnly_then_NotReadOnlyAnymore', () => {
    const s0 = initialHistoryState(versions);
    const s1 = enterReadOnlyMode(s0, 2);
    const s2 = exitReadOnlyMode(s1);
    expect(s2.readOnly).toBe(false);
    expect(s2.viewingVersion).toBeNull();
  });

  it('given_StateOps_then_OriginalNotMutated', () => {
    const s0 = initialHistoryState(versions);
    enterReadOnlyMode(s0, 1);
    expect(s0.readOnly).toBe(false);
  });
});

describe('VTX-083 latest version helper', () => {
  it('given_VersionsList_when_InitialState_then_LatestVersionExposed', () => {
    const s: HistoryState = initialHistoryState(versions);
    expect(s.latestVersion).toBe(3);
  });

  it('given_EmptyVersions_when_InitialState_then_LatestVersionNull', () => {
    const s = initialHistoryState([]);
    expect(s.latestVersion).toBeNull();
  });
});
