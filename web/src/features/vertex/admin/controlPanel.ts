export interface ControlPanelConfig {
  defaultWindowDays: number;
  pollingIntervalSec: number;
  searchAroundMaxNodes: number;
  searchAroundMaxDepth: number;
  missingDataWarningHours: number;
  activeIconCategories: string[];
}

export type ConfigValidation =
  | { valid: true }
  | { valid: false; errors: Record<string, string> };

export function defaultControlPanelConfig(): ControlPanelConfig {
  return {
    defaultWindowDays: 7,
    pollingIntervalSec: 10,
    searchAroundMaxNodes: 200,
    searchAroundMaxDepth: 3,
    missingDataWarningHours: 24,
    activeIconCategories: [],
  };
}

export function validateControlPanelConfig(c: ControlPanelConfig): ConfigValidation {
  const errors: Record<string, string> = {};
  if (!Number.isInteger(c.defaultWindowDays) || c.defaultWindowDays <= 0) {
    errors.defaultWindowDays = 'defaultWindowDays must be a positive integer';
  }
  if (!Number.isFinite(c.pollingIntervalSec) || c.pollingIntervalSec < 1) {
    errors.pollingIntervalSec = 'pollingIntervalSec must be at least 1';
  }
  if (
    !Number.isInteger(c.searchAroundMaxNodes) ||
    c.searchAroundMaxNodes <= 0 ||
    c.searchAroundMaxNodes > 10_000
  ) {
    errors.searchAroundMaxNodes = 'searchAroundMaxNodes must be in (0, 10000]';
  }
  if (
    !Number.isInteger(c.searchAroundMaxDepth) ||
    c.searchAroundMaxDepth <= 0 ||
    c.searchAroundMaxDepth > 10
  ) {
    errors.searchAroundMaxDepth = 'searchAroundMaxDepth must be in (0, 10]';
  }
  if (!Number.isFinite(c.missingDataWarningHours) || c.missingDataWarningHours < 0) {
    errors.missingDataWarningHours = 'missingDataWarningHours must be ≥ 0';
  }
  if (Object.keys(errors).length === 0) return { valid: true };
  return { valid: false, errors };
}

export interface ViewerContext {
  roles?: string[];
}

export function isAdmin(ctx: ViewerContext): boolean {
  return (ctx.roles ?? []).includes('admin');
}
