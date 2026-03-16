import { describe, it, expect } from 'vitest';
import { formatBaseType, formatStatus, truncate, formatCount } from '../formatters';

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
