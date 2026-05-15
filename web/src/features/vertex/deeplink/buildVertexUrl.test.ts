import { describe, it, expect } from 'vitest';
import { buildVertexUrlFromSelection } from './buildVertexUrl';

describe('VTX-074 buildVertexUrlFromSelection', () => {
  it('given_OneObject_when_Build_then_ObjectRidParam', () => {
    const url = buildVertexUrlFromSelection({
      objectRids: ['ri.ontology.main.object.airport.JFK'],
    });
    expect(url).toBe('/vertex/new?objectRid=ri.ontology.main.object.airport.JFK');
  });

  it('given_NoObjects_when_Build_then_BareNewRoute', () => {
    const url = buildVertexUrlFromSelection({ objectRids: [] });
    expect(url).toBe('/vertex/new');
  });

  it('given_FiveObjects_when_Build_then_ObjectSetRidPayload', () => {
    const url = buildVertexUrlFromSelection({
      objectRids: [
        'ri.ontology.main.object.airport.JFK',
        'ri.ontology.main.object.airport.LAX',
        'ri.ontology.main.object.airport.SFO',
        'ri.ontology.main.object.airport.ORD',
        'ri.ontology.main.object.airport.DFW',
      ],
    });
    expect(url).toMatch(/^\/vertex\/new\?objectRids=/);
    const p = new URLSearchParams(url.split('?')[1]);
    const rids = p.get('objectRids')!.split(',');
    expect(rids).toHaveLength(5);
    expect(rids[0]).toBe('ri.ontology.main.object.airport.JFK');
  });

  it('given_ObjectSetRidProvided_when_Build_then_UseObjectSetRid', () => {
    const url = buildVertexUrlFromSelection({
      objectSetRid: 'ri.oss.main.objectset.s1',
    });
    expect(url).toBe('/vertex/new?objectSetRid=ri.oss.main.objectset.s1');
  });

  it('given_BothObjectRidsAndSet_when_Build_then_ObjectSetWins', () => {
    const url = buildVertexUrlFromSelection({
      objectRids: ['ri.ontology.main.object.airport.JFK'],
      objectSetRid: 'ri.oss.main.objectset.s1',
    });
    expect(url).toBe('/vertex/new?objectSetRid=ri.oss.main.objectset.s1');
  });
});
