import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createAutomationRule,
  deleteAutomationRule,
  listAutomationExecutions,
  listAutomationRules,
  pauseAutomationRule,
  resumeAutomationRule,
  updateAutomationRule,
  type CreateAutomationRuleRequest,
  type ListAutomationRulesParams,
  type ListExecutionsParams,
  type UpdateAutomationRuleRequest,
} from '../api/automationRules';

const RULES_KEY = (ontology: string) => ['automationRules', ontology] as const;
const EXECUTIONS_KEY = (ontology: string, ruleId: string) =>
  ['automationRules', ontology, ruleId, 'executions'] as const;

export function useAutomationRules(
  ontology: string,
  params: ListAutomationRulesParams = {},
) {
  return useQuery({
    queryKey: [...RULES_KEY(ontology), params.status ?? 'all'],
    queryFn: () => listAutomationRules(ontology, params),
    enabled: !!ontology,
  });
}

export function useAutomationExecutions(
  ontology: string,
  ruleId: string | null,
  params: ListExecutionsParams = {},
) {
  return useQuery({
    queryKey: ruleId
      ? [...EXECUTIONS_KEY(ontology, ruleId), params.status ?? 'all']
      : ['automationRules', ontology, '__nil__', 'executions'],
    queryFn: () => listAutomationExecutions(ontology, ruleId as string, params),
    enabled: !!ontology && !!ruleId,
  });
}

export function useCreateAutomationRule(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateAutomationRuleRequest) =>
      createAutomationRule(ontology, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: RULES_KEY(ontology) });
    },
  });
}

export function useUpdateAutomationRule(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      ruleId,
      body,
    }: {
      ruleId: string;
      body: UpdateAutomationRuleRequest;
    }) => updateAutomationRule(ontology, ruleId, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: RULES_KEY(ontology) });
    },
  });
}

export function useDeleteAutomationRule(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ruleId: string) => deleteAutomationRule(ontology, ruleId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: RULES_KEY(ontology) });
    },
  });
}

export function usePauseAutomationRule(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ruleId: string) => pauseAutomationRule(ontology, ruleId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: RULES_KEY(ontology) });
    },
  });
}

export function useResumeAutomationRule(ontology: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ruleId: string) => resumeAutomationRule(ontology, ruleId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: RULES_KEY(ontology) });
    },
  });
}
