import { describe, it, expect, vi, afterEach } from 'vitest';
import { registerServiceWorker } from '../serviceWorker';

describe('registerServiceWorker (US-354)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    if ('serviceWorker' in navigator) {
      delete (navigator as { serviceWorker?: unknown }).serviceWorker;
    }
  });

  it('is a no-op when navigator.serviceWorker is missing', () => {
    expect(() => registerServiceWorker()).not.toThrow();
  });

  it('does NOT register when running in dev mode', () => {
    const register = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: { register },
    });
    // The Vitest config sets DEV truthy; the helper should bail.
    registerServiceWorker();
    // Force the load event in case the load listener was registered
    // (it should not have been when DEV is truthy).
    window.dispatchEvent(new Event('load'));
    expect(register).not.toHaveBeenCalled();
  });
});
