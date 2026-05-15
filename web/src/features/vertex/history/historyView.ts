export interface VersionEntry {
  version: number;
  createdAt: number;
  createdBy: string;
  diffSummary: string;
}

export interface HistoryState {
  versions: VersionEntry[];
  latestVersion: number | null;
  readOnly: boolean;
  viewingVersion: number | null;
}

export function initialHistoryState(versions: VersionEntry[]): HistoryState {
  const latest = versions.reduce<number | null>(
    (max, v) => (max === null || v.version > max ? v.version : max),
    null,
  );
  return {
    versions,
    latestVersion: latest,
    readOnly: false,
    viewingVersion: null,
  };
}

export function enterReadOnlyMode(state: HistoryState, version: number): HistoryState {
  if (!state.versions.some((v) => v.version === version)) {
    throw new Error(`enterReadOnlyMode: unknown version ${version}`);
  }
  return { ...state, readOnly: true, viewingVersion: version };
}

export function exitReadOnlyMode(state: HistoryState): HistoryState {
  return { ...state, readOnly: false, viewingVersion: null };
}
