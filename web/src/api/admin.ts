import { request } from './client';
import type { Ontology, ObjectType, Property, LinkType, ActionType, ActionLog, OntologyInterface, ObjectTypeInterface, OntologySnapshot } from './types';

export interface CreateOntologyInput {
  apiName: string;
  displayName: string;
  description?: string;
}

export function createOntology(input: CreateOntologyInput): Promise<Ontology> {
  return request<Ontology>('POST', '/api/admin/ontologies', input);
}

export interface CreateObjectTypeInput {
  apiName: string;
  displayName: string;
  pluralDisplayName?: string;
  description?: string;
  primaryKey: string;
  titleProperty?: string;
  status?: string;
  visibility?: string;
  icon?: string;
  color?: string;
}

export function createObjectType(
  ontologyApiName: string,
  input: CreateObjectTypeInput,
): Promise<ObjectType> {
  return request<ObjectType>(
    'POST',
    `/api/admin/ontologies/${ontologyApiName}/objectTypes`,
    input,
  );
}

export interface UpdateObjectTypeInput {
  displayName?: string;
  pluralDisplayName?: string;
  description?: string;
  titleProperty?: string;
  status?: string;
  visibility?: string;
  icon?: string;
  color?: string;
}

export function updateObjectType(
  objectTypeRid: string,
  input: UpdateObjectTypeInput,
): Promise<ObjectType> {
  return request<ObjectType>(
    'PUT',
    `/api/admin/objectTypes/${objectTypeRid}`,
    input,
  );
}

export function deleteObjectType(objectTypeRid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/objectTypes/${objectTypeRid}`);
}

export interface CreatePropertyInput {
  apiName: string;
  displayName?: string;
  description?: string;
  baseType: string;
  isArray?: boolean;
  isNullable?: boolean;
  isSearchable?: boolean;
  isSortable?: boolean;
}

export function createProperty(
  objectTypeRid: string,
  input: CreatePropertyInput,
): Promise<Property> {
  return request<Property>(
    'POST',
    `/api/admin/objectTypes/${objectTypeRid}/properties`,
    input,
  );
}

export function deleteProperty(propertyRid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/properties/${propertyRid}`);
}

export interface CreateLinkTypeInput {
  apiName: string;
  displayName: string;
  description?: string;
  objectTypeApiName: string;
  linkedObjectTypeApiName: string;
  cardinality: 'ONE_TO_ONE' | 'ONE_TO_MANY' | 'MANY_TO_MANY';
  isRequired?: boolean;
}

export function createLinkType(
  ontologyApiName: string,
  input: CreateLinkTypeInput,
): Promise<LinkType> {
  return request<LinkType>(
    'POST',
    `/api/admin/ontologies/${ontologyApiName}/linkTypes`,
    input,
  );
}

export interface CreateActionTypeInput {
  apiName: string;
  displayName: string;
  description?: string;
  status?: string;
  parameters?: unknown;
  rules?: unknown;
  submissionCriteria?: unknown;
  sideEffects?: unknown;
}

export function createActionType(
  ontologyApiName: string,
  input: CreateActionTypeInput,
): Promise<ActionType> {
  return request<ActionType>(
    'POST',
    `/api/admin/ontologies/${ontologyApiName}/actionTypes`,
    input,
  );
}

export function updateActionType(
  actionTypeRid: string,
  input: Partial<CreateActionTypeInput>,
): Promise<ActionType> {
  return request<ActionType>(
    'PUT',
    `/api/admin/actionTypes/${actionTypeRid}`,
    input,
  );
}

// --- Phase 1.2 additions ---

export interface UpdateOntologyInput {
  displayName?: string;
  description?: string;
}

export interface UpdatePropertyInput {
  displayName?: string;
  description?: string;
  isSearchable?: boolean;
  isSortable?: boolean;
  isNullable?: boolean;
}

export interface UpdateLinkTypeInput {
  displayName?: string;
  description?: string;
  required?: boolean;
}

export function updateOntology(
  ontologyRid: string,
  input: UpdateOntologyInput,
): Promise<Ontology> {
  return request<Ontology>('PUT', `/api/admin/ontologies/${ontologyRid}`, input);
}

export function updateProperty(
  propertyRid: string,
  input: UpdatePropertyInput,
): Promise<Property> {
  return request<Property>('PUT', `/api/admin/properties/${propertyRid}`, input);
}

export function updateLinkType(
  linkTypeRid: string,
  input: UpdateLinkTypeInput,
): Promise<LinkType> {
  return request<LinkType>('PUT', `/api/admin/linkTypes/${linkTypeRid}`, input);
}

export function deleteLinkType(linkTypeRid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/linkTypes/${linkTypeRid}`);
}

export function deleteActionType(actionTypeRid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/actionTypes/${actionTypeRid}`);
}

export function listAllLinkTypes(
  ontologyApiName: string,
): Promise<{ data: LinkType[] }> {
  return request<{ data: LinkType[] }>(
    'GET',
    `/api/admin/ontologies/${ontologyApiName}/linkTypes`,
  );
}

// --- Interface API ---

export interface CreateInterfaceInput {
  apiName: string;
  displayName: string;
  extendsRid?: string;
  sharedProperties?: unknown;
}

export interface UpdateInterfaceInput {
  displayName?: string;
  extendsRid?: string;
  sharedProperties?: unknown;
}

export interface AttachInterfaceInput {
  interfaceRid: string;
  propertyMapping?: Record<string, string>;
}

export function createInterface(ontologyApiName: string, input: CreateInterfaceInput): Promise<OntologyInterface> {
  return request<OntologyInterface>('POST', `/api/admin/ontologies/${ontologyApiName}/interfaces`, input);
}

export function listInterfaces(ontologyApiName: string): Promise<{ data: OntologyInterface[] }> {
  return request<{ data: OntologyInterface[] }>('GET', `/api/admin/ontologies/${ontologyApiName}/interfaces`);
}

export function updateInterface(interfaceRid: string, input: UpdateInterfaceInput): Promise<OntologyInterface> {
  return request<OntologyInterface>('PUT', `/api/admin/interfaces/${interfaceRid}`, input);
}

export function deleteInterface(interfaceRid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/interfaces/${interfaceRid}`);
}

export function attachInterface(objectTypeRid: string, input: AttachInterfaceInput): Promise<void> {
  return request<void>('POST', `/api/admin/objectTypes/${objectTypeRid}/interfaces`, input);
}

export function detachInterface(objectTypeRid: string, interfaceRid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/objectTypes/${objectTypeRid}/interfaces/${interfaceRid}`);
}

export function listObjectTypeInterfaces(objectTypeRid: string): Promise<{ data: ObjectTypeInterface[] }> {
  return request<{ data: ObjectTypeInterface[] }>('GET', `/api/admin/objectTypes/${objectTypeRid}/interfaces`);
}

// --- Action Log API ---

export function listActionLogs(
  actionTypeRid: string,
  limit = 50,
  offset = 0,
): Promise<{ data: ActionLog[]; total: number }> {
  return request<{ data: ActionLog[]; total: number }>(
    'GET',
    `/api/admin/actionTypes/${encodeURIComponent(actionTypeRid)}/logs?limit=${limit}&offset=${offset}`,
  );
}

// --- Snapshot API ---

export function createSnapshot(ontologyApiName: string): Promise<OntologySnapshot> {
  return request<OntologySnapshot>(
    'POST',
    `/api/admin/ontologies/${ontologyApiName}/snapshots`,
  );
}

export function listSnapshots(ontologyApiName: string): Promise<OntologySnapshot[]> {
  return request<OntologySnapshot[]>(
    'GET',
    `/api/admin/ontologies/${ontologyApiName}/snapshots`,
  );
}

export function getSnapshot(ontologyApiName: string, version: number): Promise<OntologySnapshot> {
  return request<OntologySnapshot>(
    'GET',
    `/api/admin/ontologies/${ontologyApiName}/snapshots/${version}`,
  );
}
