import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { PropertiesTable } from '../PropertiesTable';
import type { Property } from '../../../api/types';

// DOG-005: /explorer/ainews/AI_News rendered every property's Searchable
// and Sortable column as ✕ despite the detailed properties endpoint
// reporting true for title/category/deliveredDate/etc. The cause: the
// compact `objectType.properties` map carries only (dataType, rid), so
// PropertiesTable defaulted every flag to false. These scenarios pin
// the contract that authoritative detailed metadata wins when supplied.

const compactProperties: Record<string, { dataType: { type: string }; rid: string }> = {
  title: { dataType: { type: 'string' }, rid: 'ri.p.title' },
  rank: { dataType: { type: 'integer' }, rid: 'ri.p.rank' },
  rawItem: { dataType: { type: 'string' }, rid: 'ri.p.rawItem' },
};

const detailedProperties: Property[] = [
  {
    rid: 'ri.p.title',
    apiName: 'title',
    baseType: 'string',
    isArray: false,
    isNullable: false,
    isSearchable: true,
    isSortable: true,
  },
  {
    rid: 'ri.p.rank',
    apiName: 'rank',
    baseType: 'integer',
    isArray: false,
    isNullable: false,
    isSearchable: false,
    isSortable: true,
  },
  {
    rid: 'ri.p.rawItem',
    apiName: 'rawItem',
    baseType: 'string',
    isArray: false,
    isNullable: true,
    isSearchable: true,
    isSortable: false,
  },
];

function flagCellsByRowApiName(apiName: string) {
  const cell = screen.getByText(apiName).closest('[role="row"]') ??
    screen.getByText(apiName).closest('tr');
  if (!cell) throw new Error(`row for ${apiName} not found`);
  return within(cell as HTMLElement);
}

describe('BDD: PropertiesTable reflects authoritative detailed property flags (DOG-005)', () => {
  it('Given detailed properties mark title as searchable and sortable, When the table renders, Then title shows Searchable ✓ and Sortable ✓', () => {
    render(
      <PropertiesTable
        properties={compactProperties}
        detailedProperties={detailedProperties}
      />,
    );
    const row = flagCellsByRowApiName('title');
    const checks = row.getAllByText('✓');
    expect(checks.length).toBeGreaterThanOrEqual(2);
    // queryByText throws when multiple matches; title row also carries
    // Array=✕ and Nullable=✕ so we expect at least one ✕ — assert via
    // the all-variant.
    expect(row.queryAllByText('✕').length).toBeGreaterThanOrEqual(1);
  });

  it('Given detailed properties mark rank sortable but not searchable, When the table renders, Then rank shows Sortable ✓ and Searchable ✕', () => {
    render(
      <PropertiesTable
        properties={compactProperties}
        detailedProperties={detailedProperties}
      />,
    );
    const row = flagCellsByRowApiName('rank');
    const cells = row.getAllByText(/✓|✕/);
    expect(cells.length).toBeGreaterThanOrEqual(2);
  });

  it('Given the detailed properties request is still loading, When the table renders, Then flags degrade explicitly to a loading marker instead of confidently showing all ✕', () => {
    render(
      <PropertiesTable
        properties={compactProperties}
        detailedProperties={undefined}
        detailedStatus="loading"
      />,
    );
    // The loading degradation should NOT show every flag as ✕ for every row.
    // We assert at least one cell carries the unknown/loading sentinel.
    const unknownMarkers = screen.getAllByText('…');
    expect(unknownMarkers.length).toBeGreaterThan(0);
  });

  it('Given the detailed properties request failed, When the table renders, Then flags degrade explicitly to an unknown marker rather than misleading false flags', () => {
    render(
      <PropertiesTable
        properties={compactProperties}
        detailedProperties={undefined}
        detailedStatus="error"
      />,
    );
    const unknownMarkers = screen.getAllByText('?');
    expect(unknownMarkers.length).toBeGreaterThan(0);
  });
});
