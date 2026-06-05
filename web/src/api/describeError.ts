import { ApiRequestError } from './client';

// describeApiError turns a failed request error into a short user-facing
// string for toast messages. For a structured ApiRequestError it reads
// "<ErrorName>: <reason>" when a reason/error parameter is present, otherwise
// just "<ErrorName>"; for any other Error it surfaces the raw message; and for
// non-Error values it returns the caller-supplied fallback.
//
// This is the shared form previously duplicated across ~7 components
// (WatchButton, ReactionBar, NotificationsPage, AutomationRulesPage,
// SagaJobsPage, ProposalsPage, SecurityPoliciesPage). Each call site passes
// its own fallback string so the behavior is byte-for-byte identical to the
// removed local helpers.
export function describeApiError(
  err: unknown,
  fallback = 'Something went wrong.',
): string {
  if (err instanceof ApiRequestError) {
    const reason = err.parameters?.reason ?? err.parameters?.error;
    return reason ? `${err.errorName}: ${reason}` : err.errorName;
  }
  if (err instanceof Error) return err.message;
  return fallback;
}
