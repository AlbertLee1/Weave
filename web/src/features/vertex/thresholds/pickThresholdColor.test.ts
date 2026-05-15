import { describe, it, expect } from 'vitest';
import {
  pickThresholdColor,
  type Threshold,
} from './pickThresholdColor';

describe('VTX-078 pickThresholdColor', () => {
  const thresholds: Threshold[] = [
    { operator: '>', value: 100, color: 'red' },
    { operator: '>', value: 50, color: 'yellow' },
  ];

  it('given_ValueAboveTopThreshold_then_TopColor', () => {
    expect(pickThresholdColor(150, thresholds)).toBe('red');
  });

  it('given_ValueAtThreshold_when_OperatorIsGreaterThan_then_NotMatched', () => {
    // 100 is NOT > 100, so first threshold misses; 100 > 50 → yellow.
    expect(pickThresholdColor(100, thresholds)).toBe('yellow');
  });

  it('given_ValueAt100_when_GreaterEq_then_TopMatches', () => {
    const ts: Threshold[] = [
      { operator: '>=', value: 100, color: 'red' },
      { operator: '>=', value: 50, color: 'yellow' },
    ];
    expect(pickThresholdColor(100, ts)).toBe('red');
  });

  it('given_ValueBelowAll_then_Null', () => {
    expect(pickThresholdColor(10, thresholds)).toBeNull();
  });

  it('given_EmptyThresholds_then_Null', () => {
    expect(pickThresholdColor(999, [])).toBeNull();
  });

  it('given_LessThanOperator_when_ValueBelow_then_Match', () => {
    const ts: Threshold[] = [
      { operator: '<', value: 0, color: 'blue' },
    ];
    expect(pickThresholdColor(-5, ts)).toBe('blue');
    expect(pickThresholdColor(0, ts)).toBeNull();
  });

  it('given_EqualsOperator_when_ValueMatches_then_Match', () => {
    const ts: Threshold[] = [
      { operator: '==', value: 42, color: 'purple' },
    ];
    expect(pickThresholdColor(42, ts)).toBe('purple');
    expect(pickThresholdColor(41, ts)).toBeNull();
  });

  it('given_NonNumericValue_then_Null', () => {
    expect(pickThresholdColor(NaN, thresholds)).toBeNull();
    expect(pickThresholdColor(Infinity, thresholds)).toBeNull();
  });

  it('given_PriorityRespected_when_FirstMatches_then_FirstColor', () => {
    // Priority follows array order — first matching threshold wins.
    const ts: Threshold[] = [
      { operator: '>=', value: 50, color: 'yellow' },
      { operator: '>=', value: 100, color: 'red' },
    ];
    expect(pickThresholdColor(150, ts)).toBe('yellow');
  });
});
