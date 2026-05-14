export interface ParameterizedField {
  name: string;
  type: 'rid' | 'string' | 'number';
}

export interface GraphSnapshot {
  nodes: string[];
  edges: Array<{ src: string; dst: string }>;
}

export interface SaveAsTemplateInput {
  name: string;
  graphSnapshot: GraphSnapshot;
  parameterizedFields: ParameterizedField[];
}

export interface SaveAsTemplatePayload {
  name: string;
  parameters: Array<ParameterizedField & { required: boolean }>;
  graphSnapshot: GraphSnapshot;
}

export function buildSaveAsTemplatePayload(
  input: SaveAsTemplateInput,
): SaveAsTemplatePayload {
  const name = input.name.trim();
  if (name === '') throw new Error('buildSaveAsTemplatePayload: name required');
  const seen = new Set<string>();
  for (const f of input.parameterizedFields) {
    if (seen.has(f.name)) {
      throw new Error(`buildSaveAsTemplatePayload: duplicate field ${f.name}`);
    }
    seen.add(f.name);
  }
  return {
    name,
    parameters: input.parameterizedFields.map((f) => ({ ...f, required: true })),
    graphSnapshot: input.graphSnapshot,
  };
}
