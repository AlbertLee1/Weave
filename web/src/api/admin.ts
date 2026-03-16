import { request } from './client';
import type { Ontology, ObjectType, Property, LinkType, ActionType } from './types';

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
  sourceObjectType: string;
  targetObjectType: string;
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
