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
  status: 'PROMOTED' | 'ACTIVE' | 'EXPERIMENTAL' | 'DEPRECATED' | 'EXAMPLE';
  visibility: 'PROMINENT' | 'NORMAL' | 'HIDDEN';
  icon?: string;
  color?: string;
  properties?: Record<string, { dataType: DataType; rid: string }>;
}

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
}

export interface ActionType {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  status: string;
  parameters: unknown;
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

export interface WireObject {
  __rid: string;
  __primaryKey: string | number;
  __apiName: string;
  [property: string]: unknown;
}

export interface ObjectPage {
  data: WireObject[];
  nextPageToken?: string;
  totalCount?: string;
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

export interface ActionApplyRequest {
  actionType: string;
  parameters: Record<string, unknown>;
}

export interface ActionApplyResponse {
  edits?: ActionEdit[];
}

export interface ActionEdit {
  type: 'addObject' | 'modifyObject' | 'deleteObject';
  objectType: string;
  primaryKey: string | number;
  properties?: Record<string, unknown>;
}

export interface ApiError {
  errorCode: string;
  errorName: string;
  errorInstanceId: string;
  parameters?: Record<string, string>;
  statusCode: number;
}

export interface OntologyInterface {
  rid: string;
  apiName: string;
  displayName: string;
  extendsRid?: string;
  sharedProperties?: unknown;
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
  | FilterObjectSet
  | UnionObjectSet
  | IntersectObjectSet
  | SubtractObjectSet
  | SearchAroundObjectSet
  | ReferenceObjectSet
  | WithPropertiesObjectSet
  | NearestNeighborsObjectSet;

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

export interface WithPropertiesObjectSet {
  type: 'withProperties';
  objectSet: ObjectSetDefinition;
  properties?: string[];
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

export interface OrderByField {
  field: string;
  direction: 'asc' | 'desc';
}

export interface OrderBy {
  fields: OrderByField[];
}

export interface LoadObjectSetRequest {
  objectSet: ObjectSetDefinition;
  select?: string[];
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
