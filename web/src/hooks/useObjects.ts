import { useQuery } from '@tanstack/react-query';
import {
  listObjects,
  searchObjects,
  getObject,
  listLinkedObjects,
  type ListObjectsParams,
  type SearchObjectsParams,
  type ListLinkedObjectsParams,
} from '../api/objects';

export function useListObjects(params: ListObjectsParams) {
  return useQuery({
    queryKey: ['objects', 'list', params],
    queryFn: () => listObjects(params),
    enabled: !!params.ontologyApiName && !!params.objectType,
  });
}

export function useSearchObjects(
  params: SearchObjectsParams & { enabled?: boolean },
) {
  const { enabled = true, ...searchParams } = params;
  return useQuery({
    queryKey: ['objects', 'search', searchParams],
    queryFn: () => searchObjects(searchParams),
    enabled: enabled && !!searchParams.ontologyApiName && !!searchParams.objectType,
  });
}

export function useGetObject(
  ontologyApiName: string,
  objectType: string,
  primaryKey: string,
) {
  return useQuery({
    queryKey: ['objects', 'get', ontologyApiName, objectType, primaryKey],
    queryFn: () => getObject({ ontologyApiName, objectType, primaryKey }),
    enabled: !!ontologyApiName && !!objectType && !!primaryKey,
  });
}

export function useLinkedObjects(params: ListLinkedObjectsParams) {
  return useQuery({
    queryKey: ['objects', 'linked', params],
    queryFn: () => listLinkedObjects(params),
    enabled:
      !!params.ontologyApiName &&
      !!params.objectType &&
      !!params.primaryKey &&
      !!params.linkType,
  });
}
