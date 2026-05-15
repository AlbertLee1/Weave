import { describe, it, expect } from 'vitest';
import {
  planParameterizedSearchArounds,
} from './parameterizedSearchAround';

const fnRid = 'ri.functions.main.function.fn1';

describe('VTX-075 planParameterizedSearchArounds', () => {
  it('given_3Objects_when_Plan_then_3Calls', () => {
    const plan = planParameterizedSearchArounds({
      searchAroundFnRid: fnRid,
      objectRids: [
        'ri.ontology.main.object.airport.JFK',
        'ri.ontology.main.object.airport.LAX',
        'ri.ontology.main.object.airport.SFO',
      ],
      sharedParams: { depth: 2 },
    });
    expect(plan).toHaveLength(3);
    expect(plan[0]).toEqual({
      functionRid: fnRid,
      objectRid: 'ri.ontology.main.object.airport.JFK',
      params: { depth: 2 },
    });
  });

  it('given_NoSharedParams_when_Plan_then_EmptyParamsPerCall', () => {
    const plan = planParameterizedSearchArounds({
      searchAroundFnRid: fnRid,
      objectRids: ['ri.ontology.main.object.airport.JFK'],
    });
    expect(plan[0].params).toEqual({});
  });

  it('given_EmptyObjectRids_when_Plan_then_EmptyArray', () => {
    const plan = planParameterizedSearchArounds({
      searchAroundFnRid: fnRid,
      objectRids: [],
    });
    expect(plan).toEqual([]);
  });

  it('given_DuplicateObjectRids_when_Plan_then_Deduplicated', () => {
    const plan = planParameterizedSearchArounds({
      searchAroundFnRid: fnRid,
      objectRids: [
        'ri.ontology.main.object.airport.JFK',
        'ri.ontology.main.object.airport.JFK',
        'ri.ontology.main.object.airport.LAX',
      ],
    });
    expect(plan).toHaveLength(2);
  });

  it('given_NoFnRid_when_Plan_then_Throws', () => {
    expect(() =>
      planParameterizedSearchArounds({
        searchAroundFnRid: '',
        objectRids: ['ri.ontology.main.object.airport.JFK'],
      }),
    ).toThrow(/searchAroundFnRid/);
  });
});
