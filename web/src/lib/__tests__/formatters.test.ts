import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  formatBaseType,
  formatStatus,
  truncate,
  formatCount,
  formatRelativeTime,
} from '../formatters';

describe('formatBaseType', () => {
  it('capitalizes known types', () => {
    expect(formatBaseType('string')).toBe('String');
    expect(formatBaseType('integer')).toBe('Integer');
    expect(formatBaseType('boolean')).toBe('Boolean');
    expect(formatBaseType('timestamp')).toBe('Timestamp');
  });

  it('returns unknown types as-is', () => {
    expect(formatBaseType('custom')).toBe('custom');
  });
});

describe('formatStatus', () => {
  it('formats status to title case', () => {
    expect(formatStatus('ACTIVE')).toBe('Active');
    expect(formatStatus('DEPRECATED')).toBe('Deprecated');
    expect(formatStatus('EXPERIMENTAL')).toBe('Experimental');
  });
});

describe('truncate', () => {
  it('returns short text unchanged', () => {
    expect(truncate('hello', 10)).toBe('hello');
  });

  it('truncates long text with ellipsis', () => {
    expect(truncate('hello world', 8)).toBe('hello w\u2026');
  });

  it('returns text at exact length unchanged', () => {
    expect(truncate('hello', 5)).toBe('hello');
  });
});

describe('formatCount', () => {
  it('formats numbers with locale', () => {
    expect(formatCount(1000)).toBe('1,000');
  });

  it('formats string numbers', () => {
    expect(formatCount('42')).toBe('42');
  });

  it('returns dash for undefined', () => {
    expect(formatCount(undefined)).toBe('-');
  });

  it('returns dash for NaN', () => {
    expect(formatCount('not-a-number')).toBe('-');
  });
});

describe('formatRelativeTime', () => {
  const NOW = new Date('2026-04-18T12:00:00Z');

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns "just now" for seconds-old timestamps', () => {
    expect(formatRelativeTime(new Date(NOW.getTime() - 10_000).toISOString())).toBe(
      'just now',
    );
  });

  it('returns minutes for sub-hour timestamps', () => {
    expect(formatRelativeTime(new Date(NOW.getTime() - 5 * 60_000).toISOString())).toBe(
      '5m ago',
    );
  });

  it('returns hours for sub-day timestamps', () => {
    expect(
      formatRelativeTime(new Date(NOW.getTime() - 3 * 60 * 60_000).toISOString()),
    ).toBe('3h ago');
  });

  it('returns days for sub-week timestamps', () => {
    expect(
      formatRelativeTime(
        new Date(NOW.getTime() - 4 * 24 * 60 * 60_000).toISOString(),
      ),
    ).toBe('4d ago');
  });

  it('falls back to locale date for older timestamps', () => {
    const date = new Date(NOW.getTime() - 30 * 24 * 60 * 60_000);
    expect(formatRelativeTime(date.toISOString())).toBe(date.toLocaleDateString());
  });

  it('returns empty string for missing input', () => {
    expect(formatRelativeTime(undefined)).toBe('');
    expect(formatRelativeTime('')).toBe('');
  });

  it('returns empty string for invalid date strings', () => {
    expect(formatRelativeTime('not-a-date')).toBe('');
  });
});
