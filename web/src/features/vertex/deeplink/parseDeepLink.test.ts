import { describe, it, expect } from 'vitest';
import { parseDeepLink } from './parseDeepLink';

describe('VTX-072 parseDeepLink', () => {
  it('given_NoParams_when_Parse_then_EmptyConfig', () => {
    const cfg = parseDeepLink(new URLSearchParams(''));
    expect(cfg).toEqual({});
  });

  it('given_ObjectRid_when_Parse_then_ObjectRidPresent', () => {
    const cfg = parseDeepLink(
      new URLSearchParams('?objectRid=ri.ontology.main.object.airport.JFK'),
    );
    expect(cfg.objectRid).toBe('ri.ontology.main.object.airport.JFK');
  });

  it('given_ObjectSetRid_when_Parse_then_ObjectSetRidPresent', () => {
    const cfg = parseDeepLink(new URLSearchParams('?objectSetRid=ri.oss.main.objectset.s1'));
    expect(cfg.objectSetRid).toBe('ri.oss.main.objectset.s1');
  });

  it('given_SearchAroundFnRid_when_Parse_then_FieldPresent', () => {
    const cfg = parseDeepLink(
      new URLSearchParams('?searchAroundFnRid=ri.functions.main.function.fn1'),
    );
    expect(cfg.searchAroundFnRid).toBe('ri.functions.main.function.fn1');
  });

  it('given_SelectedTimeISO_when_Parse_then_EpochMs', () => {
    const cfg = parseDeepLink(new URLSearchParams('?selectedTime=2026-05-14T00:00:00Z'));
    expect(cfg.selectedTimeMs).toBe(new Date('2026-05-14T00:00:00Z').getTime());
  });

  it('given_SelectedTimeInvalid_when_Parse_then_Undefined', () => {
    const cfg = parseDeepLink(new URLSearchParams('?selectedTime=not-a-date'));
    expect(cfg.selectedTimeMs).toBeUndefined();
  });

  it('given_StartEndTime_when_Parse_then_TimeWindow', () => {
    const cfg = parseDeepLink(
      new URLSearchParams('?startTime=2026-05-01T00:00:00Z&endTime=2026-05-14T00:00:00Z'),
    );
    expect(cfg.timeWindow).toEqual({
      from: new Date('2026-05-01T00:00:00Z').getTime(),
      to: new Date('2026-05-14T00:00:00Z').getTime(),
    });
  });

  it('given_SelectObjectRid_when_Parse_then_Present', () => {
    const cfg = parseDeepLink(
      new URLSearchParams('?selectObjectRid=ri.ontology.main.object.airport.JFK'),
    );
    expect(cfg.selectObjectRid).toBe('ri.ontology.main.object.airport.JFK');
  });

  it('given_ObjectRidNotRid_when_Parse_then_Rejected', () => {
    const cfg = parseDeepLink(new URLSearchParams('?objectRid=garbage'));
    expect(cfg.objectRid).toBeUndefined();
  });

  it('given_AllParams_when_Parse_then_AllPresent', () => {
    const p = new URLSearchParams();
    p.set('objectRid', 'ri.ontology.main.object.airport.JFK');
    p.set('objectSetRid', 'ri.oss.main.objectset.s1');
    p.set('searchAroundFnRid', 'ri.functions.main.function.fn1');
    p.set('selectedTime', '2026-05-14T00:00:00Z');
    const cfg = parseDeepLink(p);
    expect(cfg.objectRid).toBeDefined();
    expect(cfg.objectSetRid).toBeDefined();
    expect(cfg.searchAroundFnRid).toBeDefined();
    expect(cfg.selectedTimeMs).toBeDefined();
  });
});
