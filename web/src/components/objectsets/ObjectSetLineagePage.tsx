import { useCallback, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router';
import { useSavedObjectSets } from '../../hooks/useObjectSets';
import {
  buildLineageTree,
  countLineageNodes,
  type LineageTreeNode,
} from '../../lib/objectSetLineage';
import type { DerivedPropertyDef } from '../../api/types';
import { EmptyState } from '../common/EmptyState';

function nodeLabel(node: LineageTreeNode): string {
  switch (node.type) {
    case 'base':
    case 'static':
    case 'asType':
      return `${node.type}: ${node.objectType ?? '(unknown)'}`;
    case 'filter':
      return 'filter';
    case 'union':
    case 'intersect':
    case 'subtract':
      return node.type;
    case 'searchAround':
      return `searchAround ${node.link ?? ''}${
        node.direction ? ` (${node.direction})` : ''
      }`;
    case 'reference':
      return `reference: ${node.reference ?? ''}`;
    case 'withProperties':
      return `withProperties${
        node.derivedProperties && node.derivedProperties.length > 0
          ? ` (+${node.derivedProperties.length})`
          : ''
      }`;
    case 'interfaceBase':
      return `interfaceBase: ${node.interfaceType ?? ''}`;
    case 'interfaceLinkSearchAround':
      return `interfaceLinkSearchAround: ${node.interfaceLink ?? ''}`;
    case 'methodInput':
      return `methodInput: ${node.input ?? ''}`;
    case 'sample':
      return `sample${node.size != null ? ` size=${node.size}` : ''}`;
    case 'asBaseObjectTypes':
      return 'asBaseObjectTypes';
    case 'nearestNeighbors':
      return 'nearestNeighbors';
    default:
      return node.type;
  }
}

function describeWhere(where: unknown): string {
  if (where == null) return '';
  try {
    return JSON.stringify(where);
  } catch {
    return String(where);
  }
}

function describeDerivedProperties(dps: DerivedPropertyDef[] | undefined): string {
  if (!dps || dps.length === 0) return '';
  return dps
    .map(
      (dp) =>
        `${dp.name}=${dp.metric}(${dp.field ?? '*'}) via ${dp.link}${
          dp.direction ? `[${dp.direction}]` : ''
        }`,
    )
    .join('; ');
}

interface TreeNodeViewProps {
  node: LineageTreeNode;
  depth: number;
}

function TreeNodeView({ node, depth }: TreeNodeViewProps) {
  const isLeaf = node.children.length === 0;
  return (
    <li
      data-testid="objectset-lineage-tree-node"
      data-node-type={node.type}
      data-node-id={node.id}
      data-depth={String(depth)}
      data-is-leaf={isLeaf ? 'true' : 'false'}
      className="flex flex-col"
    >
      <div
        className="px-2 py-1.5 rounded border border-border bg-bg-tertiary font-mono text-xs flex flex-col gap-0.5"
        style={{ marginLeft: depth * 16 }}
      >
        <div className="flex items-center gap-2">
          <span
            data-testid="objectset-lineage-node-type"
            className={`text-[10px] uppercase tracking-wider px-1 py-0.5 rounded ${
              isLeaf
                ? 'bg-accent-cyan/10 text-accent-cyan'
                : 'bg-accent-magenta/10 text-accent-magenta'
            }`}
          >
            {node.type}
          </span>
          <span className="text-text-primary truncate">{nodeLabel(node)}</span>
        </div>
        {node.where != null && (
          <div
            data-testid="objectset-lineage-node-where"
            className="text-text-secondary truncate"
            title={describeWhere(node.where)}
          >
            where {describeWhere(node.where)}
          </div>
        )}
        {node.derivedProperties && node.derivedProperties.length > 0 && (
          <div
            data-testid="objectset-lineage-node-derived"
            className="text-text-secondary truncate"
            title={describeDerivedProperties(node.derivedProperties)}
          >
            derived: {describeDerivedProperties(node.derivedProperties)}
          </div>
        )}
      </div>
      {node.children.length > 0 && (
        <ul
          data-testid="objectset-lineage-tree-children"
          className="flex flex-col gap-1 mt-1"
        >
          {node.children.map((c) => (
            <TreeNodeView key={c.id} node={c} depth={depth + 1} />
          ))}
        </ul>
      )}
    </li>
  );
}

export function ObjectSetLineagePage() {
  const { ontology } = useParams<{ ontology: string }>();
  const ontologyApiName = ontology ?? '';

  const { items: saved } = useSavedObjectSets(ontologyApiName);

  const [selectedSavedId, setSelectedSavedId] = useState<string>('');

  const selected = useMemo(
    () => saved.find((s) => s.id === selectedSavedId),
    [saved, selectedSavedId],
  );

  const tree = useMemo(() => {
    if (!selected) return null;
    return buildLineageTree(selected.def);
  }, [selected]);

  const totalNodes = useMemo(
    () => (tree ? countLineageNodes(tree) : 0),
    [tree],
  );

  const handlePick = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      setSelectedSavedId(e.target.value);
    },
    [],
  );

  if (!ontologyApiName) {
    return (
      <div
        data-testid="objectset-lineage-no-ontology"
        className="flex items-center justify-center h-full"
      >
        <EmptyState
          title="No ontology selected"
          description="Pick an ontology to inspect ObjectSet lineage."
        />
      </div>
    );
  }

  return (
    <div
      data-testid="objectset-lineage-page"
      className="flex flex-col h-full overflow-hidden"
    >
      <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-bg-primary">
        <div>
          <h1 className="text-base font-sans font-semibold text-text-primary">
            Object Set Lineage
          </h1>
          <p className="text-xs font-mono text-text-secondary mt-0.5">
            {ontologyApiName}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link
            to={`/objectsets/${ontologyApiName}`}
            className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
          >
            Composer
          </Link>
          <Link
            to={`/objectsets/${ontologyApiName}/snapshots`}
            className="bg-bg-tertiary border border-border text-text-primary px-3 py-1.5 rounded text-xs font-mono hover:bg-bg-elevated"
          >
            Snapshots
          </Link>
        </div>
      </div>

      <div className="px-4 py-3 border-b border-border flex flex-col gap-3 lg:flex-row lg:items-end">
        <div className="flex-1 flex flex-col gap-1">
          <label
            htmlFor="lineage-saved-pick"
            className="text-xs font-sans text-text-secondary uppercase tracking-wider"
          >
            Saved Object Set
          </label>
          <select
            id="lineage-saved-pick"
            aria-label="Saved Object Set"
            data-testid="objectset-lineage-saved-pick"
            value={selectedSavedId}
            onChange={handlePick}
            className="bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none"
          >
            <option value="">-- pick a saved set --</option>
            {saved.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
        {tree && (
          <div
            data-testid="objectset-lineage-counts"
            data-node-count={String(totalNodes)}
            className="text-xs font-mono text-text-secondary self-start lg:self-end"
          >
            {totalNodes} step{totalNodes === 1 ? '' : 's'}
          </div>
        )}
      </div>

      <div className="flex-1 overflow-auto p-4">
        {!saved.length ? (
          <div data-testid="objectset-lineage-no-saved">
            <EmptyState
              title="No saved Object Sets"
              description="Save an Object Set in the Composer first to inspect its lineage."
            />
          </div>
        ) : !tree ? (
          <div data-testid="objectset-lineage-pending">
            <EmptyState
              title="Pick a saved Object Set"
              description="The operation chain that produces the set will render as a tree."
            />
          </div>
        ) : (
          <div data-testid="objectset-lineage-tree" className="max-w-4xl">
            <ul className="flex flex-col gap-1">
              <TreeNodeView node={tree} depth={0} />
            </ul>
          </div>
        )}
      </div>
    </div>
  );
}
