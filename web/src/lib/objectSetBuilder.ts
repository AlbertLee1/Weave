import type {
  DerivedPropertyDef,
  ObjectSetDefinition,
  WhereClause,
} from '../api/types';

// ObjectSetNode is a tree node mirroring ObjectSetDefinition with a stable id
// for React rendering. The `id` field is stripped when converting to a wire
// definition.
export type ObjectSetNode =
  | BaseNode
  | StaticNode
  | FilterNode
  | UnionNode
  | IntersectNode
  | SubtractNode
  | SearchAroundNode
  | ReferenceNode
  | WithPropertiesNode
  | NearestNeighborsNode
  | SampleNode
  | UnsupportedObjectSetNode;

interface NodeBase {
  id: string;
}

export interface BaseNode extends NodeBase {
  type: 'base';
  objectType: string;
}

export interface StaticNode extends NodeBase {
  type: 'static';
  objectType: string;
  primaryKeys: string[];
}

export interface FilterNode extends NodeBase {
  type: 'filter';
  objectSet: ObjectSetNode;
  where: WhereClause;
}

export interface UnionNode extends NodeBase {
  type: 'union';
  objectSets: ObjectSetNode[];
}

export interface IntersectNode extends NodeBase {
  type: 'intersect';
  objectSets: ObjectSetNode[];
}

export interface SubtractNode extends NodeBase {
  type: 'subtract';
  objectSets: ObjectSetNode[];
}

export interface SearchAroundNode extends NodeBase {
  type: 'searchAround';
  objectSet: ObjectSetNode;
  link: string;
  direction?: 'forward' | 'reverse';
}

export interface ReferenceNode extends NodeBase {
  type: 'reference';
  reference: string;
}

export interface WithPropertiesNode extends NodeBase {
  type: 'withProperties';
  objectSet: ObjectSetNode;
  properties?: string[];
  derivedProperties?: DerivedPropertyDef[];
}

export interface NearestNeighborsNode extends NodeBase {
  type: 'nearestNeighbors';
  objectSet: ObjectSetNode;
  propertyIdentifier?: { property: { apiName: string } };
  propertyIdentifiers?: Array<{ property: { apiName: string } }>;
  fusionStrategy?: '' | 'min' | 'rrf';
  numNeighbors?: number;
  similarityThreshold?: number;
  query?: {
    vector?: { value: number[] };
    text?: { value: string };
  };
}

export interface SampleNode extends NodeBase {
  type: 'sample';
  objectSet: ObjectSetNode;
  size: number;
  seed?: number;
}

export type ObjectSetComposerVariantSupport =
  | 'editable'
  | 'readOnlyUnsupported';

export const OBJECT_SET_COMPOSER_VARIANT_SUPPORT = {
  base: 'editable',
  static: 'editable',
  filter: 'editable',
  union: 'editable',
  intersect: 'editable',
  subtract: 'editable',
  searchAround: 'editable',
  reference: 'editable',
  withProperties: 'editable',
  nearestNeighbors: 'editable',
  sample: 'editable',
  asType: 'readOnlyUnsupported',
  asBaseObjectTypes: 'readOnlyUnsupported',
  interfaceBase: 'readOnlyUnsupported',
  interfaceLinkSearchAround: 'readOnlyUnsupported',
  methodInput: 'readOnlyUnsupported',
} satisfies Record<ObjectSetDefinition['type'], ObjectSetComposerVariantSupport>;

export type EditableObjectSetType = {
  [Type in ObjectSetDefinition['type']]: (typeof OBJECT_SET_COMPOSER_VARIANT_SUPPORT)[Type] extends 'editable'
    ? Type
    : never;
}[ObjectSetDefinition['type']];

export interface UnsupportedObjectSetNode extends NodeBase {
  type: 'unsupported';
  objectSetType: ObjectSetDefinition['type'];
  def: ObjectSetDefinition;
}

export function isEditableObjectSetType(
  type: ObjectSetDefinition['type'],
): type is EditableObjectSetType {
  return OBJECT_SET_COMPOSER_VARIANT_SUPPORT[type] === 'editable';
}

export function unsupportedObjectSetMessage(
  type: ObjectSetDefinition['type'],
): string {
  return `${type} ObjectSet is supported by the backend but is read-only in the composer`;
}

// US-332: each saved ObjectSet carries a list of named/timestamped versions
// so users can iterate on a query without losing earlier shapes. The legacy
// shape (`def` + `createdAt` only) is still accepted by readSaved and lifted
// into a single-entry `versions` array on first read so existing local data
// keeps working.
export interface ObjectSetVersion {
  versionId: string;
  def: ObjectSetDefinition;
  createdAt: string;
  note?: string;
}

export interface SavedObjectSet {
  id: string;
  name: string;
  // def mirrors the active version's definition so existing call sites
  // (`s.def`) keep working. Version-aware code should read
  // `findActiveVersion(s)` instead.
  def: ObjectSetDefinition;
  createdAt: string;
  versions: ObjectSetVersion[];
  activeVersionId: string;
}

export function findActiveVersion(saved: SavedObjectSet): ObjectSetVersion | undefined {
  return (
    saved.versions.find((v) => v.versionId === saved.activeVersionId) ??
    saved.versions[0]
  );
}

export function newVersionId(): string {
  return `v-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
}

let idCounter = 0;
export function newId(): string {
  idCounter += 1;
  return `n${Date.now().toString(36)}-${idCounter}`;
}

export function emptyBase(objectType: string): BaseNode {
  return { id: newId(), type: 'base', objectType };
}

// nodeToDefinition strips ids and produces the wire-format Definition.
export function nodeToDefinition(node: ObjectSetNode): ObjectSetDefinition {
  switch (node.type) {
    case 'base':
      return { type: 'base', objectType: node.objectType };
    case 'static':
      return {
        type: 'static',
        objectType: node.objectType,
        primaryKeys: node.primaryKeys
          .map((pk) => pk.trim())
          .filter((pk) => pk.length > 0),
      };
    case 'filter':
      return {
        type: 'filter',
        objectSet: nodeToDefinition(node.objectSet),
        where: node.where,
      };
    case 'union':
      return {
        type: 'union',
        objectSets: node.objectSets.map(nodeToDefinition),
      };
    case 'intersect':
      return {
        type: 'intersect',
        objectSets: node.objectSets.map(nodeToDefinition),
      };
    case 'subtract':
      return {
        type: 'subtract',
        objectSets: node.objectSets.map(nodeToDefinition),
      };
    case 'searchAround': {
      const def: ObjectSetDefinition = {
        type: 'searchAround',
        objectSet: nodeToDefinition(node.objectSet),
        link: node.link,
      };
      if (node.direction) {
        (def as { direction?: 'forward' | 'reverse' }).direction = node.direction;
      }
      return def;
    }
    case 'reference':
      return { type: 'reference', reference: node.reference };
    case 'withProperties': {
      const def: ObjectSetDefinition = {
        type: 'withProperties',
        objectSet: nodeToDefinition(node.objectSet),
      };
      if (node.properties && node.properties.length > 0) {
        def.properties = node.properties;
      }
      if (node.derivedProperties && node.derivedProperties.length > 0) {
        def.derivedProperties = node.derivedProperties.map((dp) => ({ ...dp }));
      }
      return def;
    }
    case 'nearestNeighbors':
      return {
        type: 'nearestNeighbors',
        objectSet: nodeToDefinition(node.objectSet),
        propertyIdentifier: node.propertyIdentifier,
        propertyIdentifiers: node.propertyIdentifiers,
        fusionStrategy: node.fusionStrategy,
        numNeighbors: node.numNeighbors,
        similarityThreshold: node.similarityThreshold,
        query: node.query,
      };
    case 'sample': {
      const def: ObjectSetDefinition = {
        type: 'sample',
        objectSet: nodeToDefinition(node.objectSet),
        size: node.size,
      };
      if (node.seed !== undefined) {
        def.seed = node.seed;
      }
      return def;
    }
    case 'unsupported':
      return node.def;
  }
}

// definitionToNode wraps a wire Definition in node form, generating fresh ids.
export function definitionToNode(def: ObjectSetDefinition): ObjectSetNode {
  switch (def.type) {
    case 'base':
      return { id: newId(), type: 'base', objectType: def.objectType };
    case 'static':
      return {
        id: newId(),
        type: 'static',
        objectType: def.objectType,
        primaryKeys: def.primaryKeys,
      };
    case 'filter':
      return {
        id: newId(),
        type: 'filter',
        objectSet: definitionToNode(def.objectSet),
        where: def.where,
      };
    case 'union':
      return {
        id: newId(),
        type: 'union',
        objectSets: def.objectSets.map(definitionToNode),
      };
    case 'intersect':
      return {
        id: newId(),
        type: 'intersect',
        objectSets: def.objectSets.map(definitionToNode),
      };
    case 'subtract':
      return {
        id: newId(),
        type: 'subtract',
        objectSets: def.objectSets.map(definitionToNode),
      };
    case 'searchAround':
      return {
        id: newId(),
        type: 'searchAround',
        objectSet: definitionToNode(def.objectSet),
        link: def.link,
        direction: def.direction,
      };
    case 'reference':
      return { id: newId(), type: 'reference', reference: def.reference };
    case 'withProperties':
      return {
        id: newId(),
        type: 'withProperties',
        objectSet: definitionToNode(def.objectSet),
        properties: def.properties,
        derivedProperties: def.derivedProperties?.map((dp) => ({ ...dp })),
      };
    case 'nearestNeighbors':
      return {
        id: newId(),
        type: 'nearestNeighbors',
        objectSet: definitionToNode(def.objectSet),
        propertyIdentifier: def.propertyIdentifier,
        propertyIdentifiers: def.propertyIdentifiers,
        fusionStrategy: def.fusionStrategy,
        numNeighbors: def.numNeighbors,
        similarityThreshold: def.similarityThreshold,
        query: def.query,
      };
    case 'sample':
      return {
        id: newId(),
        type: 'sample',
        objectSet: definitionToNode(def.objectSet),
        size: def.size,
        seed: def.seed,
      };
    case 'asType':
    case 'asBaseObjectTypes':
    case 'interfaceBase':
    case 'interfaceLinkSearchAround':
    case 'methodInput':
      return {
        id: newId(),
        type: 'unsupported',
        objectSetType: def.type,
        def,
      };
  }
}

// validateNode returns a flat list of human-readable errors. An empty array
// means the tree is ready to execute.
export function validateNode(node: ObjectSetNode): string[] {
  const errors: string[] = [];
  walk(node, errors);
  return errors;
}

export function validateDefinition(def: ObjectSetDefinition): string[] {
  return validateNode(definitionToNode(def));
}

const SUPPORTED_WHERE_OPERATORS = new Set([
  'eq',
  'gt',
  'gte',
  'lt',
  'lte',
  'isNull',
  'contains',
  'fuzzy',
  'phrase',
  'regex',
  'containsAllTerms',
  'containsAnyTerm',
  'containsAllTermsInOrder',
  'containsAllTermsInOrderPrefixLastTerm',
  'startsWith',
  'wildcard',
  'and',
  'or',
  'not',
  'withinBoundingBox',
  'intersectsBoundingBox',
  'withinPolygon',
  'intersectsPolygon',
  'doesNotIntersectPolygon',
  'doesNotIntersectBoundingBox',
  'withinDistanceOf',
]);

const VALUE_OPTIONAL_WHERE_OPERATORS = new Set(['isNull']);

function isWhereClauseLike(value: unknown): value is WhereClause {
  return (
    typeof value === 'object' &&
    value !== null &&
    'type' in value &&
    typeof (value as { type?: unknown }).type === 'string'
  );
}

function valueIsBlank(value: unknown): boolean {
  return (
    value === undefined ||
    value === null ||
    (typeof value === 'string' && value.trim().length === 0)
  );
}

function validateWhereClause(where: WhereClause, errors: string[]): void {
  const type = typeof where.type === 'string' ? where.type.trim() : '';
  if (!type) {
    errors.push('filter node requires a where clause');
    return;
  }
  if (!SUPPORTED_WHERE_OPERATORS.has(type)) {
    errors.push(`filter where clause uses unsupported operator "${type}"`);
    return;
  }

  if (type === 'and' || type === 'or') {
    if (!Array.isArray(where.value) || where.value.length === 0) {
      errors.push(`filter where clause ${type} requires child clauses`);
      return;
    }
    for (const child of where.value) {
      if (!isWhereClauseLike(child)) {
        errors.push(`filter where clause ${type} requires child clauses`);
        continue;
      }
      validateWhereClause(child, errors);
    }
    return;
  }

  if (type === 'not') {
    const child = Array.isArray(where.value) ? where.value[0] : where.value;
    if (!isWhereClauseLike(child)) {
      errors.push('filter where clause not requires a child clause');
      return;
    }
    validateWhereClause(child, errors);
    return;
  }

  if (!where.field || where.field.trim().length === 0) {
    errors.push(`filter where clause ${type} requires a field`);
  }
  if (!VALUE_OPTIONAL_WHERE_OPERATORS.has(type) && valueIsBlank(where.value)) {
    errors.push(`filter where clause ${type} requires a value`);
  }
}

function walk(node: ObjectSetNode, errors: string[]): void {
  switch (node.type) {
    case 'base':
      if (!node.objectType) errors.push('base node requires an object type');
      break;
    case 'static':
      if (!node.objectType) errors.push('static node requires an object type');
      break;
    case 'filter':
      if (!node.where || !(node.where as WhereClause).type) {
        errors.push('filter node requires a where clause');
      } else {
        validateWhereClause(node.where, errors);
      }
      walk(node.objectSet, errors);
      break;
    case 'union':
    case 'intersect':
    case 'subtract':
      if (node.objectSets.length < 2) {
        errors.push(`${node.type} node requires at least 2 child object sets`);
      }
      for (const child of node.objectSets) walk(child, errors);
      break;
    case 'searchAround':
      if (!node.link) errors.push('searchAround node requires a link');
      walk(node.objectSet, errors);
      break;
    case 'reference':
      if (!node.reference) errors.push('reference node requires a reference id');
      break;
    case 'withProperties':
      walk(node.objectSet, errors);
      break;
    case 'nearestNeighbors': {
      const columns = node.propertyIdentifiers ?? [];
      const hasSingular = Boolean(node.propertyIdentifier?.property.apiName);
      const hasPlural =
        columns.length > 0 &&
        columns.every((c) => Boolean(c.property.apiName));
      if (!hasSingular && !hasPlural) {
        errors.push('nearestNeighbors node requires an embedding property');
      }
      if (node.numNeighbors !== undefined && node.numNeighbors <= 0) {
        errors.push('nearestNeighbors node requires neighbors > 0');
      }
      if (
        !node.query?.text?.value &&
        (!node.query?.vector?.value || node.query.vector.value.length === 0)
      ) {
        errors.push('nearestNeighbors node requires query text or vector');
      }
      walk(node.objectSet, errors);
      break;
    }
    case 'sample':
      if (!Number.isFinite(node.size) || node.size <= 0) {
        errors.push('sample node requires size > 0');
      }
      walk(node.objectSet, errors);
      break;
    case 'unsupported':
      errors.push(unsupportedObjectSetMessage(node.objectSetType));
      break;
  }
}

export function localStorageKey(ontologyApiName: string): string {
  return `weave:objectSets:${ontologyApiName}`;
}
