import { describe, expect, it } from 'vitest';
import cssSource from '../index.css?raw';

/**
 * a11y — WCAG 2.3.3 Animation from Interactions.
 *
 * Global CSS must honor `prefers-reduced-motion: reduce` as a site-wide
 * fallback, neutralizing all animations/transitions for vestibular-disorder
 * users who enabled "reduce motion" at the OS level.
 *
 * NOTE: This is a CSS-only change. jsdom cannot evaluate media-query
 * rendering behavior, so we assert on the source file contents (loaded via
 * Vite's `?raw` import, the existing codebase convention) to guarantee the
 * rule exists. The rule only takes effect when the user has reduced motion
 * enabled, so normal environments/tests are unaffected.
 */
describe('prefers-reduced-motion global fallback', () => {
  it('declares a prefers-reduced-motion: reduce media query', () => {
    expect(cssSource).toContain('@media (prefers-reduced-motion: reduce)');
  });

  it('neutralizes transition durations under reduced motion', () => {
    expect(cssSource).toContain('transition-duration: 0.01ms');
  });

  it('neutralizes animation durations under reduced motion', () => {
    expect(cssSource).toContain('animation-duration: 0.01ms');
  });
});
