import { describe, it, expect } from 'vitest';
import {
  annotateEventsWithColor,
  filterEventsByCategory,
  type RawEvent,
} from './eventBars';

const events: RawEvent[] = [
  { rid: 'fd1', objectType: 'FlightDelay', start: 1, end: 2 },
  { rid: 'fd2', objectType: 'FlightDelay', start: 3, end: 4 },
  { rid: 'm1', objectType: 'Maintenance', start: 5, end: 6 },
  { rid: 'w1', objectType: 'Weather', start: 7, end: 8 },
];

describe('VTX-082 annotateEventsWithColor', () => {
  it('given_3DistinctTypes_when_Annotate_then_DistinctColorsPerType', () => {
    const annotated = annotateEventsWithColor(events, { paletteSeed: 'test' });
    const flightColor = annotated.find((e) => e.rid === 'fd1')!.color;
    const maintColor = annotated.find((e) => e.rid === 'm1')!.color;
    const weatherColor = annotated.find((e) => e.rid === 'w1')!.color;
    expect(flightColor).not.toBe(maintColor);
    expect(maintColor).not.toBe(weatherColor);
    expect(flightColor).not.toBe(weatherColor);
  });

  it('given_SameObjectType_when_Annotate_then_SameColor', () => {
    const annotated = annotateEventsWithColor(events, { paletteSeed: 'test' });
    const fd1 = annotated.find((e) => e.rid === 'fd1')!.color;
    const fd2 = annotated.find((e) => e.rid === 'fd2')!.color;
    expect(fd1).toBe(fd2);
  });

  it('given_ExplicitMapping_when_Annotate_then_MappingWins', () => {
    const annotated = annotateEventsWithColor(events, {
      colorMap: { Weather: '#0000FF' },
    });
    const w1 = annotated.find((e) => e.rid === 'w1')!;
    expect(w1.color).toBe('#0000FF');
  });

  it('given_NoMappingAndNoSeed_then_StableDefaultColors', () => {
    const a = annotateEventsWithColor(events, {});
    const b = annotateEventsWithColor(events, {});
    expect(a.find((e) => e.rid === 'fd1')!.color).toBe(
      b.find((e) => e.rid === 'fd1')!.color,
    );
  });
});

describe('VTX-082 filterEventsByCategory', () => {
  it('given_DeselectedCategory_when_Filter_then_TypeHidden', () => {
    const r = filterEventsByCategory(events, new Set(['FlightDelay', 'Maintenance']));
    expect(r.map((e) => e.rid).sort()).toEqual(['fd1', 'fd2', 'm1']);
  });

  it('given_EmptySelection_when_Filter_then_NoneShown', () => {
    expect(filterEventsByCategory(events, new Set())).toEqual([]);
  });

  it('given_AllSelected_when_Filter_then_AllShown', () => {
    const all = new Set(['FlightDelay', 'Maintenance', 'Weather']);
    expect(filterEventsByCategory(events, all)).toHaveLength(4);
  });
});
