import { describe, it, expect } from 'vitest';
import { describeApiError } from '../describeError';
import { ApiRequestError } from '../client';

function makeApiError(
  errorName: string,
  parameters?: Record<string, string>,
): ApiRequestError {
  return new ApiRequestError({
    statusCode: 400,
    errorCode: 'INVALID_ARGUMENT',
    errorName,
    errorInstanceId: 'inst-1',
    parameters,
  });
}

describe('describeApiError', () => {
  it('formats ApiRequestError with a reason parameter as "<ErrorName>: <reason>"', () => {
    const err = makeApiError('WatchAlreadyExists', { reason: 'already following' });
    expect(describeApiError(err, 'Watch update failed.')).toBe(
      'WatchAlreadyExists: already following',
    );
  });

  it('falls back to the error parameter when no reason is present', () => {
    const err = makeApiError('PolicyConflict', { error: 'conflicting policy' });
    expect(describeApiError(err, 'Operation failed.')).toBe(
      'PolicyConflict: conflicting policy',
    );
  });

  it('prefers reason over error when both parameters are present', () => {
    const err = makeApiError('Boom', { reason: 'the reason', error: 'the error' });
    expect(describeApiError(err, 'fallback')).toBe('Boom: the reason');
  });

  it('returns just the errorName when ApiRequestError carries no reason/error', () => {
    const err = makeApiError('NotFound');
    expect(describeApiError(err, 'Watch update failed.')).toBe('NotFound');
  });

  it('returns the message for a plain Error', () => {
    expect(describeApiError(new Error('network down'), 'Operation failed.')).toBe(
      'network down',
    );
  });

  it('returns the supplied fallback for unknown non-Error values', () => {
    expect(describeApiError('boom', 'Reaction update failed.')).toBe(
      'Reaction update failed.',
    );
    expect(describeApiError(undefined, 'Mark-read update failed.')).toBe(
      'Mark-read update failed.',
    );
    expect(describeApiError(42, 'Operation failed.')).toBe('Operation failed.');
  });

  it('uses the default fallback when none is provided', () => {
    expect(describeApiError(null)).toBe('Something went wrong.');
  });
});
