import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import {
  listObjects,
  searchObjects,
  getObject,
  listLinkedObjects,
  getObjectActivity,
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

// US-312: per-object activity timeline. Cursor-paginated via the server's
// opaque nextPageToken; `pageSize` is fixed at the call site so React
// Query can dedupe identically-shaped queries.
export function useObjectActivity(params: {
  ontologyApiName: string;
  objectType: string;
  primaryKey: string;
  pageSize?: number;
}) {
  const { ontologyApiName, objectType, primaryKey, pageSize = 50 } = params;
  const enabled = Boolean(ontologyApiName && objectType && primaryKey);
  return useInfiniteQuery({
    queryKey: [
      'objects',
      'activity',
      ontologyApiName,
      objectType,
      primaryKey,
      pageSize,
    ],
    queryFn: ({ pageParam }) =>
      getObjectActivity({
        ontologyApiName,
        objectType,
        primaryKey,
        pageSize,
        pageToken: pageParam || undefined,
      }),
    initialPageParam: '',
    getNextPageParam: (last) => last.nextPageToken || undefined,
    enabled,
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
