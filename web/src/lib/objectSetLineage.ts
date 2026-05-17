import type { ObjectSetDefinition, DerivedPropertyDef } from '../api/types';

export interface LineageTreeNode {
  id: string;
  type: string;
  objectType?: string;
  link?: string;
  direction?: string;
  reference?: string;
  interfaceType?: string;
  interfaceLink?: string;
  input?: string;
  size?: number;
  seed?: number;
  derivedProperties?: DerivedPropertyDef[];
  where?: unknown;
  children: LineageTreeNode[];
}

// buildLineageTree walks an ObjectSetDefinition and produces a recursive tree
// where each node's children are its input ObjectSets. The shape mirrors the
// backend pkg/oss/objectset.GetObjectSetLineage walk so the client can render
// the operation chain without a server round-trip — useful for saved
// ObjectSets that haven't been materialized as temporary RIDs.
export function buildLineageTree(
  def: ObjectSetDefinition | null | undefined,
): LineageTreeNode {
  let counter = 0;
  function walk(d: ObjectSetDefinition | null | undefined): LineageTreeNode {
    if (!d) {
      return { id: `n${counter++}`, type: 'unknown', children: [] };
    }
    const children: LineageTreeNode[] = [];
    const wd = d as unknown as Record<string, unknown>;
    if (
      'objectSet' in wd &&
      wd.objectSet &&
      typeof wd.objectSet === 'object'
    ) {
      children.push(walk(wd.objectSet as ObjectSetDefinition));
    }
    if (Array.isArray((wd as { objectSets?: unknown }).objectSets)) {
      for (const c of (wd as { objectSets: ObjectSetDefinition[] }).objectSets) {
        children.push(walk(c));
      }
    }
    const node: LineageTreeNode = {
      id: `n${counter++}`,
      type: d.type,
      children,
    };
    switch (d.type) {
      case 'base':
      case 'static':
      case 'asType':
        node.objectType = (d as { objectType: string }).objectType;
        break;
      case 'filter':
        node.where = (d as { where: unknown }).where;
        break;
      case 'searchAround':
        node.link = (d as { link: string }).link;
        node.direction = (d as { direction?: string }).direction;
        break;
      case 'reference':
        node.reference = (d as { reference: string }).reference;
        break;
      case 'interfaceBase':
        node.interfaceType = (d as { interfaceType: string }).interfaceType;
        break;
      case 'interfaceLinkSearchAround':
        node.interfaceLink = (d as { interfaceLink: string }).interfaceLink;
        break;
      case 'methodInput':
        node.input = (d as { input: string }).input;
        break;
      case 'withProperties': {
        const dp = (d as { derivedProperties?: DerivedPropertyDef[] })
          .derivedProperties;
        if (dp && dp.length > 0) node.derivedProperties = dp;
        break;
      }
      default:
        break;
    }
    return node;
  }
  return walk(def);
}

// countLineageNodes returns the total number of nodes in a lineage tree,
// useful for displaying "N steps" badges in the UI.
export function countLineageNodes(node: LineageTreeNode): number {
  return 1 + node.children.reduce((sum, c) => sum + countLineageNodes(c), 0);
}
