import { describe, it, expect } from 'vitest';
import { buildSaveAsTemplatePayload } from './saveAsTemplate';

describe('VTX-076 buildSaveAsTemplatePayload', () => {
  it('given_NameAndNoParameterized_when_Build_then_PayloadWithEmptyParameters', () => {
    const payload = buildSaveAsTemplatePayload({
      name: 'My template',
      graphSnapshot: { nodes: ['a', 'b'], edges: [] },
      parameterizedFields: [],
    });
    expect(payload.name).toBe('My template');
    expect(payload.parameters).toEqual([]);
    expect(payload.graphSnapshot).toEqual({ nodes: ['a', 'b'], edges: [] });
  });

  it('given_ParameterizedFields_when_Build_then_ParameterListMatches', () => {
    const payload = buildSaveAsTemplatePayload({
      name: 't',
      graphSnapshot: { nodes: [], edges: [] },
      parameterizedFields: [
        { name: 'hubRid', type: 'rid' },
        { name: 'depth', type: 'number' },
      ],
    });
    expect(payload.parameters).toEqual([
      { name: 'hubRid', type: 'rid', required: true },
      { name: 'depth', type: 'number', required: true },
    ]);
  });

  it('given_EmptyName_when_Build_then_Throws', () => {
    expect(() =>
      buildSaveAsTemplatePayload({
        name: '   ',
        graphSnapshot: { nodes: [], edges: [] },
        parameterizedFields: [],
      }),
    ).toThrow(/name/);
  });

  it('given_DuplicateFieldNames_when_Build_then_Throws', () => {
    expect(() =>
      buildSaveAsTemplatePayload({
        name: 't',
        graphSnapshot: { nodes: [], edges: [] },
        parameterizedFields: [
          { name: 'a', type: 'rid' },
          { name: 'a', type: 'string' },
        ],
      }),
    ).toThrow(/duplicate/i);
  });

  it('given_NameWithWhitespace_when_Build_then_Trimmed', () => {
    const payload = buildSaveAsTemplatePayload({
      name: '  Hub-and-spoke  ',
      graphSnapshot: { nodes: [], edges: [] },
      parameterizedFields: [],
    });
    expect(payload.name).toBe('Hub-and-spoke');
  });
});
