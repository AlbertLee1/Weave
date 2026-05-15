import { describe, expect, it, vi } from 'vitest';
import {
  buildOpenInExplorerUrl,
  buildOpenInQuiverUrl,
  openInNewTab,
} from './openLinks';

describe('VTX-035 buildOpenInQuiverUrl', () => {
  it('given_OntologyAndObjectRidAndProperty_when_Build_then_PathHasOntologyAndQueryHasBoth', () => {
    const url = buildOpenInQuiverUrl({
      ontology: 'flights',
      objectRid: 'ri.ontology.main.object.airport.JFK',
      property: 'throughput',
    });
    expect(url.startsWith('/quiver/flights')).toBe(true);
    const qs = new URLSearchParams(url.split('?')[1]);
    expect(qs.get('objectRid')).toBe('ri.ontology.main.object.airport.JFK');
    expect(qs.get('property')).toBe('throughput');
  });

  it('given_NoProperty_when_Build_then_OmitsPropertyParam', () => {
    const url = buildOpenInQuiverUrl({
      ontology: 'flights',
      objectRid: 'ri.ontology.main.object.airport.JFK',
    });
    const qs = new URLSearchParams(url.split('?')[1]);
    expect(qs.get('objectRid')).toBe('ri.ontology.main.object.airport.JFK');
    expect(qs.has('property')).toBe(false);
  });

  it('given_ObjectTypeAndPrimaryKey_when_Build_then_IncludesBothForSeriesPrefill', () => {
    const url = buildOpenInQuiverUrl({
      ontology: 'flights',
      objectRid: 'ri.ontology.main.object.airport.JFK',
      property: 'throughput',
      objectType: 'Airport',
      primaryKey: 'JFK',
    });
    const qs = new URLSearchParams(url.split('?')[1]);
    expect(qs.get('objectType')).toBe('Airport');
    expect(qs.get('primaryKey')).toBe('JFK');
  });

  it('given_PropertyWithSpecialChars_when_Build_then_QueryStringEncoded', () => {
    const url = buildOpenInQuiverUrl({
      ontology: 'flights',
      objectRid: 'ri.ontology.main.object.airport.JFK',
      property: 'avg latency (ms)',
    });
    const queryPart = url.split('?')[1];
    expect(queryPart).toContain('property=avg+latency+%28ms%29');
    const qs = new URLSearchParams(queryPart);
    expect(qs.get('property')).toBe('avg latency (ms)');
  });

  it('given_OntologyWithSlashlikeChars_when_Build_then_EncodedInPathSegment', () => {
    const url = buildOpenInQuiverUrl({
      ontology: 'flight/data',
      objectRid: 'ri.ontology.main.object.airport.JFK',
    });
    expect(url.startsWith('/quiver/flight%2Fdata')).toBe(true);
  });

  it('given_EmptyOntology_when_Build_then_Throws', () => {
    expect(() =>
      buildOpenInQuiverUrl({
        ontology: '',
        objectRid: 'ri.ontology.main.object.airport.JFK',
      }),
    ).toThrow(/ontology/i);
  });

  it('given_EmptyObjectRid_when_Build_then_Throws', () => {
    expect(() =>
      buildOpenInQuiverUrl({ ontology: 'flights', objectRid: '' }),
    ).toThrow(/objectRid/i);
  });
});

describe('VTX-035 buildOpenInExplorerUrl', () => {
  it('given_OntologyAndObjectRid_when_Build_then_QueryHasObjectRid', () => {
    const url = buildOpenInExplorerUrl({
      ontology: 'flights',
      objectRid: 'ri.ontology.main.object.airport.JFK',
    });
    expect(url).toBe(
      '/explorer/flights?objectRid=ri.ontology.main.object.airport.JFK',
    );
  });

  it('given_ObjectType_when_Build_then_UsesNestedRouteWithObjectType', () => {
    const url = buildOpenInExplorerUrl({
      ontology: 'flights',
      objectRid: 'ri.ontology.main.object.airport.JFK',
      objectType: 'Airport',
    });
    expect(url.startsWith('/explorer/flights/Airport?')).toBe(true);
    const qs = new URLSearchParams(url.split('?')[1]);
    expect(qs.get('objectRid')).toBe('ri.ontology.main.object.airport.JFK');
  });

  it('given_OntologyWithSpecialChars_when_Build_then_PathSegmentEncoded', () => {
    const url = buildOpenInExplorerUrl({
      ontology: 'flight data',
      objectRid: 'ri.ontology.main.object.airport.JFK',
    });
    expect(url.startsWith('/explorer/flight%20data?')).toBe(true);
  });

  it('given_EmptyOntology_when_Build_then_Throws', () => {
    expect(() =>
      buildOpenInExplorerUrl({
        ontology: '',
        objectRid: 'ri.ontology.main.object.airport.JFK',
      }),
    ).toThrow(/ontology/i);
  });

  it('given_EmptyObjectRid_when_Build_then_Throws', () => {
    expect(() =>
      buildOpenInExplorerUrl({ ontology: 'flights', objectRid: '' }),
    ).toThrow(/objectRid/i);
  });
});

describe('VTX-035 openInNewTab', () => {
  it('given_WindowOpenAvailable_when_Open_then_CallsOpenWithBlankAndNoopener', () => {
    const open = vi.fn();
    const fakeWindow = { open } as unknown as Window;
    openInNewTab('/quiver/flights?objectRid=ri.x', fakeWindow);
    expect(open).toHaveBeenCalledTimes(1);
    expect(open).toHaveBeenCalledWith(
      '/quiver/flights?objectRid=ri.x',
      '_blank',
      'noopener,noreferrer',
    );
  });

  it('given_UndefinedWindow_when_Open_then_NoThrowAndNoop', () => {
    expect(() =>
      openInNewTab('/quiver/flights?objectRid=ri.x', undefined),
    ).not.toThrow();
  });

  it('given_EmptyUrl_when_Open_then_Throws', () => {
    const open = vi.fn();
    const fakeWindow = { open } as unknown as Window;
    expect(() => openInNewTab('', fakeWindow)).toThrow(/url/i);
    expect(open).not.toHaveBeenCalled();
  });

  it('given_WindowWithoutOpen_when_Open_then_DoesNotThrow', () => {
    const fakeWindow = {} as unknown as Window;
    expect(() =>
      openInNewTab('/explorer/flights?objectRid=ri.x', fakeWindow),
    ).not.toThrow();
  });
});
