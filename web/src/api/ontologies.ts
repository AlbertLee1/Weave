import { ApiRequestError, request, withActiveBranch } from './client';
import { authedFetch } from '../auth/interceptor';
import type {
  Ontology,
  ObjectType,
  Property,
  LinkType,
  ActionType,
  ActionLog,
  OntologyInterface,
  InterfaceSharedProperty,
  InterfaceOutgoingLinkType,
  ObjectTypeInterface,
  ValueType,
  QueryType,
  BranchDiffEntry,
  BranchDiffPostResponse,
  MergeBranchRequest,
  MergeBranchResponse,
  MergeConflictBody,
  OntologyBranch,
  DatasourceBinding,
} from './types';

// US-499: wire shape for GET /api/v2/ontologies/{o}/objectTypes/{ot}/resolved.
// Mirrors pkg/oms.OMSHandler.GetObjectTypeResolved: parent properties +
// outgoing links are merged in with `inheritedFrom` provenance.
export interface ResolvedProperty {
  dataType: unknown;
  rid: string;
  displayName?: string;
  description?: string;
  inheritedFrom?: string;
}

export interface ResolvedOutgoingLink {
  apiName: string;
  displayName: string;
  rid: string;
  objectTypeApiName: string;
  linkedObjectTypeApiName: string;
  cardinality: string;
  required: boolean;
  description?: string;
  inheritedFrom?: string;
}

export interface ResolvedObjectType {
  apiName: string;
  displayName: string;
  status: string;
  primaryKey: string;
  primaryKeys?: string[];
  rid: string;
  visibility: string;
  pluralDisplayName?: string;
  description?: string;
  titleProperty?: string;
  extendsRid?: string;
  extendsChain?: string[];
  properties: Record<string, ResolvedProperty>;
  outgoingLinkTypes: ResolvedOutgoingLink[];
}

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

// US-499: GET the inheritance-resolved view of an ObjectType — parent
// properties + outgoing links merged in with `inheritedFrom` provenance.
export function getResolvedObjectType(
  ontologyApiName: string,
  objectTypeApiName: string,
): Promise<ResolvedObjectType> {
  return request<ResolvedObjectType>(
    'GET',
    `/api/v2/ontologies/${ontologyApiName}/objectTypes/${objectTypeApiName}/resolved`,
  );
}

// US-499: POST the ObjectType edit history (action_logs scoped to this OT).
// Backend (pkg/oms.PostObjectTypeEditsHistoryV2) returns logs in
// repository-default order — the UI is responsible for the time-descending
// presentation contract the PRD requires.
export async function postObjectTypeEditsHistory(
  ontologyApiName: string,
  objectTypeApiName: string,
): Promise<ActionLog[]> {
  const resp = await request<{ data: ActionLog[] }>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/objectTypes/${objectTypeApiName}/editsHistory`,
    {},
  );
  return resp.data ?? [];
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
  // VTX-010: Vertex graph rendering tags. Omit or pass [] when no tags.
  typeClasses?: string[];
}

export interface UpdateLinkTypeRequest {
  displayName: string;
  description?: string;
  required?: boolean;
  // VTX-010: tri-state — undefined leaves tags untouched, [] clears them,
  // a non-empty array replaces them. Mirrors the backend tri-state pointer.
  typeClasses?: string[];
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
  // US-211: composite primary key. When supplied the backend prefers
  // primaryKeys over primaryKey; senders pass it only when more than one
  // field is specified, leaving primaryKey as the single-key fallback.
  primaryKeys?: string[];
  titleProperty?: string;
  status?: 'ACTIVE' | 'ENDORSED' | 'EXPERIMENTAL' | 'DEPRECATED';
  visibility?: 'PROMINENT' | 'NORMAL' | 'HIDDEN';
  classification?: string;
  // US-212: RID of a parent ObjectType to inherit from (same ontology).
  extendsRid?: string;
  // US-264: opt this ObjectType into per-read data-access audit logging.
  auditDataAccess?: boolean;
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
  // US-212 tri-state: omit = preserve, '' = clear the parent pointer,
  // non-empty RID = validate + assign a parent ObjectType.
  extendsRid?: string;
  // US-264 tri-state: omit = preserve, explicit bool = toggle the per-read
  // data-access audit flag.
  auditDataAccess?: boolean;
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

// --- ValueType admin endpoints (US-051 / PC-A05) ---

export interface CreateValueTypeRequest {
  apiName: string;
  displayName: string;
  baseType: string;
  constraints?: Record<string, unknown>;
}

export interface UpdateValueTypeRequest {
  displayName: string;
  baseType: string;
  constraints?: Record<string, unknown>;
}

export interface ValueTypeUsage {
  propertyRid: string;
  propertyApiName: string;
  objectTypeRid: string;
  objectTypeApiName: string;
}

export async function listValueTypesAdmin(
  ontologyApiName: string,
): Promise<ValueType[]> {
  const resp = await request<{ data: ValueType[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/valueTypesAdmin`,
  );
  return resp.data;
}

export function createValueType(
  ontologyApiName: string,
  body: CreateValueTypeRequest,
): Promise<ValueType> {
  return request<ValueType>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/valueTypes`,
    body,
  );
}

export function updateValueType(
  ontologyApiName: string,
  valueTypeRid: string,
  body: UpdateValueTypeRequest,
): Promise<ValueType> {
  return request<ValueType>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/valueTypes/byRid/${encodeURIComponent(valueTypeRid)}`,
    body,
  );
}

export function deleteValueType(
  ontologyApiName: string,
  valueTypeRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/valueTypes/byRid/${encodeURIComponent(valueTypeRid)}`,
  );
}

export async function listValueTypeUsages(
  ontologyApiName: string,
  valueTypeRid: string,
): Promise<ValueTypeUsage[]> {
  const resp = await request<{ data: ValueTypeUsage[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/valueTypes/byRid/${encodeURIComponent(valueTypeRid)}/usages`,
  );
  return resp.data;
}

// --- DatasourceBinding admin endpoints (US-052 / PC-A06) ---
//
// The backend stores a JSON object for column_mapping (apiName → upstream
// column name). The UI deals with the parsed object form; the wire keeps
// it nestable for future shape evolution (e.g. ingest-time transforms).

export interface CreateDatasourceBindingRequest {
  datasetRid: string;
  branch?: string;
  columnMapping?: Record<string, string>;
  isPrimary?: boolean;
}

export interface UpdateDatasourceBindingRequest {
  datasetRid: string;
  branch?: string;
  columnMapping?: Record<string, string>;
  isPrimary?: boolean;
}

export async function listDatasourceBindings(
  ontologyApiName: string,
  objectTypeRid: string,
): Promise<DatasourceBinding[]> {
  const resp = await request<{ data: DatasourceBinding[] }>(
    'GET',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}/datasourceBindings`,
  );
  return resp.data;
}

export function createDatasourceBinding(
  ontologyApiName: string,
  objectTypeRid: string,
  body: CreateDatasourceBindingRequest,
): Promise<DatasourceBinding> {
  return request<DatasourceBinding>(
    'POST',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/objectTypes/byRid/${encodeURIComponent(objectTypeRid)}/datasourceBindings`,
    body,
  );
}

export function updateDatasourceBinding(
  ontologyApiName: string,
  bindingRid: string,
  body: UpdateDatasourceBindingRequest,
): Promise<DatasourceBinding> {
  return request<DatasourceBinding>(
    'PUT',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/datasourceBindings/byRid/${encodeURIComponent(bindingRid)}`,
    body,
  );
}

export function deleteDatasourceBinding(
  ontologyApiName: string,
  bindingRid: string,
): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/ontologies/${encodeURIComponent(ontologyApiName)}/datasourceBindings/byRid/${encodeURIComponent(bindingRid)}`,
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

// executeQueryType POSTs to the Foundry-style execute endpoint. The handler
// nests user-supplied params under a `parameters` key (see
// pkg/oms/admin_handlers.go::ExecuteQueryType). Returns the raw map the
// handler emits — either a Foundry function result (`{ value: ... }`) or the
// fallback metadata payload for QueryTypes that have no FunctionRID wired.
export function executeQueryType(
  ontologyApiName: string,
  queryApiName: string,
  parameters: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/queries/${queryApiName}/execute`,
    { parameters },
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

// US-385 / US-387: categorised diff with conflict annotations.
export async function postBranchDiff(
  ontologyApiName: string,
  branchId: string,
): Promise<BranchDiffPostResponse> {
  return request<BranchDiffPostResponse>(
    'POST',
    `/api/v2/ontologies/${ontologyApiName}/branches/${branchId}/diff`,
  );
}

// MergeBranchConflictError carries the 409 body so callers can render the
// unresolved conflict list without a re-fetch.
export class MergeBranchConflictError extends Error {
  conflicts: MergeConflictBody['conflicts'];
  unresolved: MergeConflictBody['unresolved'];
  constructor(body: MergeConflictBody) {
    super('MERGE_CONFLICT');
    this.name = 'MergeBranchConflictError';
    this.conflicts = body.conflicts ?? [];
    this.unresolved = body.unresolved ?? [];
  }
}

// US-385 / US-387: direct merge with explicit conflict resolution. The
// 409 body is non-standard (`{errorCode, conflicts, unresolved}`) so we
// branch on status before delegating to the standard request() helper.
export async function mergeBranch(
  ontologyApiName: string,
  branchId: string,
  body: MergeBranchRequest,
): Promise<MergeBranchResponse> {
  const path = withActiveBranch(
    `/api/v2/ontologies/${ontologyApiName}/branches/${branchId}/merge`,
  );
  const resp = await authedFetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const text = await resp.text();
  const parsed = text ? JSON.parse(text) : {};
  if (resp.status === 409 && parsed?.errorCode === 'MERGE_CONFLICT') {
    throw new MergeBranchConflictError(parsed as MergeConflictBody);
  }
  if (!resp.ok) {
    throw new ApiRequestError({
      errorCode: parsed.errorCode ?? 'UNKNOWN',
      errorName: parsed.errorName ?? resp.statusText,
      errorInstanceId: parsed.errorInstanceId ?? '',
      parameters: parsed.parameters,
      statusCode: resp.status,
    });
  }
  return parsed as MergeBranchResponse;
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
