import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { PropertiesTable } from '../PropertiesTable';
import type { Property } from '../../../api/types';

// Property deprecation display: the Explorer PropertiesTable already fetches
// the authoritative detailed Property[] (via /objectTypes/byRid/{rid}/
// properties), which carries `status` ('DEPRECATED'), `deprecatedReason`,
// and `editOnly`. The table previously dropped that lifecycle metadata,
// rendering only the boolean flags. These scenarios pin the contract that a
// DEPRECATED property surfaces a DEPRECATED badge (with the reason as a
// tooltip) plus an edit-only indicator, while an ACTIVE property surfaces
// neither.

const compactProperties: Record<string, { dataType: { type: string }; rid: string }> = {
  legacyCode: { dataType: { type: 'string' }, rid: 'ri.p.legacyCode' },
  name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
};

const detailedProperties: Property[] = [
  {
    rid: 'ri.p.legacyCode',
    apiName: 'legacyCode',
    baseType: 'string',
    isArray: false,
    isNullable: true,
    isSearchable: false,
    isSortable: false,
    status: 'DEPRECATED',
    deprecatedReason: 'Replaced by canonicalCode in v3',
    editOnly: true,
  },
  {
    rid: 'ri.p.name',
    apiName: 'name',
    baseType: 'string',
    isArray: false,
    isNullable: false,
    isSearchable: true,
    isSortable: true,
    status: 'ACTIVE',
    editOnly: false,
  },
];

function rowByApiName(apiName: string) {
  const cell =
    screen.getByText(apiName).closest('[role="row"]') ??
    screen.getByText(apiName).closest('tr');
  if (!cell) throw new Error(`row for ${apiName} not found`);
  return within(cell as HTMLElement);
}

describe('BDD: PropertiesTable surfaces property deprecation status & edit-only flag', () => {
  it('Given a DEPRECATED detailed property with a reason, When the table renders, Then its row shows a DEPRECATED badge whose tooltip carries the reason', () => {
    render(
      <PropertiesTable
        properties={compactProperties}
        detailedProperties={detailedProperties}
        detailedStatus="ready"
      />,
    );

    const row = rowByApiName('legacyCode');
    const badge = row.getByText('DEPRECATED');
    expect(badge).toBeInTheDocument();

    // The deprecation reason must be discoverable as a tooltip (title attr)
    // on the badge or a wrapping element so operators see *why* it is gone.
    const titled =
      (badge.getAttribute('title') ? badge : badge.closest('[title]')) as HTMLElement | null;
    expect(titled).not.toBeNull();
    expect(titled?.getAttribute('title')).toContain('Replaced by canonicalCode in v3');
  });

  it('Given a DEPRECATED detailed property that is edit-only, When the table renders, Then its row shows an edit-only indicator', () => {
    render(
      <PropertiesTable
        properties={compactProperties}
        detailedProperties={detailedProperties}
        detailedStatus="ready"
      />,
    );

    const row = rowByApiName('legacyCode');
    expect(row.getByText(/edit[- ]?only/i)).toBeInTheDocument();
  });

  it('Given an ACTIVE detailed property, When the table renders, Then its row shows neither a DEPRECATED badge nor an edit-only indicator', () => {
    render(
      <PropertiesTable
        properties={compactProperties}
        detailedProperties={detailedProperties}
        detailedStatus="ready"
      />,
    );

    const row = rowByApiName('name');
    expect(row.queryByText('DEPRECATED')).toBeNull();
    expect(row.queryByText(/edit[- ]?only/i)).toBeNull();
  });
});
