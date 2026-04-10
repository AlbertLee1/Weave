import { request } from './client';
import type {
  Ontology,
  ObjectType,
  LinkType,
  ActionType,
  OntologyInterface,
  ValueType,
  QueryType,
} from './types';

// --- Ontology endpoints ---

export async function listOntologies(): Promise<Ontology[]> {
  const resp = await request<{ data: Ontology[] }>('GET', '/api/v2/ontologies');
  return resp.data;
}

export function getOntology(apiName: string): Promise<Ontology> {
  return request<Ontology>('GET', `/api/v2/ontologies/${apiName}`);
}

export function loadOntologyMetadata(
  ontologyApiName: string,
  subsets: Record<string, boolean>,
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/metadata`,
    subsets,
  );
}

export function getOntologyFullMetadata(
  ontologyApiName: string,
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/fullMetadata?preview=true`,
  );
}

// --- ObjectType endpoints ---

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

export function getObjectTypeFullMetadata(
  ontologyApiName: string,
  objectTypeApiName: string,
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objectTypes/${objectTypeApiName}/fullMetadata?preview=true`,
  );
}

export async function getObjectTypesByRidBatch(
  ontologyApiName: string,
  rids: string[],
): Promise<Record<string, unknown>[]> {
  const resp = await request<{ data: Record<string, unknown>[] }>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objectTypes/getByRidBatch`,
    { rids },
  );
  return resp.data;
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

// --- ActionType endpoints ---

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

export function getActionTypeByRid(
  ontologyApiName: string,
  actionTypeRid: string,
): Promise<ActionType> {
  return request<ActionType>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/actionTypes/byRid/${actionTypeRid}`,
  );
}

export async function getActionTypesByRidBatch(
  ontologyApiName: string,
  rids: string[],
): Promise<Record<string, unknown>[]> {
  const resp = await request<{ data: Record<string, unknown>[] }>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/actionTypes/getByRidBatch`,
    { rids },
  );
  return resp.data;
}

export function getActionTypeFullMetadata(
  ontologyApiName: string,
  actionType: string,
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/actionTypes/${actionType}/fullMetadata?preview=true`,
  );
}

export async function listActionTypesFullMetadata(
  ontologyApiName: string,
): Promise<Record<string, unknown>[]> {
  const resp = await request<{ data: Record<string, unknown>[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/actionTypesFullMetadata?preview=true`,
  );
  return resp.data;
}

// --- InterfaceType endpoints ---

export async function listInterfaceTypes(
  ontologyApiName: string,
): Promise<OntologyInterface[]> {
  const resp = await request<{ data: OntologyInterface[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/interfaceTypes?preview=true`,
  );
  return resp.data;
}

export function getInterfaceType(
  ontologyApiName: string,
  interfaceType: string,
): Promise<OntologyInterface> {
  return request<OntologyInterface>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/interfaceTypes/${interfaceType}`,
  );
}

// --- ValueType endpoints ---

export async function listValueTypes(
  ontologyApiName: string,
): Promise<ValueType[]> {
  const resp = await request<{ data: ValueType[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/valueTypes?preview=true`,
  );
  return resp.data;
}

export function getValueType(
  ontologyApiName: string,
  valueType: string,
): Promise<ValueType> {
  return request<ValueType>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/valueTypes/${valueType}`,
  );
}

// --- QueryType endpoints ---

export async function listQueryTypes(
  ontologyApiName: string,
): Promise<QueryType[]> {
  const resp = await request<{ data: QueryType[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/queryTypes`,
  );
  return resp.data;
}

export function getQueryType(
  ontologyApiName: string,
  queryType: string,
): Promise<QueryType> {
  return request<QueryType>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/queryTypes/${queryType}`,
  );
}

// --- Query execution ---

export function executeQuery(
  ontologyApiName: string,
  queryApiName: string,
  parameters?: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/queries/${queryApiName}/execute`,
    { parameters: parameters ?? {} },
  );
}
