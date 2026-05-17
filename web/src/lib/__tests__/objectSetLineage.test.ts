import { describe, it, expect } from 'vitest';
import {
  buildLineageTree,
  countLineageNodes,
} from '../objectSetLineage';
import type { ObjectSetDefinition } from '../../api/types';

describe('buildLineageTree', () => {
  it('returns a single-node tree for a base set', () => {
    const tree = buildLineageTree({ type: 'base', objectType: 'Employee' });
    expect(tree.type).toBe('base');
    expect(tree.objectType).toBe('Employee');
    expect(tree.children).toEqual([]);
    expect(countLineageNodes(tree)).toBe(1);
  });

  it('walks filter (objectSet child) into a 2-node tree', () => {
    const def: ObjectSetDefinition = {
      type: 'filter',
      where: { type: 'gt', field: 'age', value: 30 },
      objectSet: { type: 'base', objectType: 'Employee' },
    };
    const tree = buildLineageTree(def);
    expect(tree.type).toBe('filter');
    expect(tree.where).toEqual({ type: 'gt', field: 'age', value: 30 });
    expect(tree.children.length).toBe(1);
    expect(tree.children[0].type).toBe('base');
    expect(countLineageNodes(tree)).toBe(2);
  });

  it('walks union (objectSets children) and preserves their order', () => {
    const def: ObjectSetDefinition = {
      type: 'union',
      objectSets: [
        { type: 'base', objectType: 'Employee' },
        { type: 'base', objectType: 'Contractor' },
      ],
    };
    const tree = buildLineageTree(def);
    expect(tree.children.length).toBe(2);
    expect(tree.children[0].objectType).toBe('Employee');
    expect(tree.children[1].objectType).toBe('Contractor');
    expect(countLineageNodes(tree)).toBe(3);
  });

  it('captures searchAround link + direction', () => {
    const def: ObjectSetDefinition = {
      type: 'searchAround',
      objectSet: { type: 'base', objectType: 'Employee' },
      link: 'reports',
      direction: 'reverse',
    };
    const tree = buildLineageTree(def);
    expect(tree.type).toBe('searchAround');
    expect(tree.link).toBe('reports');
    expect(tree.direction).toBe('reverse');
  });

  it('captures withProperties derivedProperties', () => {
    const def: ObjectSetDefinition = {
      type: 'withProperties',
      objectSet: { type: 'base', objectType: 'Order' },
      derivedProperties: [
        {
          name: 'totalItems',
          link: 'orderLines',
          metric: 'sum',
          field: 'qty',
        },
      ],
    };
    const tree = buildLineageTree(def);
    expect(tree.type).toBe('withProperties');
    expect(tree.derivedProperties).toHaveLength(1);
    expect(tree.derivedProperties?.[0].name).toBe('totalItems');
  });

  it('handles null / undefined defensively', () => {
    const tree = buildLineageTree(null);
    expect(tree.type).toBe('unknown');
    expect(tree.children).toEqual([]);
  });
});
