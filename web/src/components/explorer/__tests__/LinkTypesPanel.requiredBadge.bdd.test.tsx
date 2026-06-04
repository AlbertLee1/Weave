import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { LinkTypesPanel } from '../LinkTypesPanel';
import type { LinkType } from '../../../api/types';

// LINK-REQ: pkg/oms LinkType.IsRequired is serialised as `required` and the
// frontend already parses it onto LinkType.required, but LinkTypesPanel
// dropped the flag — only apiName / linkedObjectTypeApiName / description /
// cardinality were rendered. These scenarios pin the contract that the
// Explorer surfaces whether each link is required.

const linkTypes: LinkType[] = [
  {
    rid: 'ri.link.required',
    apiName: 'primaryOwner',
    displayName: 'Primary Owner',
    description: 'The owning account',
    objectTypeApiName: 'Order',
    linkedObjectTypeApiName: 'Account',
    cardinality: 'MANY_TO_MANY',
    required: true,
  },
  {
    rid: 'ri.link.optional',
    apiName: 'relatedTicket',
    displayName: 'Related Ticket',
    description: 'An optional support ticket',
    objectTypeApiName: 'Order',
    linkedObjectTypeApiName: 'Ticket',
    cardinality: 'ONE_TO_MANY',
    required: false,
  },
];

function rowForApiName(apiName: string) {
  const li = screen.getByText(apiName).closest('li');
  if (!li) throw new Error(`row for ${apiName} not found`);
  return within(li as HTMLElement);
}

describe('BDD: LinkTypesPanel surfaces required/optional status for link types (LINK-REQ)', () => {
  it('Given a link with required=true, When the panel renders, Then that row shows a "Required" marker', () => {
    render(<LinkTypesPanel linkTypes={linkTypes} />);
    const requiredRow = rowForApiName('primaryOwner');
    expect(requiredRow.getByText('Required')).toBeInTheDocument();
  });

  it('Given a link with required=false, When the panel renders, Then that row does not show a "Required" marker', () => {
    render(<LinkTypesPanel linkTypes={linkTypes} />);
    const optionalRow = rowForApiName('relatedTicket');
    expect(optionalRow.queryByText('Required')).not.toBeInTheDocument();
  });

  it('Given a non-required link, When the panel renders, Then that row keeps showing its cardinality badge', () => {
    render(<LinkTypesPanel linkTypes={linkTypes} />);
    const optionalRow = rowForApiName('relatedTicket');
    // cardinality is still rendered (ONE_TO_MANY -> ONE:TO:MANY).
    expect(optionalRow.getByText('ONE:TO:MANY')).toBeInTheDocument();
  });
});
