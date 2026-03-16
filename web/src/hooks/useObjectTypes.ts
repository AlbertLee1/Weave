import { useQuery } from '@tanstack/react-query';
import {
  listObjectTypes,
  getObjectType,
  listOutgoingLinkTypes,
} from '../api/ontologies';

export function useObjectTypes(ontologyApiName: string) {
  return useQuery({
    queryKey: ['objectTypes', ontologyApiName],
    queryFn: () => listObjectTypes(ontologyApiName),
    enabled: !!ontologyApiName,
  });
}

export function useObjectType(
  ontologyApiName: string,
  objectTypeApiName: string,
) {
  return useQuery({
    queryKey: ['objectTypes', ontologyApiName, objectTypeApiName],
    queryFn: () => getObjectType(ontologyApiName, objectTypeApiName),
    enabled: !!ontologyApiName && !!objectTypeApiName,
  });
}

export function useOutgoingLinkTypes(
  ontologyApiName: string,
  objectTypeApiName: string,
) {
  return useQuery({
    queryKey: ['linkTypes', ontologyApiName, objectTypeApiName],
    queryFn: () => listOutgoingLinkTypes(ontologyApiName, objectTypeApiName),
    enabled: !!ontologyApiName && !!objectTypeApiName,
  });
}
