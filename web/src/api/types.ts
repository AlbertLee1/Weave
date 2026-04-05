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
