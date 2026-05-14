export type SaveMode = 'appendVersion' | 'inPlace' | 'noOp';

export interface SaveDecisionInput {
  versioned: boolean;
  hasChanges: boolean;
}

export function resolveSaveMode(input: SaveDecisionInput): SaveMode {
  if (!input.hasChanges) return 'noOp';
  return input.versioned ? 'appendVersion' : 'inPlace';
}
