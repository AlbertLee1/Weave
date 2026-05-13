import { useQuery } from '@tanstack/react-query';
import { listDatasetHistory } from '../api/datasets';

// US-048: per-ontology dataset_transactions chain used to populate the
// Time Travel picker. Re-fetches every 60s so newly-recorded
// transactions show up without the user having to re-mount the page.
export function useDatasetHistory(ontologyRidOrApiName: string) {
  return useQuery({
    queryKey: ['datasetHistory', ontologyRidOrApiName],
    queryFn: () => listDatasetHistory(ontologyRidOrApiName),
    enabled: !!ontologyRidOrApiName,
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}
