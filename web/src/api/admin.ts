import { request } from './client';
import type { Ontology, ObjectType, Property, LinkType, ActionType, ActionLog, OntologyInterface, ObjectTypeInterface, OntologySnapshot, ValueType, SecurityPolicy, SharedProperty, TypeGroup, DatasourceBinding, QueryType } from './types';

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

// --- Value Type API ---

export interface CreateValueTypeInput {
  apiName: string;
  displayName: string;
  baseType: string;
  constraints?: unknown;
}

export function listValueTypes(): Promise<{ data: ValueType[] }> {
  return request<{ data: ValueType[] }>('GET', '/api/admin/value-types');
}

export function createValueType(input: CreateValueTypeInput): Promise<ValueType> {
  return request<ValueType>('POST', '/api/admin/value-types', input);
}

export function deleteValueType(rid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/value-types/${rid}`);
}

// --- Security Policy API ---

export interface CreateSecurityPolicyInput {
  policyType: 'OBJECT' | 'PROPERTY';
  rules: unknown;
}

export function listSecurityPolicies(objectTypeRid: string): Promise<{ data: SecurityPolicy[] }> {
  return request<{ data: SecurityPolicy[] }>(
    'GET',
    `/api/admin/objectTypes/${objectTypeRid}/securityPolicies`,
  );
}

export function createSecurityPolicy(
  objectTypeRid: string,
  input: CreateSecurityPolicyInput,
): Promise<SecurityPolicy> {
  return request<SecurityPolicy>(
    'POST',
    `/api/admin/objectTypes/${objectTypeRid}/securityPolicies`,
    input,
  );
}

export function deleteSecurityPolicy(rid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/securityPolicies/${rid}`);
}

// --- Shared Property API ---

export interface CreateSharedPropertyInput {
  apiName: string;
  displayName?: string;
  description?: string;
  baseType: string;
  typeConfig?: unknown;
  isArray?: boolean;
}

export function listSharedProperties(
  ontologyApiName: string,
): Promise<{ data: SharedProperty[] }> {
  return request<{ data: SharedProperty[] }>(
    'GET',
    `/api/admin/ontologies/${ontologyApiName}/shared-properties`,
  );
}

export function createSharedProperty(
  ontologyApiName: string,
  input: CreateSharedPropertyInput,
): Promise<SharedProperty> {
  return request<SharedProperty>(
    'POST',
    `/api/admin/ontologies/${ontologyApiName}/shared-properties`,
    input,
  );
}

export function updateSharedProperty(
  rid: string,
  input: Partial<CreateSharedPropertyInput>,
): Promise<SharedProperty> {
  return request<SharedProperty>(
    'PUT',
    `/api/admin/shared-properties/${rid}`,
    input,
  );
}

export function deleteSharedProperty(rid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/shared-properties/${rid}`);
}

// --- Type Group API ---

export interface CreateTypeGroupInput {
  apiName: string;
  displayName: string;
  description?: string;
  color?: string;
}

export function listTypeGroups(
  ontologyApiName: string,
): Promise<{ data: TypeGroup[] }> {
  return request<{ data: TypeGroup[] }>(
    'GET',
    `/api/admin/ontologies/${ontologyApiName}/type-groups`,
  );
}

export function createTypeGroup(
  ontologyApiName: string,
  input: CreateTypeGroupInput,
): Promise<TypeGroup> {
  return request<TypeGroup>(
    'POST',
    `/api/admin/ontologies/${ontologyApiName}/type-groups`,
    input,
  );
}

export function updateTypeGroup(
  rid: string,
  input: Partial<CreateTypeGroupInput>,
): Promise<TypeGroup> {
  return request<TypeGroup>('PUT', `/api/admin/type-groups/${rid}`, input);
}

export function deleteTypeGroup(rid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/type-groups/${rid}`);
}

export function assignTypeGroup(
  objectTypeRid: string,
  typeGroupRid: string,
): Promise<void> {
  return request<void>(
    'POST',
    `/api/admin/objectTypes/${objectTypeRid}/groups/${typeGroupRid}`,
  );
}

export function removeTypeGroup(
  objectTypeRid: string,
  typeGroupRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/admin/objectTypes/${objectTypeRid}/groups/${typeGroupRid}`,
  );
}

export function listTypeGroupsForObjectType(
  objectTypeRid: string,
): Promise<{ data: TypeGroup[] }> {
  return request<{ data: TypeGroup[] }>(
    'GET',
    `/api/admin/objectTypes/${objectTypeRid}/groups`,
  );
}

// --- Datasource Binding API ---

export interface CreateDatasourceBindingInput {
  datasetRid: string;
  branch: string;
  columnMapping?: unknown;
  isPrimary?: boolean;
}

export function listDatasourceBindings(
  objectTypeRid: string,
): Promise<{ data: DatasourceBinding[] }> {
  return request<{ data: DatasourceBinding[] }>(
    'GET',
    `/api/admin/objectTypes/${objectTypeRid}/datasourceBindings`,
  );
}

export function createDatasourceBinding(
  objectTypeRid: string,
  input: CreateDatasourceBindingInput,
): Promise<DatasourceBinding> {
  return request<DatasourceBinding>(
    'POST',
    `/api/admin/objectTypes/${objectTypeRid}/datasourceBindings`,
    input,
  );
}

export function updateDatasourceBinding(
  rid: string,
  input: Partial<CreateDatasourceBindingInput>,
): Promise<DatasourceBinding> {
  return request<DatasourceBinding>(
    'PUT',
    `/api/admin/datasourceBindings/${rid}`,
    input,
  );
}

export function deleteDatasourceBinding(rid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/datasourceBindings/${rid}`);
}

// --- Query Type API ---

export interface CreateQueryTypeInput {
  apiName: string;
  displayName: string;
  description?: string;
  parameters: unknown;
  output: unknown;
  query: unknown;
  status?: string;
}

export function listQueryTypes(
  ontologyApiName: string,
): Promise<{ data: QueryType[] }> {
  return request<{ data: QueryType[] }>(
    'GET',
    `/api/admin/ontologies/${ontologyApiName}/queryTypes`,
  );
}

export function createQueryType(
  ontologyApiName: string,
  input: CreateQueryTypeInput,
): Promise<QueryType> {
  return request<QueryType>(
    'POST',
    `/api/admin/ontologies/${ontologyApiName}/queryTypes`,
    input,
  );
}

export function updateQueryType(
  rid: string,
  input: Partial<CreateQueryTypeInput>,
): Promise<QueryType> {
  return request<QueryType>('PUT', `/api/admin/queryTypes/${rid}`, input);
}

export function deleteQueryType(rid: string): Promise<void> {
  return request<void>('DELETE', `/api/admin/queryTypes/${rid}`);
}

export function executeQueryType(
  ontologyApiName: string,
  queryApiName: string,
  parameters: Record<string, unknown>,
): Promise<unknown> {
  return request<unknown>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/queries/${queryApiName}/execute`,
    parameters,
  );
}
