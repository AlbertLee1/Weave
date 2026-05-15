import { describe, it, expect } from 'vitest';
import { resolveSaveMode, type SaveMode } from './versionedToggle';

describe('VTX-086 resolveSaveMode', () => {
  it('given_VersionedEnabled_when_HasChanges_then_AppendNewVersion', () => {
    const got: SaveMode = resolveSaveMode({ versioned: true, hasChanges: true });
    expect(got).toBe('appendVersion');
  });

  it('given_VersionedDisabled_when_HasChanges_then_InPlace', () => {
    expect(resolveSaveMode({ versioned: false, hasChanges: true })).toBe('inPlace');
  });

  it('given_NoChanges_when_VersionedEnabled_then_NoOp', () => {
    expect(resolveSaveMode({ versioned: true, hasChanges: false })).toBe('noOp');
  });

  it('given_NoChanges_when_VersionedDisabled_then_NoOp', () => {
    expect(resolveSaveMode({ versioned: false, hasChanges: false })).toBe('noOp');
  });
});
