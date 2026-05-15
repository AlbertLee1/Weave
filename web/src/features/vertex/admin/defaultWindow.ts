export interface DefaultWindowInput {
  defaultWindowDays: number;
  now: number;
}

export interface TimeWindowSpan {
  from: number;
  to: number;
}

const DAY_MS = 24 * 3600 * 1000;

export function computeDefaultTimeWindow(input: DefaultWindowInput): TimeWindowSpan {
  if (input.defaultWindowDays < 0) {
    throw new Error('computeDefaultTimeWindow: defaultWindowDays must be ≥ 0');
  }
  return {
    from: input.now - input.defaultWindowDays * DAY_MS,
    to: input.now,
  };
}
