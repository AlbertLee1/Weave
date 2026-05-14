import { describe, it, expect } from 'vitest';
import {
  validateControlPanelConfig,
  defaultControlPanelConfig,
  isAdmin,
  type ControlPanelConfig,
} from './controlPanel';

const validConfig: ControlPanelConfig = {
  defaultWindowDays: 30,
  pollingIntervalSec: 10,
  searchAroundMaxNodes: 200,
  searchAroundMaxDepth: 3,
  missingDataWarningHours: 24,
  activeIconCategories: ['aviation'],
};

describe('VTX-094 validateControlPanelConfig', () => {
  it('given_ValidConfig_when_Validate_then_Valid', () => {
    expect(validateControlPanelConfig(validConfig).valid).toBe(true);
  });

  it('given_NegativeWindowDays_then_Invalid', () => {
    const r = validateControlPanelConfig({ ...validConfig, defaultWindowDays: -1 });
    if (r.valid) throw new Error('expected invalid');
    expect(r.errors.defaultWindowDays).toBeDefined();
  });

  it('given_PollingTooLow_then_Invalid', () => {
    // Polling < 1s would hammer the server; reject.
    const r = validateControlPanelConfig({ ...validConfig, pollingIntervalSec: 0 });
    if (r.valid) throw new Error('expected invalid');
    expect(r.errors.pollingIntervalSec).toBeDefined();
  });

  it('given_MaxNodesAbove10k_then_Invalid', () => {
    const r = validateControlPanelConfig({ ...validConfig, searchAroundMaxNodes: 99999 });
    if (r.valid) throw new Error('expected invalid');
    expect(r.errors.searchAroundMaxNodes).toBeDefined();
  });

  it('given_MaxDepthAbove10_then_Invalid', () => {
    const r = validateControlPanelConfig({ ...validConfig, searchAroundMaxDepth: 99 });
    if (r.valid) throw new Error('expected invalid');
    expect(r.errors.searchAroundMaxDepth).toBeDefined();
  });

  it('given_EmptyIconCategories_then_Valid', () => {
    // An empty icon-category list is a valid choice (no constraints).
    expect(validateControlPanelConfig({ ...validConfig, activeIconCategories: [] }).valid).toBe(true);
  });
});

describe('VTX-094 defaultControlPanelConfig', () => {
  it('returns sensible defaults', () => {
    const d = defaultControlPanelConfig();
    expect(d.defaultWindowDays).toBe(7);
    expect(d.pollingIntervalSec).toBeGreaterThanOrEqual(5);
    expect(d.searchAroundMaxNodes).toBe(200);
    expect(d.searchAroundMaxDepth).toBe(3);
    expect(d.missingDataWarningHours).toBe(24);
    expect(d.activeIconCategories).toEqual([]);
  });
});

describe('VTX-094 isAdmin', () => {
  it('given_AdminRole_when_Check_then_True', () => {
    expect(isAdmin({ roles: ['admin'] })).toBe(true);
  });
  it('given_NoAdminRole_when_Check_then_False', () => {
    expect(isAdmin({ roles: ['user', 'editor'] })).toBe(false);
  });
  it('given_NoRolesField_when_Check_then_False', () => {
    expect(isAdmin({})).toBe(false);
  });
});
