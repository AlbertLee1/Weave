import { describe, expect, it } from 'vitest';
import { buildObjectSetDiffCsv } from '../objectSetDiffCsv';
import type { ObjectSetDiff } from '../objectSetDiff';

describe('buildObjectSetDiffCsv', () => {
  it('Given formula-like primary keys, field names, and values, When CSV is built, Then dangerous strings are forced to text', () => {
    const diff: ObjectSetDiff = {
      onlyInA: [
        {
          __rid: 'ri.a',
          __primaryKey: '=A1',
          __apiName: 'Employee',
          '+field': '@value',
          safe: 12,
        },
      ],
      changed: [
        {
          primaryKey: '  -changed',
          rowA: {
            __rid: 'ri.changed.a',
            __primaryKey: '  -changed',
            __apiName: 'Employee',
          },
          rowB: {
            __rid: 'ri.changed.b',
            __primaryKey: '  -changed',
            __apiName: 'Employee',
          },
          fieldChanges: [
            {
              field: '@title',
              valueA: '\t=old',
              valueB: '\r+new',
            },
          ],
        },
      ],
      onlyInB: [],
    };

    expect(buildObjectSetDiffCsv(diff, ['+field', 'safe'])).toBe(
      [
        'section,primaryKey,field,valueA,valueB',
        "Only in A,'=A1,'+field,'@value,",
        "Only in A,'=A1,safe,12,",
        `Changed,'  -changed,'@title,'\t=old,"'\r+new"`,
        '',
      ].join('\n'),
    );
  });
});
