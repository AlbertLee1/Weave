import { describe, it, expect } from 'vitest';
import {
  buildInstantiatePayload,
  validateTemplateParams,
  type TemplateSchema,
} from './instantiate';

const schema: TemplateSchema = {
  rid: 'ri.vertex.main.template.t1',
  name: 'Hub-and-spoke',
  parameters: [
    { name: 'hubRid', type: 'rid', required: true },
    { name: 'depth', type: 'number', required: true, min: 1, max: 5 },
    { name: 'label', type: 'string', required: false },
  ],
};

describe('VTX-073 validateTemplateParams', () => {
  it('given_AllRequiredPresent_when_Validate_then_Valid', () => {
    const r = validateTemplateParams(schema, {
      hubRid: 'ri.ontology.main.object.airport.JFK',
      depth: 2,
    });
    expect(r.valid).toBe(true);
  });

  it('given_MissingRequired_when_Validate_then_Invalid', () => {
    const r = validateTemplateParams(schema, { depth: 2 });
    if (r.valid) throw new Error('should be invalid');
    expect(r.errors.hubRid).toMatch(/required/i);
  });

  it('given_NonRidForRidParam_when_Validate_then_Invalid', () => {
    const r = validateTemplateParams(schema, {
      hubRid: 'not-a-rid',
      depth: 2,
    });
    if (r.valid) throw new Error('should be invalid');
    expect(r.errors.hubRid).toBeDefined();
  });

  it('given_NumberOutOfRange_when_Validate_then_Invalid', () => {
    const r = validateTemplateParams(schema, {
      hubRid: 'ri.ontology.main.object.airport.JFK',
      depth: 99,
    });
    if (r.valid) throw new Error('should be invalid');
    expect(r.errors.depth).toMatch(/max/i);
  });

  it('given_NumberBelowMin_when_Validate_then_Invalid', () => {
    const r = validateTemplateParams(schema, {
      hubRid: 'ri.ontology.main.object.airport.JFK',
      depth: 0,
    });
    if (r.valid) throw new Error('should be invalid');
    expect(r.errors.depth).toMatch(/min/i);
  });

  it('given_OptionalMissing_when_Validate_then_Valid', () => {
    const r = validateTemplateParams(schema, {
      hubRid: 'ri.ontology.main.object.airport.JFK',
      depth: 2,
    });
    expect(r.valid).toBe(true);
  });
});

describe('VTX-073 buildInstantiatePayload', () => {
  it('given_ValidParams_when_Build_then_RidAndArgs', () => {
    const payload = buildInstantiatePayload(schema, {
      hubRid: 'ri.ontology.main.object.airport.JFK',
      depth: 2,
      label: 'NE Hubs',
    });
    expect(payload).toEqual({
      templateRid: 'ri.vertex.main.template.t1',
      args: {
        hubRid: 'ri.ontology.main.object.airport.JFK',
        depth: 2,
        label: 'NE Hubs',
      },
    });
  });

  it('given_UnknownParam_when_Build_then_Stripped', () => {
    const payload = buildInstantiatePayload(schema, {
      hubRid: 'ri.ontology.main.object.airport.JFK',
      depth: 2,
      unrelated: 'x',
    } as Record<string, unknown>);
    expect(Object.keys(payload.args).sort()).toEqual(['depth', 'hubRid']);
  });
});
