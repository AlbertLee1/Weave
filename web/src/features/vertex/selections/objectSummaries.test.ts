import { describe, it, expect } from 'vitest';

import { payloadToObjectSummaries } from './objectSummaries';

describe('VTX-020 payloadToObjectSummaries', () => {
  it('Given_payloadWithObjects_When_project_Then_returnsMapKeyedByRidWithLabelAndProps', () => {
    const payload = {
      layers: [
        {
          objects: [
            {
              objectRid: 'ri.airport.JFK',
              properties: { name: 'JFK', city: 'New York', onTimePct: 92 },
            },
            {
              objectRid: 'ri.airport.LHR',
              properties: { name: 'LHR', city: 'London' },
            },
          ],
        },
      ],
    };
    const summaries = payloadToObjectSummaries(payload);
    expect(summaries.size).toBe(2);
    const jfk = summaries.get('ri.airport.JFK');
    expect(jfk?.label).toBe('JFK');
    expect(jfk?.properties.city).toBe('New York');
  });

  it('Given_objectWithoutNameProperty_When_project_Then_labelFallsBackToRid', () => {
    const payload = {
      layers: [
        { objects: [{ objectRid: 'ri.x', properties: { city: 'NYC' } }] },
      ],
    };
    expect(payloadToObjectSummaries(payload).get('ri.x')?.label).toBe('ri.x');
  });

  it('Given_objectAppearsInTwoLayers_When_project_Then_keepsFirstLayerProperties', () => {
    const payload = {
      layers: [
        { objects: [{ objectRid: 'ri.x', properties: { name: 'first', extra: 'A' } }] },
        { objects: [{ objectRid: 'ri.x', properties: { name: 'second', extra: 'B' } }] },
      ],
    };
    const summary = payloadToObjectSummaries(payload).get('ri.x');
    expect(summary?.label).toBe('first');
    expect(summary?.properties.extra).toBe('A');
  });

  it('Given_malformedPayload_When_project_Then_returnsEmptyMapWithoutThrowing', () => {
    expect(payloadToObjectSummaries(null).size).toBe(0);
    expect(payloadToObjectSummaries('not an object').size).toBe(0);
    expect(payloadToObjectSummaries({ layers: 'not array' }).size).toBe(0);
    expect(payloadToObjectSummaries({ layers: [{ objects: [{}] }] }).size).toBe(0);
  });

  // VTX-021: surface the api-name metadata + primary key so the right
  // sidebar can call OSS get / activity / timeseries.
  it('Given_layerCarriesOntologyAndObjectType_When_project_Then_summaryCarriesApiNamesAndPrimaryKey', () => {
    const payload = {
      layers: [
        {
          ontology: 'flights',
          objectType: 'Airport',
          objects: [
            {
              objectRid: 'ri.ontology.main.object.airport.JFK',
              properties: { name: 'JFK' },
            },
          ],
        },
      ],
    };
    const summary = payloadToObjectSummaries(payload).get(
      'ri.ontology.main.object.airport.JFK',
    );
    expect(summary?.ontologyApiName).toBe('flights');
    expect(summary?.objectType).toBe('Airport');
    expect(summary?.primaryKey).toBe('JFK');
  });

  it('Given_objectExplicitPrimaryKeyProperty_When_project_Then_summaryUsesIt', () => {
    const payload = {
      layers: [
        {
          ontology: 'flights',
          objectType: 'Airport',
          objects: [
            {
              objectRid: 'ri.ontology.main.object.airport.row-42',
              properties: { name: 'A', primaryKey: 'KSFO' },
            },
          ],
        },
      ],
    };
    const summary = payloadToObjectSummaries(payload).get(
      'ri.ontology.main.object.airport.row-42',
    );
    expect(summary?.primaryKey).toBe('KSFO');
  });

  it('Given_layerWithoutOntologyApiName_When_project_Then_apiNamesAreUndefined', () => {
    const payload = {
      layers: [
        {
          objects: [{ objectRid: 'ri.airport.JFK', properties: { name: 'JFK' } }],
        },
      ],
    };
    const summary = payloadToObjectSummaries(payload).get('ri.airport.JFK');
    expect(summary?.ontologyApiName).toBeUndefined();
    expect(summary?.objectType).toBeUndefined();
    // Primary key still derived from the rid's last `.`-segment.
    expect(summary?.primaryKey).toBe('JFK');
  });
});
