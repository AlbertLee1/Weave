import { describe, expect, it } from 'vitest';
import { buildAggregationCsv } from '../aggregationCsv';
import type { AggregationBucket } from '../../api/aggregation';

describe('buildAggregationCsv', () => {
  it('Given formula-like aggregation headers and group values, When CSV is built, Then dangerous strings are forced to text', () => {
    const data: AggregationBucket[] = [
      {
        group: {
          '=region': '=cmd|A1',
          '  +segment': '  -retail',
          '@note': '@SUM(1,1)',
          tabbed: '\t=indirect',
          carriage: '\r+hidden',
        },
        metrics: {
          '-total': -42,
          count: 7,
        },
      },
    ];

    expect(buildAggregationCsv(data)).toBe(
      [
        "'=region,'  +segment,'@note,tabbed,carriage,'-total,count",
        `'=cmd|A1,'  -retail,"'@SUM(1,1)",'\t=indirect,"'\r+hidden",-42,7`,
        '',
      ].join('\n'),
    );
  });
});
