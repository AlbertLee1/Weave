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
  // US-211: ordered list of property apiNames that together form a composite
  // primary key. Omitted (or single-element) for the legacy single-key case.
  primaryKeys?: string[];
  titleProperty?: string;
  status: 'ACTIVE' | 'ENDORSED' | 'EXPERIMENTAL' | 'DEPRECATED';
  visibility: 'PROMINENT' | 'NORMAL' | 'HIDDEN';
  // Free-form note recording *why* this type was deprecated. Mirrors the
  // backend ObjectType.DeprecatedReason `json:"deprecatedReason,omitempty"`.
  deprecatedReason?: string;
  // RFC3339 timestamp for *when* a deprecated type should be retired. Mirrors
  // the backend ObjectType.DeprecatedDeadline *time.Time
  // `json:"deprecatedDeadline,omitempty"` (serialized as an RFC3339 string).
  deprecatedDeadline?: string | null;
  icon?: string;
  color?: string;
  classification?: Classification;
  // US-212: RID of a parent ObjectType this type inherits from (same ontology).
  extendsRid?: string;
  // US-264: opts the ObjectType into per-read data-access audit logging.
  auditDataAccess?: boolean;
  // VTX-077: timeline event metadata. isEvent marks the ObjectType as an event
  // the Vertex Timeline renders as a bar from eventStartProp to eventEndProp.
  isEvent?: boolean;
  eventStartProp?: string;
  eventEndProp?: string;
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
  // US-261: when set, this LinkType points at its inverse LinkType (the link
  // describing the reverse direction), making the relationship bidirectional.
  // Serialised from pkg/oms LinkType.InverseLinkRID; omitted when empty.
  inverseLinkRid?: string;
  // US-261: when true, Markings on linked objects are inherited across this
  // link. Serialised from pkg/oms LinkType.PropagateMarkings; omitted when false.
  propagateMarkings?: boolean;
  // VTX-010: Vertex graph rendering tags. See
  // web/src/features/vertex/links/edgeArrowStyle.ts for the recognised
  // values. Omitted on the wire when empty.
  typeClasses?: string[];
}

// US-210 / US-497 — declared typed property on a MANY_TO_MANY link's
// edges. Shape mirrors pkg/oms.LinkProperty so the same form code can
// render both Property and LinkProperty.
export interface LinkProperty {
  rid: string;
  linkTypeRid: string;
  apiName: string;
  displayName?: string;
  description?: string;
  baseType: string;
  typeConfig?: unknown;
  isArray: boolean;
  isNullable: boolean;
}

// US-210 / US-497 — single row of link_edges, returned by the
// PUT /links/{rid}/edges/{src}/{tgt}/properties endpoint.
export interface LinkEdge {
  linkTypeRid: string;
  sourceObjectPk: string;
  targetObjectPk: string;
  edgeProperties?: Record<string, unknown>;
  createdAt: string;
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
  // US-242 approval gating.
  requiresApproval?: boolean;
  approvers?: string[];
  // US-239 saga compensation pairing.
  compensateActionRid?: string;
  // US-245 Draft-07 parameter schema.
  parameterSchema?: unknown;
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
  _highlights?: Record<string, string[]>;
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
  // Accuracy mode for distinct/percentile metrics. The backend reads
  // '' / 'ALLOW_APPROXIMATE' (default) vs 'REQUIRE_ACCURATE' (promotes
  // approximateDistinct→exactDistinct and approximatePercentile→exact
  // sorted percentile). Omit for the approximate default.
  accuracy?: 'ALLOW_APPROXIMATE' | 'REQUIRE_ACCURATE';
  // Post-aggregation row filters (SQL HAVING). Each clause names a metric
  // produced by THIS request, a comparison op, and a numeric threshold; the
  // backend drops result rows where any clause fails (AND semantics). Wire
  // shape mirrors aggregation.HavingClause (metric/op/value). Omit when empty.
  having?: HavingClause[];
  // Cube, when true, asks the backend to compute every 2^N subset of the
  // declared groupBys and concatenate the resulting rows (most specific subset
  // first, down to the grand total). Rows for aggregated-away dimensions omit
  // those keys from `group` — the result renderer surfaces them as subtotals.
  // Mutually exclusive with rollup; the backend prefers cube when both are set,
  // so the UI only ever sends one. Omit for the plain (none) mode.
  cube?: boolean;
  // Rollup, when true, asks the backend to compute the hierarchical chain
  // [gb0..N], [gb0..N-1], ..., [gb0], [] — N+1 groupings, each a subtotal of
  // the level above. Same group-key-absence semantics as cube. Mutually
  // exclusive with cube. Omit for the plain (none) mode.
  rollup?: boolean;
}

// HavingClause is a post-aggregation row filter, wire-compatible with the
// backend aggregation.HavingClause (pkg/oss/aggregation/engine.go). `metric`
// is the output name of a metric in the same request; an unnamed metric uses
// the server's derived name, so prefer aliasing the metric for robust matching.
export interface HavingClause {
  metric: string;
  op: 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte';
  value: number;
}

export interface AggregationMetric {
  type:
    | 'count'
    | 'min'
    | 'max'
    | 'sum'
    | 'avg'
    | 'approximateDistinct'
    | 'exactDistinct'
    | 'standardDeviation'
    | 'variance'
    | 'approximatePercentile'
    | 'collectList';
  field?: string;
  name?: string;
  // approximatePercentile (0-100); wire key `percentile`.
  percentile?: number;
  // collectList max values to collect; wire key `maxItems`.
  maxItems?: number;
  // approximateDistinct HyperLogLog precision (4-18); wire key `precision`.
  precision?: number;
  // direction orders the groupBy result rows by THIS metric's value
  // ("按聚合值排序"). The backend attaches ordering to a single metric — when
  // several carry one the first wins — so the UI keeps at most one set.
  direction?: 'ASC' | 'DESC';
}

export interface GroupByClause {
  field: string;
  type: 'exact' | 'fixedWidth' | 'ranges' | 'duration' | 'topValues' | 'geohash';
  // exact / topValues cap; wire key `maxGroupCount`.
  maxGroupCount?: number;
  // ranges buckets (Palantir V2 startValue/endValue); wire key `ranges`.
  ranges?: Array<{ name?: string; startValue?: number; endValue?: number }>;
  // duration period, ISO 8601 (P1D/P1W/P1M/P3M/P1Y/PT1H); wire key `duration`.
  duration?: string;
  // geohash character precision (1-12); wire key `precision`.
  precision?: number;
  // fixedWidth is the numeric bucket width, required when type === 'fixedWidth'
  // (the backend rejects a width-less fixedWidth groupBy). Wire key matches the
  // server's GroupBySpec.Width json tag.
  fixedWidth?: number;
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

// ActionBatchApplyOptions narrows the single-apply options for the batch
// path. The server's ApplyBatch handler (pkg/actions/handlers.go) only
// accepts returnEdits ∈ {ALL, NONE} and 400s on ALL_V2_WITH_DELETIONS — that
// mode is single-apply only. Deliberately NOT extending ActionApplyOptions so
// the broader single-apply returnEdits union can never leak onto the batch
// wire (which would always round-trip a 400).
export interface ActionBatchApplyOptions {
  returnEdits?: 'ALL' | 'NONE';
}

// ActionBatchApplyRequest is the body shape for applyBatch.
export interface ActionBatchApplyRequest {
  requests: Array<{ parameters: Record<string, unknown> }>;
  options?: ActionBatchApplyOptions;
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
  // functionRid points at the embedded Function backing a function-backed
  // QueryType (pkg/oms.QueryType.FunctionRID -> wire["functionRid"]).
  // Absent for non-function-backed queries.
  functionRid?: string;
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
  | SampleObjectSet
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
  // propertyIdentifiers (Gap-Q4) runs KNN against multiple vector columns
  // in parallel; mutually exclusive with the singular propertyIdentifier.
  propertyIdentifiers?: Array<{ property: { apiName: string } }>;
  // fusionStrategy selects how multi-column matches are combined:
  // '' / 'min' (min distance per PK) or 'rrf' (Reciprocal Rank Fusion).
  // Ignored on single-column queries.
  fusionStrategy?: '' | 'min' | 'rrf';
  numNeighbors?: number;
  similarityThreshold?: number;
  query?: {
    vector?: { value: number[] };
    text?: { value: string };
  };
}

// SampleObjectSet (US-225) draws a reservoir sample of the inner ObjectSet.
// size must be > 0; seed is an optional deterministic PRNG seed.
export interface SampleObjectSet {
  type: 'sample';
  objectSet: ObjectSetDefinition;
  size: number;
  seed?: number;
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

// --- US-113 / US-383 Branch create (mirrors pkg/oms/handlers_branch.go
// CreateBranchRequest). The handler returns the raw OntologyBranch with
// HTTP 201. `name` is the only required field; the rest chain/pin the new
// branch off a parent or a dataset-transaction checkpoint.
export interface CreateBranchRequest {
  name: string;
  createdBy?: string;
  parentBranchId?: string;
  baseTx?: string;
}

// 409 body shape returned by POST /rebase when the branch's pending changes
// conflict with newer main-trunk state. Mirrors handlers_branch.go
// RebaseBranch's REBASE_CONFLICT branch.
export interface RebaseConflictBody {
  errorCode: 'REBASE_CONFLICT';
  conflicts: AnnotatedMergeConflict[];
}
