import { request } from './client';
import type { Ontology, ObjectType, LinkType, ActionType } from './types';

export async function listOntologies(): Promise<Ontology[]> {
  const resp = await request<{ data: Ontology[] }>('GET', '/api/v2/ontologies');
  return resp.data;
}

export function getOntology(apiName: string): Promise<Ontology> {
  return request<Ontology>('GET', `/api/v2/ontologies/${apiName}`);
}

export async function listObjectTypes(ontologyApiName: string): Promise<ObjectType[]> {
  const resp = await request<{ data: ObjectType[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objectTypes`,
  );
  return resp.data;
}

export function getObjectType(
  ontologyApiName: string,
  objectTypeApiName: string,
): Promise<ObjectType> {
  return request<ObjectType>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objectTypes/${objectTypeApiName}`,
  );
}

export async function listOutgoingLinkTypes(
  ontologyApiName: string,
  objectTypeApiName: string,
): Promise<LinkType[]> {
  const resp = await request<{ data: LinkType[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objectTypes/${objectTypeApiName}/outgoingLinkTypes`,
  );
  return resp.data;
}

export async function listActionTypes(ontologyApiName: string): Promise<ActionType[]> {
  const resp = await request<{ data: ActionType[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/actionTypes`,
  );
  return resp.data;
}

export function getActionType(
  ontologyApiName: string,
  actionTypeRid: string,
): Promise<ActionType> {
  return request<ActionType>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/actionTypes/${actionTypeRid}`,
  );
}
