import { describe, it, expect, beforeEach } from 'vitest';
import {
  DEFAULT_BRANCH,
  activeBranchFor,
  useBranchStore,
} from '../branchStore';

describe('branchStore', () => {
  beforeEach(() => {
    useBranchStore.setState({ selections: {} });
  });

  it('returns DEFAULT_BRANCH when ontology is null/undefined', () => {
    expect(activeBranchFor(null)).toBe(DEFAULT_BRANCH);
    expect(activeBranchFor(undefined)).toBe(DEFAULT_BRANCH);
  });

  it('returns DEFAULT_BRANCH when ontology has no entry', () => {
    expect(activeBranchFor('foundry')).toBe(DEFAULT_BRANCH);
  });

  it('setBranch persists a non-default branch in selections', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    expect(activeBranchFor('foundry')).toBe('feature-x');
  });

  it('setBranch with DEFAULT_BRANCH removes the entry', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    useBranchStore.getState().setBranch('foundry', DEFAULT_BRANCH);
    expect(useBranchStore.getState().selections.foundry).toBeUndefined();
    expect(activeBranchFor('foundry')).toBe(DEFAULT_BRANCH);
  });

  it('setBranch with blank string normalises to default and clears entry', () => {
    useBranchStore.getState().setBranch('foundry', 'feature-x');
    useBranchStore.getState().setBranch('foundry', '   ');
    expect(useBranchStore.getState().selections.foundry).toBeUndefined();
  });

  it('clearBranch removes only the named entry', () => {
    useBranchStore.getState().setBranch('a', 'feat-a');
    useBranchStore.getState().setBranch('b', 'feat-b');
    useBranchStore.getState().clearBranch('a');
    expect(useBranchStore.getState().selections.a).toBeUndefined();
    expect(useBranchStore.getState().selections.b).toBe('feat-b');
  });

  it('selections are isolated per ontology', () => {
    useBranchStore.getState().setBranch('a', 'feat-a');
    useBranchStore.getState().setBranch('b', 'feat-b');
    expect(activeBranchFor('a')).toBe('feat-a');
    expect(activeBranchFor('b')).toBe('feat-b');
  });
});
