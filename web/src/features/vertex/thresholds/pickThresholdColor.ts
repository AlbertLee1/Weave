export type ThresholdOperator = '>' | '>=' | '<' | '<=' | '==';

export interface Threshold {
  operator: ThresholdOperator;
  value: number;
  color: string;
}

export function pickThresholdColor(
  value: number,
  thresholds: Threshold[],
): string | null {
  if (!Number.isFinite(value)) return null;
  for (const t of thresholds) {
    if (matches(value, t)) return t.color;
  }
  return null;
}

function matches(value: number, t: Threshold): boolean {
  switch (t.operator) {
    case '>':
      return value > t.value;
    case '>=':
      return value >= t.value;
    case '<':
      return value < t.value;
    case '<=':
      return value <= t.value;
    case '==':
      return value === t.value;
  }
}
