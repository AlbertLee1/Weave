import { describe, expect, it, vi } from 'vitest';

describe('BDD: shared jsdom browser primitive shims', () => {
  it('provides quiet canvas and window.open primitives for web CI', () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      const canvas = document.createElement('canvas');
      const ctx = canvas.getContext('2d');

      expect(ctx).toBeTruthy();
      expect(typeof ctx?.measureText).toBe('function');
      expect(ctx?.measureText('OSv2').width).toBeGreaterThan(0);

      expect(globalThis.Path2D).toBeTypeOf('function');
      const path = new Path2D();
      expect(() => {
        path.moveTo(0, 0);
        path.lineTo(10, 10);
        path.closePath();
      }).not.toThrow();

      expect(() => window.open('/vertex/new', '_blank')).not.toThrow();
      expect(consoleError).not.toHaveBeenCalledWith(
        expect.stringMatching(/not implemented/i),
      );
    } finally {
      consoleError.mockRestore();
    }
  });
});
