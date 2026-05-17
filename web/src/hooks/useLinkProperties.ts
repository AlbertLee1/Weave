import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  listLinkProperties,
  putLinkEdgeProperties,
} from '../api/linkProperties';

// US-497 — read the LinkProperty schema for a given LinkType. Disabled
// when no linkTypeRid yet so callers can wire this hook to a derived
// value without guarding with extra conditionals.
export function useLinkProperties(
  ontologyApiName: string,
  linkTypeRid: string,
) {
  return useQuery({
    queryKey: ['linkProperties', ontologyApiName, linkTypeRid],
    queryFn: () => listLinkProperties(ontologyApiName, linkTypeRid),
    enabled: !!ontologyApiName && !!linkTypeRid,
  });
}

// US-497 — PUT replaces the whole edge_properties JSONB; on success we
// invalidate the matching linkedObjects query so the table re-renders
// with any enriched values once the backend list endpoint surfaces them
// (today it doesn't, but the invalidation is a no-op cost and keeps the
// hook honest for the day enrichment lands here).
export function useUpdateLinkEdgeProperties(
  ontologyApiName: string,
  linkTypeRid: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: {
      sourcePk: string;
      targetPk: string;
      values: Record<string, unknown>;
    }) =>
      putLinkEdgeProperties(
        ontologyApiName,
        linkTypeRid,
        vars.sourcePk,
        vars.targetPk,
        vars.values,
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['objects', 'linked'] });
    },
  });
}
