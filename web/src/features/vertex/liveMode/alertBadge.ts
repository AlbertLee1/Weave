export interface AlertableNode {
  id: string;
  properties: Record<string, unknown>;
}

export interface AlertConfig {
  property: string;
  threshold: number;
}

export interface LiveModeFlags {
  liveMode: boolean;
  paused?: boolean;
}

export interface AlertResult {
  alerted: Set<string>;
}

export function isAlerted(node: AlertableNode, cfg: AlertConfig): boolean {
  const v = node.properties[cfg.property];
  if (typeof v !== 'number' || !Number.isFinite(v)) return false;
  return v > cfg.threshold;
}

export function computeAlertedNodes(
  nodes: AlertableNode[],
  cfg: AlertConfig,
  flags: LiveModeFlags,
): AlertResult {
  const alerted = new Set<string>();
  if (!flags.liveMode || flags.paused) return { alerted };
  for (const n of nodes) {
    if (isAlerted(n, cfg)) alerted.add(n.id);
  }
  return { alerted };
}
