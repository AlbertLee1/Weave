import { describe, it, expect, beforeEach } from 'vitest';
import {
  activeAsOfFor,
  isTimeTravelActive,
  useTimeTravelStore,
} from '../timeTravelStore';

describe('timeTravelStore', () => {
  beforeEach(() => {
    useTimeTravelStore.setState({ selections: {} });
  });

  it('returns "" when ontology is null/undefined', () => {
    expect(activeAsOfFor(null)).toBe('');
    expect(activeAsOfFor(undefined)).toBe('');
  });

  it('returns "" when ontology has no entry', () => {
    expect(activeAsOfFor('foundry')).toBe('');
    expect(isTimeTravelActive('foundry')).toBe(false);
  });

  it('setAsOf persists a non-empty tx id in selections', () => {
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    expect(activeAsOfFor('foundry')).toBe('tx-abc');
    expect(isTimeTravelActive('foundry')).toBe(true);
  });

  it('setAsOf with blank string removes the entry', () => {
    useTimeTravelStore.getState().setAsOf('foundry', 'tx-abc');
    useTimeTravelStore.getState().setAsOf('foundry', '   ');
    expect(useTimeTravelStore.getState().selections.foundry).toBeUndefined();
    expect(activeAsOfFor('foundry')).toBe('');
  });

  it('clearAsOf removes only the named entry', () => {
    useTimeTravelStore.getState().setAsOf('a', 'tx-a');
    useTimeTravelStore.getState().setAsOf('b', 'tx-b');
    useTimeTravelStore.getState().clearAsOf('a');
    expect(useTimeTravelStore.getState().selections.a).toBeUndefined();
    expect(useTimeTravelStore.getState().selections.b).toBe('tx-b');
  });

  it('selections are isolated per ontology', () => {
    useTimeTravelStore.getState().setAsOf('a', 'tx-a');
    useTimeTravelStore.getState().setAsOf('b', 'tx-b');
    expect(activeAsOfFor('a')).toBe('tx-a');
    expect(activeAsOfFor('b')).toBe('tx-b');
  });

  it('trims whitespace around stored values', () => {
    useTimeTravelStore.getState().setAsOf('foundry', '  tx-padded  ');
    expect(activeAsOfFor('foundry')).toBe('tx-padded');
  });
});
