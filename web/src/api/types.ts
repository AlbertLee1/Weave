// Mirrors Go models from pkg/oms/models.go and pkg/oss/wire.go

export interface Ontology {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
}

export interface ObjectType {
  rid: string;
  apiName: string;
  displayName: string;
  pluralDisplayName?: string;
  description?: string;
  primaryKey: string;
  titleProperty?: string;
  status: 'ACTIVE' | 'ENDORSED' | 'EXPERIMENTAL' | 'DEPRECATED';
  visibility: 'PROMINENT' | 'NORMAL' | 'HIDDEN';
  icon?: string;
  color?: string;
  classification?: Classification;
  properties?: Record<string, { dataType: DataType; rid: string }>;
}

// US-262: data-classification vocabulary (mirrors pkg/oms.KnownClassifications).
export const CLASSIFICATION_VALUES = [
  'Public',
  'Internal',
  'Confidential',
  'PII',
  'Secret',
] as const;
export type Classification = (typeof CLASSIFICATION_VALUES)[number];

export interface DataType {
  type: string;
  itemType?: DataType;
}

export interface Property {
  rid: string;
  apiName: string;
  displayName?: string;
  description?: string;
  baseType: string;
  typeConfig?: unknown;
  isArray: boolean;
  isNullable: boolean;
  isSearchable: boolean;
  isSortable: boolean;
  status?: string;
  deprecatedReason?: string;
  editOnly?: boolean;
  classification?: Classification;
}

export interface LinkType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  objectTypeApiName: string;
  linkedObjectTypeApiName: string;
  cardinality: 'ONE_TO_ONE' | 'ONE_TO_MANY' | 'MANY_TO_MANY';
  required: boolean;
  // VTX-010: Vertex graph rendering tags. See
  // web/src/features/vertex/links/edgeArrowStyle.ts for the recognised
  // values. Omitted on the wire when empty.
  typeClasses?: string[];
}

// ActionParameterV2 — Foundry OSv2 parameter definition with nested dataType.
export interface ActionParameterV2 {
  dataType: DataType;
  required: boolean;
  description?: string;
}

export interface ActionType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  status: string;
  parameters: Record<string, ActionParameterV2>;
  rules?: unknown;
  submissionCriteria?: unknown;
  sideEffects?: unknown;
}

export interface ActionLog {
  id: number;
  actionTypeRid: string;
  userId: string;
  parameters: unknown;
  edits: unknown;
  status: string;
  errorMessage?: string;
  createdAt: string;
}

// Tier 2.3: object change history.
export interface ObjectHistoryEntry {
  id: string;
  objectTypeRid: string;
  primaryKey: string;
  version: number;
  prevState?: Record<string, unknown> | null;
  newState?: Record<string, unknown> | null;
  editType: 'CREATE' | 'MODIFY' | 'DELETE';
  actionLogRid?: string;
  userId?: string;
  recordedAt: string;
}

export interface ObjectHistoryResponse {
  history: ObjectHistoryEntry[];
  totalVersions: number;
}

// US-312: per-object activity timeline. The wire shape mirrors the Go
// oms.ObjectHistory record one-to-one; pagination is cursor-based on the
// monotonically increasing per-PK `version` column.
export interface ObjectActivityEntry {
  id: string;
  objectTypeRid: string;
  primaryKey: string;
  version: number;
  prevState?: Record<string, unknown> | null;
  newState?: Record<string, unknown> | null;
  editType: 'CREATE' | 'MODIFY' | 'DELETE';
  source?: string;
  actionLogRid?: string;
  userId?: string;
  recordedAt: string;
}

export interface ObjectActivityResponse {
  data: ObjectActivityEntry[];
  nextPageToken?: string;
}

export interface WireObject {
  __rid: string;
  __primaryKey: string | number;
  __apiName: string;
  [property: string]: unknown;
}

export interface FacetBucket {
  value: string;
  count: number;
}

export interface ObjectPage {
  data: WireObject[];
  nextPageToken?: string;
  totalCount?: string;
  facets?: Record<string, FacetBucket[]>;
}

export interface AggregationRequest {
  aggregation: AggregationMetric[];
  groupBy?: GroupByClause[];
  where?: WhereClause;
  objectType?: string;
}

export interface AggregationMetric {
  type: 'min' | 'max' | 'sum' | 'avg' | 'count';
  field?: string;
  name?: string;
}

export interface GroupByClause {
  field: string;
  type: 'exact' | 'ranges' | 'fixedWidth';
}

export interface WhereClause {
  type: string;
  field?: string;
  value: unknown;
}

// Foundry OSv2 action apply options (mode + returnEdits).
export interface ActionApplyOptions {
  mode?: 'VALIDATE_ONLY' | 'VALIDATE_AND_EXECUTE';
  returnEdits?: 'ALL' | 'ALL_V2_WITH_DELETIONS' | 'NONE';
  // Optimistic concurrency: server compares against current object version
  // and returns 409 StaleObject if they diverge. See US-023/US-024.
  expectedVersion?: number;
}

// ActionApplyRequest is the Foundry OSv2 body shape for
// POST /api/v2/ontologies/{ontology}/actions/{action}/apply — the action
// API name lives in the URL, not the body, so this interface carries
// only the parameter payload.
export interface ActionApplyRequest {
  parameters: Record<string, unknown>;
  options?: ActionApplyOptions;
}

// ActionBatchApplyRequest is the body shape for applyBatch.
export interface ActionBatchApplyRequest {
  requests: Array<{ parameters: Record<string, unknown> }>;
  options?: { returnEdits?: 'ALL' | 'NONE' };
}

// BatchApplyActionResponse — Foundry OSv2 response envelope for batch apply.
export interface BatchApplyActionResponse {
  edits?: ActionResults;
}

// CountObjectsResponse — response for object count endpoint.
export interface CountObjectsResponse {
  count: number;
}

// SyncApplyActionResponseV2 — Foundry OSv2 response envelope for single apply.
// US-319: actionLogId surfaces the persisted action_logs row id so the toast
// Undo button can call POST /actions/revert with the right id during its
// 5-second window.
export interface ActionApplyResponse {
  operationId?: string;
  actionLogId?: number;
  validation?: { result: string };
  edits?: ActionResults;
}

// ActionResults — Foundry OSv2 edit summary (counts, not individual edits).
export interface ActionResults {
  type: 'edits';
  addedObjectCount: number;
  modifiedObjectCount: number;
  deletedObjectCount: number;
  addedLinksCount: number;
  deletedLinksCount: number;
}

export interface ApiError {
  errorCode: string;
  errorName: string;
  errorInstanceId: string;
  parameters?: Record<string, string>;
  statusCode: number;
}

export interface InterfaceSharedProperty {
  apiName: string;
  displayName?: string;
  description?: string;
  baseType: string;
  isArray?: boolean;
}

export interface InterfaceOutgoingLinkType {
  apiName: string;
  displayName: string;
  linkedEntityTypeApiName: string;
  linkedEntityTypeRid?: string;
  cardinality: 'ONE' | 'MANY';
  required?: boolean;
  description?: string;
}

export interface OntologyInterface {
  rid: string;
  apiName: string;
  displayName: string;
  extendsRid?: string;
  sharedProperties?: InterfaceSharedProperty[] | null;
  outgoingLinkTypes?: InterfaceOutgoingLinkType[] | null;
}

export interface ObjectTypeInterface {
  objectTypeRid: string;
  interfaceRid: string;
  propertyMapping: Record<string, string>;
}

export interface OntologySnapshot {
  id: number;
  ontologyRid: string;
  version: number;
  data: unknown;
  createdAt: string;
  createdBy?: string;
}

export interface ValueType {
  rid: string;
  apiName: string;
  displayName: string;
  baseType: string;
  constraints?: unknown;
  version: number;
}

export interface SecurityPolicy {
  rid: string;
  objectTypeRid: string;
  policyType: 'OBJECT' | 'PROPERTY';
  rules: unknown;
}

export interface SharedProperty {
  rid: string;
  apiName: string;
  displayName?: string;
  description?: string;
  baseType: string;
  typeConfig?: unknown;
  isArray: boolean;
}

export interface TypeGroup {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  color?: string;
}

export interface DatasourceBinding {
  rid: string;
  objectTypeRid?: string;
  datasetRid: string;
  branch: string;
  columnMapping?: unknown;
  isPrimary: boolean;
}

// QueryTypeParameter mirrors the persisted JSON-encoded element of
// pkg/oms.QueryType.Parameters (a flat array of definitions). The runtime
// shape is loose because the storage column is `JSONB`; UIs that need to
// render forms should coerce via parseQueryTypeParameters().
export interface QueryTypeParameter {
  id: string;
  type: 'string' | 'integer' | 'double' | 'boolean' | 'array' | string;
  required?: boolean;
  description?: string;
}

export interface QueryType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  parameters: unknown;
  output: unknown;
  query: unknown;
  status: string;
}

// --- ObjectSet Definition (mirrors pkg/oss/objectset/definition.go) ---

export type ObjectSetDefinition =
  | BaseObjectSet
  | StaticObjectSet
  | FilterObjectSet
  | UnionObjectSet
  | IntersectObjectSet
  | SubtractObjectSet
  | SearchAroundObjectSet
  | ReferenceObjectSet
  | WithPropertiesObjectSet
  | NearestNeighborsObjectSet
  | AsTypeObjectSet
  | AsBaseObjectTypesObjectSet
  | InterfaceBaseObjectSet
  | InterfaceLinkSearchAroundObjectSet
  | MethodInputObjectSet;

export interface BaseObjectSet {
  type: 'base';
  objectType: string;
}

export interface FilterObjectSet {
  type: 'filter';
  objectSet: ObjectSetDefinition;
  where: WhereClause;
}

export interface UnionObjectSet {
  type: 'union';
  objectSets: ObjectSetDefinition[];
}

export interface IntersectObjectSet {
  type: 'intersect';
  objectSets: ObjectSetDefinition[];
}

export interface SubtractObjectSet {
  type: 'subtract';
  objectSets: ObjectSetDefinition[];
}

export interface SearchAroundObjectSet {
  type: 'searchAround';
  objectSet: ObjectSetDefinition;
  link: string;
  direction?: 'forward' | 'reverse';
}

export interface ReferenceObjectSet {
  type: 'reference';
  reference: string;
}

export interface DerivedPropertyDef {
  name: string;
  link: string;
  direction?: 'forward' | 'reverse';
  metric: 'count' | 'sum' | 'avg' | 'min' | 'max';
  field?: string;
}

export interface WithPropertiesObjectSet {
  type: 'withProperties';
  objectSet: ObjectSetDefinition;
  properties?: string[];
  derivedProperties?: DerivedPropertyDef[];
}

export interface NearestNeighborsObjectSet {
  type: 'nearestNeighbors';
  objectSet: ObjectSetDefinition;
  propertyIdentifier?: { property: { apiName: string } };
  numNeighbors?: number;
  similarityThreshold?: number;
  query?: {
    vector?: { value: number[] };
    text?: { value: string };
  };
}

export interface StaticObjectSet {
  type: 'static';
  objectType: string;
  primaryKeys: string[];
}

export interface AsTypeObjectSet {
  type: 'asType';
  objectType: string;
  objectSet: ObjectSetDefinition;
}

export interface AsBaseObjectTypesObjectSet {
  type: 'asBaseObjectTypes';
  objectSet: ObjectSetDefinition;
}

export interface InterfaceBaseObjectSet {
  type: 'interfaceBase';
  interfaceType: string;
}

export interface InterfaceLinkSearchAroundObjectSet {
  type: 'interfaceLinkSearchAround';
  objectSet: ObjectSetDefinition;
  interfaceLink: string;
}

export interface MethodInputObjectSet {
  type: 'methodInput';
  input: string;
}

export interface OrderByField {
  field: string;
  direction: 'asc' | 'desc';
}

export interface OrderBy {
  fields: OrderByField[];
}

export interface LoadObjectSetRequest {
  objectSet: ObjectSetDefinition;
  select: string[];
  orderBy?: OrderBy;
  pageSize?: number;
  pageToken?: string;
  snapshot?: boolean;
}

export interface LoadObjectSetResponse {
  data: WireObject[];
  nextPageToken?: string;
  totalCount?: string;
}

export interface CreateTemporaryResponse {
  objectSetRid: string;
}

// --- Branch Diff (mirrors pkg/oms/handlers_branch.go BranchDiffEntry) ---

export interface BranchDiffEntry {
  entityType: string;
  entityRid: string;
  changeType: 'ADDED' | 'MODIFIED' | 'DELETED';
  before: Record<string, unknown> | null;
  after: Record<string, unknown> | null;
}

// --- Ontology Branch (mirrors pkg/oms/models.go OntologyBranch) ---

export interface OntologyBranch {
  id: string;
  ontologyRid: string;
  name: string;
  baseVersion: number;
  parentBranchId?: string;
  baseTx?: string;
  status: 'open' | 'merged' | 'closed';
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

// --- US-385 / US-387 Branch Reconcile (mirrors pkg/oms/handlers_branch_merge.go) ---

export interface AnnotatedDiffEntry {
  entityType: string;
  entityRid: string;
  apiName: string;
  resolutionKey: string;
  changeType: 'ADDED' | 'MODIFIED' | 'DELETED';
  hasConflict: boolean;
  before?: Record<string, unknown> | null;
  after?: Record<string, unknown> | null;
}

export interface AnnotatedMergeConflict {
  entityType: string;
  entityRid: string;
  apiName: string;
  resolutionKey: string;
  changeType: 'ADDED' | 'MODIFIED' | 'DELETED';
  branchState?: Record<string, unknown> | null;
  mainState?: Record<string, unknown> | null;
}

export interface BranchDiffPostResponse {
  branch: OntologyBranch;
  added: AnnotatedDiffEntry[];
  modified: AnnotatedDiffEntry[];
  deleted: AnnotatedDiffEntry[];
  conflicts: AnnotatedMergeConflict[];
  hasConflicts: boolean;
}

export type ConflictResolutionChoice = 'use-branch' | 'use-main';

export interface MergeBranchRequest {
  conflictResolution?: Record<string, ConflictResolutionChoice>;
}

export interface MergeBranchResponse {
  branch: OntologyBranch;
  appliedCount: number;
  skippedCount: number;
}

// 409 body shape returned by POST /merge when conflicts are unresolved.
export interface MergeConflictBody {
  errorCode: 'MERGE_CONFLICT';
  conflicts: AnnotatedMergeConflict[];
  unresolved: AnnotatedMergeConflict[];
}
