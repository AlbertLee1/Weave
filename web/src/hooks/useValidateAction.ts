import { useMutation } from '@tanstack/react-query';
import { validateAction } from '../api/actions';
import type { ValidateActionResponse } from '../api/actions';

// useValidateAction wraps the dedicated, side-effect-free
// POST /api/v2/ontologies/{ontology}/actions/{action}/validate surface as a
// mutation. It is intentionally a mutation (not a query) because validation
// is driven by user input on demand (debounced keystrokes), not by a stable
// cache key — and because the endpoint never mutates server state, there is
// nothing to invalidate on success.
//
// The endpoint returns HTTP 200 even when the draft is INVALID (the envelope
// is a validation report), so `mutateAsync` resolves with a
// ValidateActionResponse to inspect; it only rejects on transport / non-2xx
// errors. Callers should branch on `response.result`, never on a thrown error
// for "invalid".
export interface ValidateActionVariables {
  action: string;
  parameters: Record<string, unknown>;
}

export function useValidateAction(ontologyApiName: string) {
  return useMutation<ValidateActionResponse, Error, ValidateActionVariables>({
    mutationFn: (vars) =>
      validateAction(ontologyApiName, vars.action, vars.parameters),
  });
}
