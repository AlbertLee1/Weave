import { describe, it, expect } from 'vitest';
import {
  emptyBase,
  nodeToDefinition,
  definitionToNode,
  validateNode,
  newId,
  localStorageKey,
  type ObjectSetNode,
} from '../objectSetBuilder';
import type { ObjectSetDefinition } from '../../api/types';

describe('emptyBase', () => {
  it('creates a base node with given object type', () => {
    const node = emptyBase('Employee');
    expect(node.type).toBe('base');
    if (node.type === 'base') {
      expect(node.objectType).toBe('Employee');
    }
    expect(node.id).toBeTruthy();
  });
});

describe('newId', () => {
  it('returns a unique string', () => {
    const a = newId();
    const b = newId();
    expect(a).not.toEqual(b);
    expect(typeof a).toBe('string');
  });
});

describe('nodeToDefinition / definitionToNode round-trip', () => {
  it('round-trips a base node', () => {
    const node = emptyBase('Employee');
    const def = nodeToDefinition(node);
    expect(def).toEqual({ type: 'base', objectType: 'Employee' });
    const back = definitionToNode(def);
    expect(back.type).toBe('base');
    if (back.type === 'base') {
      expect(back.objectType).toBe('Employee');
    }
  });

  it('round-trips a filter node', () => {
    const node: ObjectSetNode = {
      id: '1',
      type: 'filter',
      objectSet: emptyBase('Employee'),
      where: { type: 'eq', field: 'department', value: 'Engineering' },
    };
    const def = nodeToDefinition(node);
    expect(def).toEqual({
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Employee' },
      where: { type: 'eq', field: 'department', value: 'Engineering' },
    });
    const back = definitionToNode(def);
    expect(back.type).toBe('filter');
  });

  it('round-trips a union node with two children', () => {
    const node: ObjectSetNode = {
      id: '1',
      type: 'union',
      objectSets: [emptyBase('Employee'), emptyBase('Manager')],
    };
    const def = nodeToDefinition(node) as { objectSets: ObjectSetDefinition[] };
    expect(def.objectSets).toHaveLength(2);
    const back = definitionToNode(node as ObjectSetNode as never as ObjectSetDefinition);
    // direct path: build def then back
    const back2 = definitionToNode(nodeToDefinition(node));
    expect(back2.type).toBe('union');
    if (back2.type === 'union') {
      expect(back2.objectSets).toHaveLength(2);
    }
    // silence unused variable lint
    expect(back).toBeDefined();
  });

  it('round-trips a withProperties node with derivedProperties', () => {
    const node: ObjectSetNode = {
      id: '1',
      type: 'withProperties',
      objectSet: emptyBase('customer'),
      derivedProperties: [
        {
          name: 'orderCount',
          link: 'customerOrders',
          direction: 'forward',
          metric: 'count',
        },
      ],
    };
    const def = nodeToDefinition(node);
    expect(def).toEqual({
      type: 'withProperties',
      objectSet: { type: 'base', objectType: 'customer' },
      derivedProperties: [
        {
          name: 'orderCount',
          link: 'customerOrders',
          direction: 'forward',
          metric: 'count',
        },
      ],
    });
    const back = definitionToNode(def);
    expect(back.type).toBe('withProperties');
    if (back.type === 'withProperties') {
      expect(back.derivedProperties).toHaveLength(1);
      expect(back.derivedProperties?.[0]).toMatchObject({
        name: 'orderCount',
        link: 'customerOrders',
        metric: 'count',
      });
    }
  });

  it('round-trips a searchAround node', () => {
    const node: ObjectSetNode = {
      id: '1',
      type: 'searchAround',
      objectSet: emptyBase('Employee'),
      link: 'reportsTo',
      direction: 'forward',
    };
    const def = nodeToDefinition(node);
    expect(def).toEqual({
      type: 'searchAround',
      objectSet: { type: 'base', objectType: 'Employee' },
      link: 'reportsTo',
      direction: 'forward',
    });
  });

  it('round-trips a nearestNeighbors node', () => {
    const node: ObjectSetNode = {
      id: '1',
      type: 'nearestNeighbors',
      objectSet: emptyBase('Incident'),
      propertyIdentifier: { property: { apiName: 'embedding' } },
      numNeighbors: 3,
      query: { text: { value: 'find similar incidents' } },
    };

    const def = nodeToDefinition(node);
    expect(def).toEqual({
      type: 'nearestNeighbors',
      objectSet: { type: 'base', objectType: 'Incident' },
      propertyIdentifier: { property: { apiName: 'embedding' } },
      numNeighbors: 3,
      query: { text: { value: 'find similar incidents' } },
    });

    const back = definitionToNode(def);
    expect(back.type).toBe('nearestNeighbors');
    if (back.type === 'nearestNeighbors') {
      expect(back.propertyIdentifier?.property.apiName).toBe('embedding');
      expect(back.numNeighbors).toBe(3);
      expect(back.query?.text?.value).toBe('find similar incidents');
    }
  });

  it('round-trips a static node and removes blank primary keys', () => {
    const def: ObjectSetDefinition = {
      type: 'static',
      objectType: 'Employee',
      primaryKeys: ['e1', '', ' e2 ', '   '],
    };

    const node = definitionToNode(def);
    expect(node.type).toBe('static');
    if (node.type === 'static') {
      expect(node.objectType).toBe('Employee');
      expect(node.primaryKeys).toEqual(['e1', '', ' e2 ', '   ']);
    }

    expect(nodeToDefinition(node)).toEqual({
      type: 'static',
      objectType: 'Employee',
      primaryKeys: ['e1', 'e2'],
    });
  });

  it('degrades backend-only variants into read-only unsupported nodes', () => {
    const defs = [
      {
        type: 'asType',
        objectType: 'Employee',
        objectSet: { type: 'base', objectType: 'Person' },
      },
      {
        type: 'asBaseObjectTypes',
        objectSet: { type: 'base', objectType: 'Person' },
      },
      { type: 'interfaceBase', interfaceType: 'PersonInterface' },
      {
        type: 'interfaceLinkSearchAround',
        objectSet: { type: 'base', objectType: 'Person' },
        interfaceLink: 'assignedTo',
      },
      { type: 'methodInput', input: 'selectedObjects' },
    ] satisfies ObjectSetDefinition[];

    for (const def of defs) {
      const node = definitionToNode(def);
      expect(node.type).toBe('unsupported');
      if (node.type === 'unsupported') {
        expect(node.objectSetType).toBe(def.type);
        expect(nodeToDefinition(node)).toEqual(def);
      }
      expect(validateNode(node)).toContain(
        `${def.type} ObjectSet is supported by the backend but is read-only in the composer`,
      );
    }
  });

  it('contracts every ObjectSetDefinition variant as editable or read-only unsupported', () => {
    const examples = {
      base: { type: 'base', objectType: 'Employee' },
      static: { type: 'static', objectType: 'Employee', primaryKeys: [] },
      filter: {
        type: 'filter',
        objectSet: { type: 'base', objectType: 'Employee' },
        where: { type: 'eq', field: 'name', value: 'Ada' },
      },
      union: {
        type: 'union',
        objectSets: [
          { type: 'base', objectType: 'Employee' },
          { type: 'base', objectType: 'Manager' },
        ],
      },
      intersect: {
        type: 'intersect',
        objectSets: [
          { type: 'base', objectType: 'Employee' },
          { type: 'base', objectType: 'Manager' },
        ],
      },
      subtract: {
        type: 'subtract',
        objectSets: [
          { type: 'base', objectType: 'Employee' },
          { type: 'base', objectType: 'Manager' },
        ],
      },
      searchAround: {
        type: 'searchAround',
        objectSet: { type: 'base', objectType: 'Employee' },
        link: 'reportsTo',
      },
      reference: { type: 'reference', reference: 'ri.objectSet.saved.1' },
      withProperties: {
        type: 'withProperties',
        objectSet: { type: 'base', objectType: 'Employee' },
      },
      nearestNeighbors: {
        type: 'nearestNeighbors',
        objectSet: { type: 'base', objectType: 'Employee' },
        propertyIdentifier: { property: { apiName: 'embedding' } },
        numNeighbors: 5,
        query: { text: { value: 'similar employees' } },
      },
      sample: {
        type: 'sample',
        objectSet: { type: 'base', objectType: 'Employee' },
        size: 10,
        seed: 42,
      },
      asType: {
        type: 'asType',
        objectType: 'Employee',
        objectSet: { type: 'base', objectType: 'Person' },
      },
      asBaseObjectTypes: {
        type: 'asBaseObjectTypes',
        objectSet: { type: 'base', objectType: 'Person' },
      },
      interfaceBase: { type: 'interfaceBase', interfaceType: 'PersonInterface' },
      interfaceLinkSearchAround: {
        type: 'interfaceLinkSearchAround',
        objectSet: { type: 'base', objectType: 'Person' },
        interfaceLink: 'assignedTo',
      },
      methodInput: { type: 'methodInput', input: 'selectedObjects' },
    } satisfies Record<ObjectSetDefinition['type'], ObjectSetDefinition>;

    for (const def of Object.values(examples)) {
      expect(() => definitionToNode(def)).not.toThrow();
    }
  });

  it('omits ids in the produced definition', () => {
    const node: ObjectSetNode = {
      id: 'root-id',
      type: 'filter',
      objectSet: { id: 'child-id', type: 'base', objectType: 'Employee' },
      where: { type: 'eq', field: 'x', value: 'y' },
    };
    const def = nodeToDefinition(node);
    expect(JSON.stringify(def)).not.toContain('root-id');
    expect(JSON.stringify(def)).not.toContain('child-id');
  });
});

describe('validateNode', () => {
  it('returns no errors for a valid base node', () => {
    expect(validateNode(emptyBase('Employee'))).toEqual([]);
  });

  it('flags base node missing objectType', () => {
    const errs = validateNode({ id: '1', type: 'base', objectType: '' });
    expect(errs.length).toBeGreaterThan(0);
  });

  it('flags filter node missing where clause', () => {
    const errs = validateNode({
      id: '1',
      type: 'filter',
      objectSet: emptyBase('Employee'),
      where: undefined as never,
    });
    expect(errs.length).toBeGreaterThan(0);
  });

  it('flags filter clauses with missing field, missing value, or unsupported operator', () => {
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: { type: 'eq', field: '', value: '' },
      }),
    ).toEqual([
      'filter where clause eq requires a field',
      'filter where clause eq requires a value',
    ]);

    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: { type: 'neq', field: 'status', value: 'archived' },
      }),
    ).toContain('filter where clause uses unsupported operator "neq"');
  });

  it('accepts isNull filter clauses without a comparison value', () => {
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: { type: 'isNull', field: 'archivedAt', value: '' },
      }),
    ).toEqual([]);
  });

  it('accepts backend text-search filter clauses with string values', () => {
    for (const type of [
      'contains',
      'containsAllTerms',
      'containsAllTermsInOrder',
      'startsWith',
    ]) {
      expect(
        validateNode({
          id: '1',
          type: 'filter',
          objectSet: emptyBase('Employee'),
          where: { type, field: 'description', value: 'critical outage' },
        }),
      ).toEqual([]);
    }
  });

  it('accepts Foundry in filter clauses carrying an array of candidate values', () => {
    // BDD: Given a filter node using the backend-supported `in` operator
    // (pkg/oss/where/converter.go case "in" — matches objects whose field
    // equals ANY value in the array), When the builder validates the tree,
    // Then no "unsupported operator" error is raised so the UI stays in
    // sync with backend capability.
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: { type: 'in', field: 'status', value: ['active', 'pending'] },
      }),
    ).toEqual([]);
  });

  it('accepts Foundry interval filter clauses carrying a rule instead of a value', () => {
    // BDD: Given a filter node using the backend-supported `interval`
    // operator (pkg/oss/where/interval.go — Foundry IntervalQuery: the
    // payload is a sub-rule tree under `rule`, there is NO `value` key),
    // When the builder validates the tree, Then neither an "unsupported
    // operator" nor a "requires a value" error is raised.
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: {
          type: 'interval',
          field: 'description',
          rule: { type: 'match', query: 'software engineer', ordered: true },
        },
      }),
    ).toEqual([]);
  });

  it('flags interval filter clauses missing their rule', () => {
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: { type: 'interval', field: 'description' },
      }),
    ).toEqual(['filter where clause interval requires a rule object']);
  });

  it('accepts Foundry relativeDateRange filter clauses with relative bounds and a timeZoneId', () => {
    // BDD: Given a filter node using the backend-supported
    // `relativeDateRange` operator (pkg/oss/where/relative_date_range.go —
    // Foundry RelativeDateRangeQuery: bounds live in relativeStartTime /
    // relativeEndTime and timeZoneId is required; there is NO `value`),
    // When the builder validates the tree, Then no error is raised.
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: {
          type: 'relativeDateRange',
          field: 'hiredAt',
          relativeStartTime: { type: 'relativePoint', value: -7, timeUnit: 'DAY' },
          relativeEndTime: { type: 'relativePoint', value: 1, timeUnit: 'DAY' },
          timeZoneId: 'Etc/UTC',
        },
      }),
    ).toEqual([]);
  });

  it('flags relativeDateRange filter clauses missing timeZoneId or both bounds', () => {
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: {
          type: 'relativeDateRange',
          field: 'hiredAt',
          relativeStartTime: { type: 'relativePoint', value: -7, timeUnit: 'DAY' },
        },
      }),
    ).toEqual(['filter where clause relativeDateRange requires a timeZoneId']);

    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Employee'),
        where: {
          type: 'relativeDateRange',
          field: 'hiredAt',
          timeZoneId: 'Etc/UTC',
        },
      }),
    ).toEqual([
      'filter where clause relativeDateRange requires at least one of relativeStartTime / relativeEndTime',
    ]);
  });

  it('accepts Foundry geoShapeV2 filter clauses carrying geometry + spatialFilterMode instead of a value', () => {
    // BDD: Given a filter node using the backend-supported `geoShapeV2`
    // operator (pkg/oss/where/converter.go case "geoShapeV2" — Foundry
    // GeoShapeV2Query: the payload is a `geometry` union + `spatialFilterMode`,
    // there is NO `value` key), When the builder validates the tree, Then
    // neither an "unsupported operator" nor a "requires a value" error is
    // raised so the UI stays in sync with backend capability.
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Store'),
        where: {
          type: 'geoShapeV2',
          field: 'location',
          geometry: {
            type: 'geoJson',
            geoJson:
              '{"type":"Polygon","coordinates":[[[-80,35],[-70,35],[-70,45],[-80,45],[-80,35]]]}',
          },
          spatialFilterMode: 'WITHIN',
        },
      }),
    ).toEqual([]);

    // The envelope geometry variant is equally valid.
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Store'),
        where: {
          type: 'geoShapeV2',
          field: 'location',
          geometry: {
            type: 'envelope',
            topLeft: { latitude: 45, longitude: -80 },
            bottomRight: { latitude: 35, longitude: -70 },
          },
          spatialFilterMode: 'INTERSECTS',
        },
      }),
    ).toEqual([]);
  });

  it('flags geoShapeV2 filter clauses missing geometry or with an invalid spatialFilterMode', () => {
    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Store'),
        where: { type: 'geoShapeV2', field: 'location', spatialFilterMode: 'WITHIN' },
      }),
    ).toEqual(['filter where clause geoShapeV2 requires a geometry object']);

    expect(
      validateNode({
        id: '1',
        type: 'filter',
        objectSet: emptyBase('Store'),
        where: {
          type: 'geoShapeV2',
          field: 'location',
          geometry: { type: 'envelope' },
          spatialFilterMode: 'OVERLAPS' as never,
        },
      }),
    ).toEqual([
      'filter where clause geoShapeV2 requires a spatialFilterMode of INTERSECTS/DISJOINT/WITHIN/CONTAINS',
    ]);
  });

  it('flags union with fewer than 2 children', () => {
    const errs = validateNode({
      id: '1',
      type: 'union',
      objectSets: [emptyBase('Employee')],
    });
    expect(errs.length).toBeGreaterThan(0);
  });

  it('flags searchAround missing link', () => {
    const errs = validateNode({
      id: '1',
      type: 'searchAround',
      objectSet: emptyBase('Employee'),
      link: '',
    });
    expect(errs.length).toBeGreaterThan(0);
  });

  it('recurses into child nodes', () => {
    const errs = validateNode({
      id: '1',
      type: 'filter',
      objectSet: { id: '2', type: 'base', objectType: '' },
      where: { type: 'eq', field: 'x', value: 'y' },
    });
    expect(errs.length).toBeGreaterThan(0);
  });

  it('accepts a complete nearestNeighbors node', () => {
    const errs = validateNode({
      id: '1',
      type: 'nearestNeighbors',
      objectSet: emptyBase('Employee'),
      propertyIdentifier: { property: { apiName: 'embedding' } },
      numNeighbors: 5,
      query: { text: { value: 'find similar employees' } },
    });
    expect(errs).toEqual([]);
  });

  it('accepts a multi-column nearestNeighbors node via propertyIdentifiers', () => {
    const errs = validateNode({
      id: '1',
      type: 'nearestNeighbors',
      objectSet: emptyBase('Employee'),
      propertyIdentifiers: [
        { property: { apiName: 'title_vec' } },
        { property: { apiName: 'body_vec' } },
      ],
      fusionStrategy: 'rrf',
      numNeighbors: 5,
      query: { vector: { value: [0.1, 0.2] } },
    });
    expect(errs).toEqual([]);
  });

  it('flags a multi-column nearestNeighbors node with a blank column', () => {
    const errs = validateNode({
      id: '1',
      type: 'nearestNeighbors',
      objectSet: emptyBase('Employee'),
      propertyIdentifiers: [
        { property: { apiName: 'title_vec' } },
        { property: { apiName: '' } },
      ],
      numNeighbors: 5,
      query: { text: { value: 'q' } },
    });
    expect(errs).toContain('nearestNeighbors node requires an embedding property');
  });

  it('accepts a sample node with size and round-trips through definition', () => {
    const node: ObjectSetNode = {
      id: '1',
      type: 'sample',
      objectSet: emptyBase('Employee'),
      size: 25,
      seed: 7,
    };
    expect(validateNode(node)).toEqual([]);
    const def = nodeToDefinition(node);
    expect(def).toEqual({
      type: 'sample',
      objectSet: { type: 'base', objectType: 'Employee' },
      size: 25,
      seed: 7,
    });
  });

  it('omits seed from the sample definition when unset', () => {
    const def = nodeToDefinition({
      id: '1',
      type: 'sample',
      objectSet: emptyBase('Employee'),
      size: 5,
    });
    expect(def).toEqual({
      type: 'sample',
      objectSet: { type: 'base', objectType: 'Employee' },
      size: 5,
    });
    expect('seed' in def).toBe(false);
  });

  it('flags a sample node with size <= 0', () => {
    const errs = validateNode({
      id: '1',
      type: 'sample',
      objectSet: emptyBase('Employee'),
      size: 0,
    });
    expect(errs).toContain('sample node requires size > 0');
  });

  it('accepts a static node with object type and primary keys', () => {
    const errs = validateNode({
      id: '1',
      type: 'static',
      objectType: 'Employee',
      primaryKeys: ['e1'],
    });
    expect(errs).toEqual([]);
  });

  it('flags static node missing objectType', () => {
    const errs = validateNode({
      id: '1',
      type: 'static',
      objectType: '',
      primaryKeys: ['e1'],
    });
    expect(errs).toContain('static node requires an object type');
  });
});

describe('localStorageKey', () => {
  it('namespaces by ontology', () => {
    expect(localStorageKey('northwind')).toBe('weave:objectSets:northwind');
    expect(localStorageKey('chinook')).toBe('weave:objectSets:chinook');
  });
});
