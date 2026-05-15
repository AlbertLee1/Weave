import { describe, it, expect } from 'vitest';
import {
  extractExtendedLabels,
  type ExtendedLabel,
} from './extendedLabels';

describe('VTX-019 extractExtendedLabels', () => {
  it('Given_layerWith3LabelsAcrossPropertyTimeSeriesMeasure_When_extract_Then_returns3Cards', () => {
    const payload = {
      layers: [
        {
          id: 'l-airports',
          extendedLabels: [
            { kind: 'property', property: 'onTimePct', label: 'On-time %' },
            { kind: 'timeSeries', property: 'temperatureSeries' },
            { kind: 'measure', functionRid: 'ri.functions.main.fn.avgDelay' },
          ],
          objects: [
            {
              objectRid: 'ri.ontology.main.object.airport.JFK',
              properties: { name: 'JFK', onTimePct: 92 },
            },
          ],
        },
      ],
    };
    const out: ExtendedLabel[] = extractExtendedLabels(
      payload,
      'ri.ontology.main.object.airport.JFK',
    );
    expect(out).toHaveLength(3);
    expect(out[0]).toMatchObject({
      kind: 'property',
      label: 'On-time %',
      value: '92',
    });
    expect(out[1]).toMatchObject({
      kind: 'timeSeries',
      label: 'temperatureSeries',
    });
    expect(out[2]).toMatchObject({
      kind: 'measure',
      label: 'avgDelay',
    });
    // Keys must be unique so React's reconciler can scale.
    const keys = new Set(out.map((l) => l.key));
    expect(keys.size).toBe(3);
  });

  it('Given_objectNotInAnyLayer_When_extract_Then_returnsEmptyArray', () => {
    const payload = {
      layers: [
        {
          extendedLabels: [{ kind: 'property', property: 'name' }],
          objects: [{ objectRid: 'ri.a', properties: { name: 'A' } }],
        },
      ],
    };
    expect(extractExtendedLabels(payload, 'ri.unknown')).toEqual([]);
  });

  it('Given_layerWithNoExtendedLabels_When_extract_Then_returnsEmptyArray', () => {
    const payload = {
      layers: [
        {
          objects: [{ objectRid: 'ri.a', properties: { name: 'A' } }],
        },
      ],
    };
    expect(extractExtendedLabels(payload, 'ri.a')).toEqual([]);
  });

  it('Given_propertyValueMissing_When_extract_Then_propertyLabelValueIsDash', () => {
    const payload = {
      layers: [
        {
          extendedLabels: [{ kind: 'property', property: 'onTimePct' }],
          objects: [
            { objectRid: 'ri.a', properties: { name: 'A' } },
          ],
        },
      ],
    };
    const out = extractExtendedLabels(payload, 'ri.a');
    expect(out[0].value).toBe('—');
  });

  it('Given_payloadIsNullOrMalformed_When_extract_Then_returnsEmptyArrayWithoutThrowing', () => {
    expect(extractExtendedLabels(null, 'ri.a')).toEqual([]);
    expect(extractExtendedLabels(undefined, 'ri.a')).toEqual([]);
    expect(extractExtendedLabels({ layers: 'not-array' }, 'ri.a')).toEqual([]);
    expect(extractExtendedLabels({ layers: [{}] }, 'ri.a')).toEqual([]);
  });

  it('Given_unknownKind_When_extract_Then_skipsThatLabel', () => {
    const payload = {
      layers: [
        {
          extendedLabels: [
            { kind: 'property', property: 'name' },
            { kind: 'unknown', property: 'foo' },
            { kind: 'measure', functionRid: 'ri.fn.X' },
          ],
          objects: [{ objectRid: 'ri.a', properties: { name: 'A' } }],
        },
      ],
    };
    const out = extractExtendedLabels(payload, 'ri.a');
    expect(out).toHaveLength(2);
    expect(out.map((l) => l.kind)).toEqual(['property', 'measure']);
  });

  it('Given_objectInTwoLayersBothWithLabels_When_extract_Then_returnsLabelsFromFirstLayerMatch', () => {
    // Mirrors payloadToGraph's "first occurrence wins" dedupe rule.
    const payload = {
      layers: [
        {
          extendedLabels: [{ kind: 'property', property: 'name' }],
          objects: [{ objectRid: 'ri.a', properties: { name: 'First' } }],
        },
        {
          extendedLabels: [
            { kind: 'property', property: 'name' },
            { kind: 'measure', functionRid: 'ri.fn.X' },
          ],
          objects: [{ objectRid: 'ri.a', properties: { name: 'Second' } }],
        },
      ],
    };
    const out = extractExtendedLabels(payload, 'ri.a');
    expect(out).toHaveLength(1);
    expect(out[0].value).toBe('First');
  });

  it('Given_payloadLabelExplicitLabelText_When_extract_Then_usesItVerbatim', () => {
    const payload = {
      layers: [
        {
          extendedLabels: [
            { kind: 'property', property: 'iata', label: 'IATA Code' },
            { kind: 'timeSeries', property: 'temp', label: 'Temp °C' },
            { kind: 'measure', functionRid: 'ri.fn.unused', label: 'Avg Delay' },
          ],
          objects: [
            { objectRid: 'ri.a', properties: { iata: 'JFK' } },
          ],
        },
      ],
    };
    const labels = extractExtendedLabels(payload, 'ri.a');
    expect(labels.map((l) => l.label)).toEqual([
      'IATA Code',
      'Temp °C',
      'Avg Delay',
    ]);
  });
});
