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
