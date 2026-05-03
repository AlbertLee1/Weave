import { request } from './client';
import type {
  Ontology,
  ObjectType,
  Property,
  LinkType,
  ActionType,
  OntologyInterface,
  InterfaceSharedProperty,
  InterfaceOutgoingLinkType,
  ObjectTypeInterface,
  ValueType,
  QueryType,
  BranchDiffEntry,
  OntologyBranch,
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

// --- LinkType admin endpoints (US-148) ---

export type Cardinality = 'ONE_TO_ONE' | 'ONE_TO_MANY' | 'MANY_TO_MANY';

export interface CreateLinkTypeRequest {
  apiName: string;
  displayName: string;
  description?: string;
  objectTypeApiName: string;
  linkedObjectTypeApiName: string;
  cardinality: Cardinality;
  foreignKeyConfig?: unknown;
  joinTableConfig?: unknown;
  required?: boolean;
}

export interface UpdateLinkTypeRequest {
  displayName: string;
  description?: string;
  required?: boolean;
}

export async function listLinkTypes(
  ontologyApiName: string,
): Promise<LinkType[]> {
  const resp = await request<{ data: LinkType[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/linkTypes`,
  );
  return resp.data;
}

export function createLinkType(
  ontologyApiName: string,
  body: CreateLinkTypeRequest,
): Promise<LinkType> {
  return request<LinkType>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/linkTypes`,
    body,
  );
}

export function updateLinkType(
  ontologyApiName: string,
  linkTypeRid: string,
  body: UpdateLinkTypeRequest,
): Promise<LinkType> {
  return request<LinkType>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/linkTypes/byRid/${encodeURIComponent(linkTypeRid)}`,
    body,
  );
}

export function deleteLinkType(
  ontologyApiName: string,
  linkTypeRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/linkTypes/byRid/${encodeURIComponent(linkTypeRid)}`,
  );
}

export interface CreateObjectTypeRequest {
  apiName: string;
  displayName: string;
  pluralDisplayName?: string;
  description?: string;
  primaryKey: string;
  titleProperty?: string;
  status?: 'ACTIVE' | 'ENDORSED' | 'EXPERIMENTAL' | 'DEPRECATED';
  visibility?: 'PROMINENT' | 'NORMAL' | 'HIDDEN';
  classification?: string;
}

export interface UpdateObjectTypeRequest {
  displayName: string;
  pluralDisplayName?: string;
  description?: string;
  titleProperty?: string;
  status: 'ACTIVE' | 'ENDORSED' | 'EXPERIMENTAL' | 'DEPRECATED';
  visibility: 'PROMINENT' | 'NORMAL' | 'HIDDEN';
  iconName?: string;
  color?: string;
  deprecatedReason?: string;
  deprecatedDeadline?: string | null;
  // US-262 tri-state: omit = preserve, '' = clear, known label = assign.
  classification?: string;
}

export function createObjectType(
  ontologyApiName: string,
  body: CreateObjectTypeRequest,
): Promise<ObjectType> {
  return request<ObjectType>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes`,
    body,
  );
}

export function updateObjectType(
  ontologyApiName: string,
  objectTypeRid: string,
  body: UpdateObjectTypeRequest,
): Promise<ObjectType> {
  return request<ObjectType>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}`,
    body,
  );
}

export function deleteObjectType(
  ontologyApiName: string,
  objectTypeRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}`,
  );
}

// --- Property admin endpoints (US-147) ---

export interface CreatePropertyRequest {
  apiName: string;
  displayName?: string;
  description?: string;
  baseType: string;
  typeConfig?: unknown;
  isArray?: boolean;
  isNullable?: boolean;
  isSearchable?: boolean;
  isSortable?: boolean;
  editOnly?: boolean;
  classification?: string;
}

export interface UpdatePropertyRequest {
  displayName?: string;
  description?: string;
  isSearchable?: boolean;
  isSortable?: boolean;
  isNullable?: boolean;
  status?: string;
  deprecatedReason?: string;
  editOnly?: boolean;
  // US-262 tri-state: omit = preserve, '' = clear, known label = assign.
  classification?: string;
}

export async function listProperties(
  ontologyApiName: string,
  objectTypeRid: string,
): Promise<Property[]> {
  const resp = await request<{ data: Property[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}/properties`,
  );
  return resp.data;
}

export function createProperty(
  ontologyApiName: string,
  objectTypeRid: string,
  body: CreatePropertyRequest,
): Promise<Property> {
  return request<Property>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}/properties`,
    body,
  );
}

export function updateProperty(
  ontologyApiName: string,
  propertyRid: string,
  body: UpdatePropertyRequest,
): Promise<Property> {
  return request<Property>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/properties/byRid/${encodeURIComponent(propertyRid)}`,
    body,
  );
}

export function deleteProperty(
  ontologyApiName: string,
  propertyRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/properties/byRid/${encodeURIComponent(propertyRid)}`,
  );
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

// --- ActionType admin endpoints (US-149) ---

// Internal (stored) parameter definition: array element shape the backend
// persists in ActionType.Parameters JSONB. The admin builder emits this
// shape directly (the V2 wire format is read-only).
export interface ActionTypeParamDef {
  id: string;
  type: string;
  required?: boolean;
  description?: string;
}

// Union of rule shapes supported by pkg/actions/rules.go. Each rule is
// identified by its `type` and carries type-specific fields.
export type ActionTypeRuleType =
  | 'createObject'
  | 'modifyObject'
  | 'deleteObject'
  | 'createLink'
  | 'deleteLink'
  | 'createOrModifyObject'
  | 'createInterfaceObject'
  | 'modifyInterfaceObject'
  | 'deleteInterfaceObject';

export interface ActionTypeRule {
  type: ActionTypeRuleType;
  objectType?: string;
  interfaceApiName?: string;
  linkTypeApiName?: string;
  primaryKey?: string;
  sourceObjectPrimaryKey?: string;
  targetObjectPrimaryKey?: string;
  propertyBindings?: Record<string, unknown>;
}

export interface CreateActionTypeRequest {
  apiName: string;
  displayName: string;
  description?: string;
  status?: string;
  parameters: ActionTypeParamDef[];
  rules: ActionTypeRule[];
}

export interface UpdateActionTypeRequest {
  displayName: string;
  description?: string;
  status: string;
  parameters: ActionTypeParamDef[];
  rules: ActionTypeRule[];
  submissionCriteria?: unknown;
  sideEffects?: unknown;
}

export async function listActionTypesAdmin(
  ontologyApiName: string,
): Promise<ActionType[]> {
  const resp = await request<{ data: ActionType[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actionTypesAdmin`,
  );
  return resp.data;
}

export function createActionType(
  ontologyApiName: string,
  body: CreateActionTypeRequest,
): Promise<ActionType> {
  return request<ActionType>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actionTypes`,
    body,
  );
}

export function updateActionType(
  ontologyApiName: string,
  actionTypeRid: string,
  body: UpdateActionTypeRequest,
): Promise<ActionType> {
  return request<ActionType>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actionTypes/byRid/${encodeURIComponent(actionTypeRid)}`,
    body,
  );
}

export function deleteActionType(
  ontologyApiName: string,
  actionTypeRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/actionTypes/byRid/${encodeURIComponent(actionTypeRid)}`,
  );
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

// --- Interface admin endpoints (US-150) ---

export interface CreateInterfaceRequest {
  apiName: string;
  displayName: string;
  description?: string;
  extendsRid?: string;
  sharedProperties?: InterfaceSharedProperty[];
  outgoingLinkTypes?: InterfaceOutgoingLinkType[];
}

export interface UpdateInterfaceRequest {
  displayName: string;
  extendsRid?: string;
  sharedProperties?: InterfaceSharedProperty[];
  outgoingLinkTypes?: InterfaceOutgoingLinkType[];
}

export async function listInterfacesAdmin(
  ontologyApiName: string,
): Promise<OntologyInterface[]> {
  const resp = await request<{ data: OntologyInterface[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/interfacesAdmin`,
  );
  return resp.data;
}

export function createInterface(
  ontologyApiName: string,
  body: CreateInterfaceRequest,
): Promise<OntologyInterface> {
  return request<OntologyInterface>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/interfaces`,
    body,
  );
}

export function updateInterface(
  ontologyApiName: string,
  interfaceRid: string,
  body: UpdateInterfaceRequest,
): Promise<OntologyInterface> {
  return request<OntologyInterface>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/interfaces/byRid/${encodeURIComponent(interfaceRid)}`,
    body,
  );
}

export function deleteInterface(
  ontologyApiName: string,
  interfaceRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/interfaces/byRid/${encodeURIComponent(interfaceRid)}`,
  );
}

export async function listObjectTypeInterfaces(
  ontologyApiName: string,
  objectTypeRid: string,
): Promise<ObjectTypeInterface[]> {
  const resp = await request<{ data: ObjectTypeInterface[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}/interfaces`,
  );
  return resp.data;
}

export function attachInterfaceToObjectType(
  ontologyApiName: string,
  objectTypeRid: string,
  body: { interfaceRid: string; propertyMapping?: Record<string, string> },
): Promise<ObjectTypeInterface> {
  return request<ObjectTypeInterface>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}/interfaces`,
    body,
  );
}

export function detachInterfaceFromObjectType(
  ontologyApiName: string,
  objectTypeRid: string,
  interfaceRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}/interfaces/${encodeURIComponent(interfaceRid)}`,
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

// --- Branch endpoints ---

export async function listBranches(
  ontologyApiName: string,
): Promise<OntologyBranch[]> {
  const resp = await request<{ data: OntologyBranch[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/branches`,
  );
  return resp.data ?? [];
}

export async function getBranchDiff(
  ontologyApiName: string,
  branchId: string,
): Promise<BranchDiffEntry[]> {
  const resp = await request<{ data: BranchDiffEntry[] }>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/branches/${branchId}/diff`,
  );
  return resp.data;
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
