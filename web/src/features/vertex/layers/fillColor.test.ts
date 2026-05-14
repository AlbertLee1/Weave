import { describe, it, expect } from 'vitest';
import { computeFillColor, type FillColorConfig } from './fillColor';

describe('VTX-064 computeFillColor by property + rainbow scale', () => {
  const cfg: FillColorConfig = {
    by: 'property',
    property: 'alertScore',
    scale: 'rainbow',
    domain: [0, 100],
  };

  it('given_RainbowScale_when_ValueAtMin_then_ReturnsStartHue', () => {
    const c = computeFillColor({ alertScore: 0 }, cfg);
    expect(c).toMatch(/^hsl\(/);
    expect(c).toBe('hsl(0, 80%, 50%)');
  });

  it('given_RainbowScale_when_ValueAtMax_then_ReturnsEndHue', () => {
    const c = computeFillColor({ alertScore: 100 }, cfg);
    expect(c).toBe('hsl(300, 80%, 50%)');
  });

  it('given_RainbowScale_when_ValueAtMid_then_ReturnsMidHue', () => {
    const c = computeFillColor({ alertScore: 50 }, cfg);
    expect(c).toBe('hsl(150, 80%, 50%)');
  });

  it('given_RainbowScale_when_PropertyMissing_then_ReturnsFallbackGray', () => {
    const c = computeFillColor({}, cfg);
    expect(c).toBe('#9CA3AF');
  });

  it('given_RainbowScale_when_ValueBelowDomain_then_ClampsToStart', () => {
    const c = computeFillColor({ alertScore: -10 }, cfg);
    expect(c).toBe('hsl(0, 80%, 50%)');
  });

  it('given_RainbowScale_when_ValueAboveDomain_then_ClampsToEnd', () => {
    const c = computeFillColor({ alertScore: 999 }, cfg);
    expect(c).toBe('hsl(300, 80%, 50%)');
  });
});

describe('VTX-064 computeFillColor by property + threshold scale', () => {
  const cfg: FillColorConfig = {
    by: 'property',
    property: 'alertScore',
    scale: 'threshold',
    thresholds: [
      { lt: 10, color: '#22C55E' },
      { lt: 50, color: '#EAB308' },
      { lt: 100, color: '#EF4444' },
    ],
  };

  it('given_ThresholdScale_when_ValueBelowFirst_then_ReturnsGreen', () => {
    expect(computeFillColor({ alertScore: 5 }, cfg)).toBe('#22C55E');
  });

  it('given_ThresholdScale_when_ValueInMid_then_ReturnsYellow', () => {
    expect(computeFillColor({ alertScore: 30 }, cfg)).toBe('#EAB308');
  });

  it('given_ThresholdScale_when_ValueInHigh_then_ReturnsRed', () => {
    expect(computeFillColor({ alertScore: 80 }, cfg)).toBe('#EF4444');
  });

  it('given_ThresholdScale_when_ValueAboveAll_then_ReturnsLastColor', () => {
    expect(computeFillColor({ alertScore: 200 }, cfg)).toBe('#EF4444');
  });

  it('given_ThresholdScale_when_PropertyMissing_then_ReturnsFallback', () => {
    expect(computeFillColor({}, cfg)).toBe('#9CA3AF');
  });
});

describe('VTX-064 computeFillColor by static color', () => {
  it('given_StaticConfig_then_ReturnsLiteralColor', () => {
    const c = computeFillColor({ alertScore: 999 }, {
      by: 'static',
      color: '#3FB36F',
    });
    expect(c).toBe('#3FB36F');
  });
});
