import { useMemo } from 'react';
import type { ObjectType, LinkType } from '../../api/types';

interface SchemaGraphProps {
  objectTypes: ObjectType[];
  linkTypes: LinkType[];
}

interface NodeLayout {
  apiName: string;
  label: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

interface EdgeLayout {
  from: string;
  to: string;
  label: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

const NODE_W = 160;
const NODE_H = 40;
const PADDING = 60;

/**
 * Circular layout: distribute object type nodes evenly around a circle.
 */
function computeLayout(
  objectTypes: ObjectType[],
  linkTypes: LinkType[],
): { nodes: NodeLayout[]; edges: EdgeLayout[]; width: number; height: number } {
  const count = objectTypes.length;

  if (count === 0) {
    return { nodes: [], edges: [], width: 0, height: 0 };
  }

  const radius = Math.max(140, count * 50);
  const cx = radius + PADDING + NODE_W / 2;
  const cy = radius + PADDING + NODE_H / 2;

  const nodeMap = new Map<string, NodeLayout>();

  const nodes: NodeLayout[] = objectTypes.map((ot, i) => {
    const angle = (2 * Math.PI * i) / count - Math.PI / 2;
    const x = cx + radius * Math.cos(angle);
    const y = cy + radius * Math.sin(angle);
    const node: NodeLayout = {
      apiName: ot.apiName,
      label: ot.displayName,
      x,
      y,
      width: NODE_W,
      height: NODE_H,
    };
    nodeMap.set(ot.apiName, node);
    return node;
  });

  const edges: EdgeLayout[] = linkTypes
    .filter((lt) => nodeMap.has(lt.objectTypeApiName) && nodeMap.has(lt.linkedObjectTypeApiName))
    .map((lt) => {
      const from = nodeMap.get(lt.objectTypeApiName)!;
      const to = nodeMap.get(lt.linkedObjectTypeApiName)!;
      return {
        from: lt.objectTypeApiName,
        to: lt.linkedObjectTypeApiName,
        label: lt.apiName,
        x1: from.x + from.width / 2,
        y1: from.y + from.height / 2,
        x2: to.x + to.width / 2,
        y2: to.y + to.height / 2,
      };
    });

  const totalW = (cx + radius + PADDING + NODE_W / 2) * 1;
  const totalH = (cy + radius + PADDING + NODE_H / 2) * 1;

  return {
    nodes,
    edges,
    width: Math.ceil(totalW),
    height: Math.ceil(totalH),
  };
}

export function SchemaGraph({ objectTypes, linkTypes }: SchemaGraphProps) {
  const { nodes, edges, width, height } = useMemo(
    () => computeLayout(objectTypes, linkTypes),
    [objectTypes, linkTypes],
  );

  if (nodes.length === 0) {
    return null;
  }

  return (
    <div className="overflow-auto w-full h-full" data-testid="schema-graph">
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className="select-none"
      >
        <defs>
          <marker
            id="arrowhead"
            markerWidth="8"
            markerHeight="6"
            refX="8"
            refY="3"
            orient="auto"
          >
            <polygon points="0 0, 8 3, 0 6" className="fill-text-muted" />
          </marker>
        </defs>

        {/* Edges */}
        {edges.map((edge, i) => {
          const midX = (edge.x1 + edge.x2) / 2;
          const midY = (edge.y1 + edge.y2) / 2;
          return (
            <g key={`edge-${i}`}>
              <line
                x1={edge.x1}
                y1={edge.y1}
                x2={edge.x2}
                y2={edge.y2}
                className="stroke-text-muted"
                strokeWidth={1}
                markerEnd="url(#arrowhead)"
              />
              <text
                x={midX}
                y={midY - 6}
                textAnchor="middle"
                className="fill-text-secondary text-[10px] font-mono"
              >
                {edge.label}
              </text>
            </g>
          );
        })}

        {/* Nodes */}
        {nodes.map((node) => (
          <g key={node.apiName}>
            <rect
              x={node.x}
              y={node.y}
              width={node.width}
              height={node.height}
              rx={6}
              className="fill-bg-tertiary stroke-border"
              strokeWidth={1}
            />
            <text
              x={node.x + node.width / 2}
              y={node.y + node.height / 2 + 4}
              textAnchor="middle"
              className="fill-accent-cyan text-xs font-mono"
            >
              {node.label}
            </text>
          </g>
        ))}
      </svg>
    </div>
  );
}
