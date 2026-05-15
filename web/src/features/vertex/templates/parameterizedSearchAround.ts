export interface ParameterizedSearchAroundInput {
  searchAroundFnRid: string;
  objectRids: string[];
  sharedParams?: Record<string, unknown>;
}

export interface SearchAroundCall {
  functionRid: string;
  objectRid: string;
  params: Record<string, unknown>;
}

export function planParameterizedSearchArounds(
  input: ParameterizedSearchAroundInput,
): SearchAroundCall[] {
  if (!input.searchAroundFnRid) {
    throw new Error('planParameterizedSearchArounds: searchAroundFnRid required');
  }
  const params = input.sharedParams ?? {};
  const seen = new Set<string>();
  const plan: SearchAroundCall[] = [];
  for (const rid of input.objectRids) {
    if (seen.has(rid)) continue;
    seen.add(rid);
    plan.push({
      functionRid: input.searchAroundFnRid,
      objectRid: rid,
      params,
    });
  }
  return plan;
}
