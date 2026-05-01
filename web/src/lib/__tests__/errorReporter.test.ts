import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { reportError, setErrorReporter } from '../errorReporter';

describe('errorReporter', () => {
  let consoleSpy: ReturnType<typeof vi.spyOn>;
  beforeEach(() => {
    consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
  });
  afterEach(() => {
    consoleSpy.mockRestore();
    setErrorReporter(null);
    delete (window as unknown as { __WEAVE_ERROR_REPORT_URL__?: string }).__WEAVE_ERROR_REPORT_URL__;
  });

  it('always logs to console.error', () => {
    reportError(new Error('boom'));
    expect(consoleSpy).toHaveBeenCalled();
  });

  it('coerces non-Error values to a payload Error', () => {
    const reporter = vi.fn();
    setErrorReporter(reporter);
    reportError('plain-string-error');
    expect(reporter).toHaveBeenCalledWith(
      expect.objectContaining({ message: 'plain-string-error' }),
    );
  });

  it('passes through componentStack to the custom reporter', () => {
    const reporter = vi.fn();
    setErrorReporter(reporter);
    reportError(new Error('x'), '\n  at Component\n  at App');
    expect(reporter).toHaveBeenCalledWith(
      expect.objectContaining({
        message: 'x',
        componentStack: '\n  at Component\n  at App',
      }),
    );
  });

  it('a custom reporter that throws does not escalate', () => {
    setErrorReporter(() => {
      throw new Error('reporter-internal-failure');
    });
    expect(() => reportError(new Error('x'))).not.toThrow();
  });

  it('POSTs to __WEAVE_ERROR_REPORT_URL__ via sendBeacon when set', () => {
    const beacon = vi.fn().mockReturnValue(true);
    (navigator as unknown as { sendBeacon: typeof beacon }).sendBeacon = beacon;
    (window as unknown as { __WEAVE_ERROR_REPORT_URL__?: string }).__WEAVE_ERROR_REPORT_URL__ =
      '/api/client-errors';

    reportError(new Error('beam'));
    expect(beacon).toHaveBeenCalledWith('/api/client-errors', expect.any(Blob));
  });

  it('clearing the reporter (null) skips the custom reporter', () => {
    const reporter = vi.fn();
    setErrorReporter(reporter);
    setErrorReporter(null);
    reportError(new Error('x'));
    expect(reporter).not.toHaveBeenCalled();
  });
});
