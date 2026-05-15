export interface GraphPayload {
  name: string;
  nodes: Array<{ id: string }>;
  edges: Array<{ src: string; dst: string }>;
  styling?: unknown;
}

export function duplicateGraphPayload(p: GraphPayload): GraphPayload {
  const base = p.name === '' ? '(Copy)' : `${p.name} (Copy)`;
  // structuredClone is part of the ES2022 surface available in Vite + Node
  // 18+; cheaper to maintain than a recursive clone helper.
  return {
    ...structuredClone(p),
    name: base,
  };
}
